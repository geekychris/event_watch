# Wire protocol

Everything a client — in any language — needs to know to talk to the server.

## Transport

- **WebSocket** at `GET /ws` for interactive clients (subscribe + publish + get_state, low-latency fan-out).
- **HTTP** for service-to-service publish, webhook ingress, long-poll fallback, and admin/state reads.

All JSON. `Content-Type: application/json`. UTF-8.

## Auth

Off by default. When `--auth=bearer --auth-token=<t>` is set, every request
and the WS upgrade must authenticate as **one of**:

- `Authorization: Bearer <t>` header
- `?access_token=<t>` query param (needed for browser JS `WebSocket()` which can't set headers)

`/webhook/{plugin}` bypasses this middleware — plugins do their own signature
verification (GitHub uses `X-Hub-Signature-256`).

## Common types

### Event

Every event delivered by the server (subscribe frame, historical fetch, or
long-poll batch) has this shape:

```json
{
  "id":          "16-byte-hex",
  "topic":       "int/counter",
  "type":        "int_incr",
  "seq":         42,
  "occurred_at": "2026-08-31T22:07:44.123456789-07:00",
  "actor":       "alice",
  "payload":     {"delta": 5},
  "state":       {"value": 105, "exists": true, "updated_at": "..."}
}
```

- `seq` is monotonic **per topic**, starts at 1.
- `state` is present **only on live fan-out** — historical reads
  (`GET /events`, `Store.Read`) return `state=null` because state is a
  derived cache, not persisted per event.
- `payload` is topic-type-specific; see [object-types.md](object-types.md).

### Topic

`<object_type>/<segment>[/<segment>...]`. Each segment: `[a-zA-Z0-9._-]+`.
Max total length 512. Object types are **case-sensitive**; use lowercase.

---

## WebSocket protocol

### Client → server frames

#### `subscribe`

```json
{"op":"subscribe","topic":"pr/octo/hello/1","from":"latest"}
{"op":"subscribe","topic":"build/ci/42","from":"last:50"}
{"op":"subscribe","topic":"deploy/prod/api","from_seq":128}
```

- `from`: `"latest"` (only new) or `"last:N"` (backfill N most recent events, then live)
- `from_seq`: alternative to `from` — start at exactly `seq` (inclusive)
- Duplicate subscribe for the same `topic` on one WS returns an error frame

#### `unsubscribe`

```json
{"op":"unsubscribe","topic":"pr/octo/hello/1"}
```

#### `publish`

```json
{"op":"publish","topic":"int/counter","type":"int_incr","payload":{"delta":5},"actor":"alice","req_id":"r7"}
```

Server assigns `seq`, `id`, and `occurred_at`. `req_id` is optional; if set,
the server replies with an ack carrying the same `req_id` and the new `seq`.

#### `get_state`

```json
{"op":"get_state","topic":"int/counter","req_id":"r8"}
```

Server replies with the current computed state (or a `not found` error frame
if the topic has no state yet).

#### `ping`

```json
{"op":"ping"}
```

Application-level heartbeat; server replies `{"type":"pong"}`. Independent of
the WS PING/PONG control frames the server also sends.

### Server → client frames

#### `event`

```json
{"type":"event","topic":"int/counter","event":{ /* Event, see above */ }}
```

#### `state` (response to `get_state`)

```json
{"type":"state","topic":"int/counter","state":{"value":105,"exists":true},"req_id":"r8"}
```

#### `ack` (response to `subscribe` or `publish`)

```json
{"type":"ack","topic":"int/counter","last_seq":5,"req_id":"r7"}
```

`last_seq` on a subscribe ack is the **fence** — the highest seq in the
backfill. Live events with `seq <= fence` are dedup'd server-side; you won't
see them.

#### `lagging`

```json
{"type":"lagging","topic":"int/counter","missed":12}
```

Sent when the server dropped events because your per-subscription buffer was
full. Do a `get_state` to resync. No unsubscribe is implied — the subscription
stays alive.

#### `error`

```json
{"type":"error","message":"invalid topic","topic":"...","req_id":"r7"}
```

Only carries `req_id` if the failing op had one.

#### `pong`

```json
{"type":"pong"}
```

### Reconnect + resume

If your WS drops mid-stream, reconnect and re-issue subscribes with
`from_seq=<last_seq_you_saw>+1`. As long as the server hasn't trimmed past
that seq (see TTL / archiver), you'll get exactly the events you missed with
no duplicates.

The Go / Python / Java / Rust / ESP-IDF / Arduino clients all do this
automatically; you only need it if you're building a custom client.

### Heartbeats

- WS control PING every 30s from the server; missed PONG for 60s → server closes.
- The Go server also honors the application-level `{"op":"ping"}` op for clients that can't respond to WS PINGs.

---

## HTTP endpoints

### Publish + webhook

| Method | Path | Body | Notes |
|---|---|---|---|
| `POST` | `/publish` | `{topic, type, payload?, actor?}` | assigns seq; returns `{id, seq, topic}` |
| `POST` | `/webhook/{plugin}` | provider-native payload | plugin verifies signature + maps to 0..N events |

### Field arithmetic (sugar over `/publish`)

| Method | Path | Body | Effect |
|---|---|---|---|
| `POST` | `/field/set/{topic...}` | `{value}` | dispatches to `<obj_type>_set` (e.g. `int_set`) |
| `POST` | `/field/incr/{topic...}` | `{delta?}` | `int_incr`; delta defaults to 1 |
| `POST` | `/field/decr/{topic...}` | `{delta?}` | `int_decr`; delta defaults to 1 |
| `POST` | `/field/delete/{topic...}` | — | dispatches to `<obj_type>_delete` |
| `POST` | `/field/time-now/{topic...}` | — | `time_now` |
| `POST` | `/field/time-add/{topic...}` | `{seconds}` | `time_add` |

All return `{seq, topic, type}`.

### Reads

| Method | Path | Query | Response |
|---|---|---|---|
| `GET` | `/state/{topic...}` | — | current computed state (JSON) or `not found` |
| `GET` | `/events/{topic...}` | `from_seq`, `limit` | `{events: [Event]}` (historical, no `state` field) |
| `GET` | `/topics` | `prefix`, `limit` | `{topics: [string]}` |

### Long-poll

| Method | Path | Query | Response |
|---|---|---|---|
| `GET` | `/poll` | `topic` (repeatable), `from_seq`, `max_wait_ms` | `{events, last_seq}` |

- `max_wait_ms=0` → return immediately (short-poll)
- Otherwise, block up to that duration waiting for the first event; then batch anything else that arrives in a 20ms follow-up window so bursts collapse into one response

### Admin / observability

| Method | Path | Response |
|---|---|---|
| `GET` | `/metrics` | Prometheus text format |
| `GET` | `/admin/metrics.json` | JSON snapshot used by the UI (`connected_clients`, `subscriptions_by_type`, `ingested_by_type`, `fanned_out_by_type`, `dropped_by_type`, `topics`) |
| `GET` | `/` | embedded htmx UI |

### Static UI

`/static/*` serves the embedded UI assets (CSS, JS). Not relevant for client
integrations.

## Response codes

- `200 OK` — normal success
- `202 Accepted` — webhook accepted (may have produced 0 events)
- `400 Bad Request` — invalid topic, invalid JSON, unknown event type
- `401 Unauthorized` — auth on and token missing/wrong
- `404 Not Found` — `/state/<topic>` with no state, unknown webhook plugin
- `405 Method Not Allowed` — wrong verb on a known path

## Wire limits

- Max WS text frame: 1 MiB read limit on the server
- Max HTTP body: 1 MiB (`/publish`), 5 MiB (`/webhook/{plugin}`)
- Topic length: 512 chars
- Per-subscription channel: 256 events buffered before drop-and-mark
