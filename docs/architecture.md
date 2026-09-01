# Architecture

## Principle of operation

Everything in the system reduces to one idea:

> **Every "thing" is an append-only event stream on a named topic, and its
> "current shape" is a pure function of that stream.**

From that single sentence, the rest of the design follows mechanically:

1. **The event stream is the source of truth. State is a derived cache.**
   Publishers *append* events to a topic. A reducer folds those events into
   a snapshot. The snapshot is cheap to read but never authoritative — it
   can always be recomputed from the events. This is why historical reads
   return raw events (no `state`) and only live fan-out carries `state`.

2. **Ordering is the currency.** Every event carries a per-topic monotonic
   sequence number (`seq`). Everything else in the system — reducer
   determinism, dedup after backfill, reconnect-with-resume, atomicity of
   `int_incr` — relies on that one invariant.

3. **Serialise for correctness, fan out non-blocking for speed.** The
   ingest path (Append → Reduce → SetState → Publish-to-hub) runs under a
   single lock, so reducers see a consistent read-modify-write. Delivery
   to subscribers is best-effort per channel — one slow consumer never
   blocks another (drop-and-mark policy). This split is deliberate: the
   thing that must be linear (assigning `seq` and updating state) is
   linear; the thing that must be fast (fan-out) is lock-free.

4. **Reducers are pure and pluggable.** A reducer is just
   `(state, event) → state`. It knows nothing about storage, transport,
   auth, or how it's called. To model a new kind of object, write one
   file with one method. That's why the same server supports rich
   lifecycles (PR, deploy, build) and scalar fields (int, str, time)
   with the same core code.

5. **One wire protocol, many clients.** All producers, subscribers, UIs,
   and MCUs speak the same JSON frames over WebSocket. HTTP endpoints
   exist only as sugar for callers that can't hold a WS (services doing
   fire-and-forget publishes, webhook providers, browsers that want to
   long-poll). This is why porting the client library to a new language
   is a weekend, not a project.

6. **Storage is a swappable interface.** The `Store` interface handles
   events + state + meta. Two implementations ship (in-memory + Redis);
   both pass the same test suite. The choice is a runtime flag — the rest
   of the code never sees it.

## Structural elements

The server is a Go binary organised as a few narrowly-scoped layers.
Nothing here is clever — each piece owns one job and hands off to the
next through a small interface.

**Transport (`internal/transport`)** — HTTP + WebSocket handlers. Stateless.
Its job is to translate one wire frame into one call on the Broker, and one
outgoing event on the Hub into one wire frame back. Every handler is a
thin adapter; no business logic lives here.

**Broker (`internal/broker`)** — the ingest orchestrator, and the only
package that mutates state. Holds a per-server mutex around the four-step
ingest sequence (`Append → GetState → Apply → SetState`) so `int_incr` and
friends are atomic under any number of concurrent publishers. Also exposes
`Subscribe(topic, from)` which wires up a Hub subscription plus the
correct backfill.

**Store (`internal/store`)** — persistence. An interface with two
implementations:
- `memory/` — a `map[topic]*topicData` with an `RWMutex`. Used for tests
  and zero-dep local runs. No network, no fsync.
- `redis/` — Redis Streams for events, Hashes for meta, Strings for state,
  Sets for topic enumeration, INCR for sequence assignment. Uses a
  `TxPipeline` per Append.

**Hub (`internal/hub`)** — in-process fan-out only. Holds a registry
`topic → set of Subscriptions`. `Publish(event)` iterates subscribers and
attempts a non-blocking send to each; buffer-full drops the event and
increments the sub's `lag` counter. `Subscribe(topic) → *Subscription`
returns a channel and a `done` chan; there is no channel close (races),
just done-based cancellation.

**Reducers (`internal/objtypes` + `internal/computed`)** — a `Registry`
mapping `object_type → Reducer`. Reducers are pure: they get the previous
`json.RawMessage` state and an event, and return the new state as
`json.RawMessage`. One file per object type. The `Broker` looks up the
reducer by the topic's first segment and calls `Apply` inside the ingest
lock.

**Auth (`internal/auth`)** — `Authenticator` interface with two
implementations: `noop` (default, everyone anonymous) and `bearer` (static
token via `Authorization` header or `?access_token=` query). Wired as
standard `http.Handler` middleware. Off by default; when enabled, applies
uniformly to WS upgrade, publish, and all reads.

