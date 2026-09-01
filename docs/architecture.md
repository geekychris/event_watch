# Architecture

## Component map

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

## Data-model glossary

- **Event** — one immutable mutation. Fields: `id, topic, type, seq, occurred_at, actor, payload, state?`. Seq is per-topic monotonic. State is populated only on live fan-out.
- **Topic** — string of shape `<object_type>/<segment>[/<segment>...]`. First segment picks the reducer. All segments must be `[a-zA-Z0-9._-]+`.
- **Object type** — first segment of the topic; drives which reducer runs.
- **Reducer** — `(state, event) → state` for one object type. Pure. Called under the broker mutex.
- **Computed state** — the reducer's output at the current seq. Cached per topic; refreshed on every publish.
- **Subscription** — a `hub.Subscription` on the server (one per WS client per topic) OR a client-side callback registration (many per topic per client).
- **TopicMeta** — per-topic bookkeeping: object_type, TTL, created_at, last_event_at, last_seq. Used by the archiver.