**Webhook plugins (`internal/webhook`)** — a `WebhookPlugin` interface
(`Verify` + `Transform`) plus a registry. Each plugin translates a
provider's raw payload into 0..N `core.Event` calls back into the Broker.
GitHub ships in v1. Adding one is a single Go file plus a line in
`server.go`.

**Metrics (`internal/metrics`)** — Prometheus registry with low-cardinality
labels (`object_type` only, never per-topic). Also exports a JSON snapshot
for the built-in UI. Counters/histograms are updated by the Transport and
the Broker.

**Archiver (`internal/archiver`)** — a background goroutine that scans
`ListTopics`, reads each `TopicMeta.TTL`, and calls `Store.DeleteTopic` for
anything past its TTL. Runs on a configurable interval (default 5m).

**Client library (`client/` and `clients/*`)** — same responsibilities
across every language, matched to that language's idioms. Owns the WS
socket, dispatches inbound events to callback registries (with
refcount-per-topic on the full-parity clients), sends outbound requests
with `req_id` for request/response, and reconnects on drop with
per-topic `from_seq=lastSeen+1` resume.

### How the pieces compose

Two data paths, no more:

**Publish** — Transport parses a frame → Broker validates topic → Broker
acquires ingest lock → Store.Append assigns seq → Broker reads prev state,
runs reducer, writes next state, upserts meta → Broker attaches `state`
to a copy of the event → Hub.Publish fans out that copy → each
subscriber's forwarder writes to its WS. All of that is one function
call per event; no queue, no goroutine handoff except at fan-out.

**Subscribe** — Transport parses the frame → Broker calls
`Hub.Subscribe(topic)` *before* reading history (so no live event is
missed during the read) → Broker fetches historical events per the
`from` option → Broker returns `(sub, backfill, fenceSeq)` → Transport
sends backfill first, then an `ack` with `last_seq=fenceSeq`, then
forwards live events from `sub.C` while dropping any with
`seq <= fenceSeq`.

Everything else — reconnect, ack, get_state, lagging frames — is a small
variation on those two.

## Core types

- **`Event`** — `{id, topic, type, seq, occurred_at, actor, payload, state?}`.
  `seq` is per-topic monotonic starting at 1. `state` is present only on
  live fan-out; historical reads return it empty.
- **`Topic`** — `<object_type>/<segment>[/<segment>...]`. Each segment
  matches `[A-Za-z0-9._-]+`, max 512 chars total. The first segment picks
  the reducer.
- **`TopicMeta`** — `{topic, object_type, ttl, created_at, last_event_at,
  last_seq}`. Written on every Append, read by the archiver.
- **Reducer** — Go interface `ObjectType() string` +
  `Apply(state json.RawMessage, e *Event) (json.RawMessage, error)`.
  Pure. Called under the Broker mutex, so the read-modify-write is atomic.
- **Subscription** — server-side: one `hub.Subscription` per WS per topic.
  Client-side: many callback registrations per topic, refcounted, all sharing
  one upstream WS subscription.

---

## Component map

With the principle and structure above in mind, here's the same picture as
boxes and arrows.

```mermaid
flowchart TB
    subgraph Transport["internal/transport — HTTP + WebSocket"]
        WS["ws.go<br/>WebSocket handler<br/>(subscribe/publish/get_state/ping)"]
        POLL["poll.go<br/>short + long-poll fallback"]
        PUB["publish.go<br/>POST /publish<br/>POST /webhook/{plugin}"]
        FLD["field.go<br/>POST /field/set, incr, decr, delete, ..."]
        ADM["admin.go<br/>state, events, topics, metrics.json"]
    end

    subgraph Core["Core"]
        BR["internal/broker<br/>Broker: ingest orchestrator<br/>serialises Append→Reduce→Publish"]
        HUB["internal/hub<br/>in-process fan-out<br/>subscription channels"]
        REG["internal/computed<br/>Reducer registry<br/>(object_type → Reducer)"]
    end

    subgraph Reducers["internal/objtypes — one file per type"]
        R1["pr.go<br/>build.go<br/>deploy.go<br/>job.go<br/>chat.go"]
        R2["field.go<br/>str / int / time reducers"]
    end

    subgraph Storage["internal/store"]
        SI["Store interface"]
        MEM["memory/<br/>in-proc map + slice + RWMutex"]
        RED["redis/<br/>Streams + Hashes + Set"]
    end

    subgraph Auth["internal/auth"]
        AI["Authenticator interface"]
        NOO["noop<br/>anonymous"]
        BEA["bearer<br/>header or ?access_token="]
    end

    subgraph Sidecars["Sidecars"]
        MET["internal/metrics<br/>Prometheus + JSON snapshot"]
        ARC["internal/archiver<br/>periodic TTL sweep"]
        WH["internal/webhook<br/>plugin registry + github/"]
    end

    Transport -->|Publish| BR
    Transport -->|Subscribe| BR
    BR -->|Append/Read/GetState| SI
    BR -->|Apply| REG
    REG --> Reducers
    BR -->|Publish| HUB
    HUB -->|fan out| Transport
    SI -.-> MEM
    SI -.-> RED
    Transport -->|middleware| Auth
    AI -.-> NOO
    AI -.-> BEA
    Transport -->|counters/histograms| MET
    ARC -->|DeleteTopic| SI
    Transport -->|webhook plugin| WH
    WH -->|Transform → Events| BR
```

## Ingest path — single-topic ordering under concurrent publishers

Every publish (from HTTP, WS, or a webhook) hits `Broker.Publish`, which
grabs a **process-wide mutex** for the duration of one event. This is the
guarantee that makes `int_incr` atomic: no interleaving between `read state
→ apply reducer → write state`.

```mermaid
sequenceDiagram
    autonumber
    participant P as Producer
    participant BR as Broker
    participant ST as Store
    participant RD as Reducer
    participant HB as Hub
    participant SUB as Subscribers

    P->>BR: Publish event
    Note right of BR: ingest.Lock()
    BR->>ST: Append -> seq
    BR->>ST: GetState -> prev
    BR->>RD: Apply(prev, event) -> next
    BR->>ST: SetState(topic, next)
    BR->>ST: UpsertMeta(object_type, TTL)
    Note over BR: attach state=next to a copy of the event
    BR->>HB: Publish fanout copy
    Note right of BR: ingest.Unlock()
    HB-->>SUB: event with state, on each sub channel
```

Design choices worth calling out:

- **Store.Append happens before reduce.** The persisted event never carries the reduced state — state is a *derived cache*, and historical reads (`Read`, `Latest`) return raw events. This keeps the stream canonical and lets you evolve reducers without rewriting history.
- **The fanned-out event is a copy** (`fanout := *e`) with `State` set. Subscribers get event + current-state in one frame; the persisted event doesn't.
- **A single ingest mutex** is fine at v1 scale. If you need higher throughput for cross-topic parallelism, shard by `hash(topic) % N` — the Broker exposes a pluggable hook for this.

## Fan-out — one hub, per-sub buffered channels, drop-and-mark

```mermaid
flowchart LR
    IN[Broker.Publish] --> H[Hub.Publish]
    H --> S1["sub1.C<br/>cap 256"]
    H --> S2["sub2.C<br/>cap 256"]
    H --> S3["sub3.C<br/>cap 256, FULL"]
    S1 --> W1[WS writer 1]
    S2 --> W2[WS writer 2]
    S3 -.->|drop-and-mark| MET["dropped_slow_consumer_total ++"]
    S3 -.->|lag counter ++| L3["send 'lagging' frame<br/>to client"]
```

- Buffered per-subscriber channels (default 256).
- Non-blocking `select` — a slow consumer never blocks the publisher; instead the event is dropped, `sub.Lag()` increments, and a `{"type":"lagging","missed":N}` frame is delivered so the client can decide to refetch via `GetState`.
- Close is race-safe: the channel is *never* closed; instead a `done` chan is closed and publishers include a `<-done` arm in their non-blocking select.

## Subscribe backfill — seed + fence-based dedup

When you subscribe with `from=last:50` or `from_seq=X`, the server needs to
avoid duplicating events between the historical replay and the live tail.

```mermaid
sequenceDiagram
    participant C as Client
    participant BR as Broker
    participant ST as Store
    participant HB as Hub

    C->>BR: subscribe(topic, from=last:50)
    BR->>HB: hub.Subscribe(topic) → sub
    Note over BR: sub is live from this instant
    BR->>ST: Latest(topic, 50) → backfill[]
    Note over BR: fenceSeq = backfill[last].seq
    BR-->>C: replay backfill events
    BR-->>C: {"type":"ack","last_seq":fenceSeq}
    loop live tail
        HB-->>BR: event on sub.C
        alt event.seq <= fenceSeq
            Note over BR: drop (already in backfill)
        else event.seq > fenceSeq
            BR-->>C: event
        end
    end
```

The subscription is created **before** the backfill fetch, so no live event
is missed. The `fenceSeq` is used to dedupe against events that raced in
between.

## Client-side refcount dispatch

Every language client (except the MCU ones) implements the same idea: N
callbacks on one topic share one upstream WS subscription. Only when the
last handle closes does the client send `unsubscribe` upstream.

```mermaid
flowchart LR
    subgraph App["Application"]
        A["widget A<br/>Subscribe -> handle1"]
        B["widget B<br/>Subscribe -> handle2"]
        C["widget C<br/>Subscribe -> handle3"]
    end
    subgraph Client["client library"]
        R["topic table<br/>pr/x -> cb1, cb2, cb3"]
    end
    A --> R
    B --> R
    C --> R
    R -->|one WS subscribe| SRV[server]
    SRV -->|one event stream| R
    R -->|cb1 event| A
    R -->|cb2 event| B
    R -->|cb3 event| C
```

When `handle2.Close()` runs, the client removes `cb2`; the WS subscription
stays because `[cb1, cb3]` is still non-empty. When the last handle closes,
the client sends `{"op":"unsubscribe","topic":"pr/x"}`.

## Package structure (server side)

```
event_watch/
  cmd/
    server/                # cmd/server/main.go — flags → server.Run
    testclient/            # scriptable CLI harness
  internal/
    core/                  # Event, TopicMeta, Topic parse, SubscribeOpts
    store/
      store.go             # interface + ErrNotFound
      memory/store.go      # in-proc impl (map + slice + RWMutex)
      redis/store.go       # Redis impl (Streams + Hashes + Sets + INCR)
    hub/                   # subscriber registry + non-blocking fan-out
    computed/              # Reducer interface + Registry
    objtypes/              # one file per object type
    broker/                # ingest orchestrator (this is the "brain")
    auth/                  # Authenticator + noop + bearer + middleware
    transport/             # HTTP + WS handlers
    metrics/               # Prometheus registry + JSON snapshot
    archiver/              # background TTL sweep
    webhook/
      plugin.go            # WebhookPlugin interface + Registry
      github/              # first-party plugin
    server/                # config + Run() wire-up
  client/                  # Go client library (external-facing)
  clients/                 # non-Go client libraries
    python/  java/  rust/  esp-idf/  arduino/
  web/                     # htmx UI embedded via go:embed
  wails-client/            # native desktop app
```

## Deployment topology

### v1 (single-node)

```mermaid
flowchart LR
    subgraph Node["one event_watch process"]
        S["HTTP + WS server<br/>:8080"]
        HUB[in-process hub]
        BR[broker]
        MEM["memory or redis store"]
    end
    R[("Redis (optional)")]
    C[clients]
    C <-->|WebSocket| S
    C -->|POST publish or webhook| S
    MEM -.->|persist| R
```

- One server binary. All fan-out is in-process; sub-millisecond delivery.
- Store choice (memory vs redis) is a startup flag. Same interface either way.
- Restart-safe with Redis (events + state survive restarts).

### Future (multi-node)

```mermaid
flowchart LR
    subgraph Cluster["N event_watch nodes behind LB"]
        N1[node 1] --- N2[node 2] --- N3[node 3]
    end
    R[("Redis Streams + PubSub")]
    LB[load balancer]
    C[clients]
    C <-->|WebSocket sticky| LB
    LB --> N1
    LB --> N2
    LB --> N3
    N1 -.->|Notify| R
    N2 -.->|Watch| R
    N3 -.->|Watch| R
```

The Store interface already has `Notify(topic, event)` and `Watch()` hook
methods — the Redis implementation doesn't use them yet, and neither does the
broker. Flipping the switch is a plumbing exercise, not a redesign.
