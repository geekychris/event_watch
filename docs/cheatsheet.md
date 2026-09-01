# Cheatsheet — every message format + event type

One-page reference. Bookmark. If you want narrative, see
[`wire-protocol.md`](wire-protocol.md) and [`object-types.md`](object-types.md).

> **This cheatsheet is also live in both UIs** (React + Wails) as a
> "Cheatsheet — event types & payload examples" card. Every row has an
> **Inject →** button that fills the Publish card's fields with the
> exact topic/type/payload — click, review, publish. Same content, no
> copy-paste from this doc into a form. Both UIs also expose small
> **ⓘ** hover-hints on the Publish card's Topic / Event type / Payload
> inputs.

## Wire — the 3 things every client does

### Publish

```bash
# via HTTP
curl -X POST -H 'Content-Type: application/json' http://localhost:8080/publish -d '{
  "topic":   "int/counter",
  "type":    "int_incr",
  "payload": {"delta": 5},
  "actor":   "alice"           // optional
}'
# → {"id":"...","seq":42,"topic":"int/counter"}
```

```json
// via WebSocket, add req_id if you want an ack:
{"op":"publish","topic":"int/counter","type":"int_incr","payload":{"delta":5},"req_id":"r1"}
// ← {"type":"ack","topic":"int/counter","last_seq":42,"req_id":"r1"}
```

### Subscribe

```json
// via WebSocket only:
{"op":"subscribe","topic":"int/counter","from":"latest"}      // new events only
{"op":"subscribe","topic":"int/counter","from":"last:50"}     // last 50 + live
{"op":"subscribe","topic":"int/counter","from_seq":100}       // from seq 100 + live
{"op":"unsubscribe","topic":"int/counter"}
```

Server frames back:
```json
{"type":"event","topic":"int/counter","event":{
  "id":"...","topic":"int/counter","type":"int_incr","seq":42,
  "occurred_at":"2026-...","payload":{"delta":5},
  "state":{"value":47,"exists":true}}}      // state only on live fan-out

{"type":"ack","topic":"...","last_seq":N,"req_id":"..."}      // backfill fence or publish confirm
{"type":"lagging","topic":"...","missed":N}                   // slow-consumer drop notice
{"type":"error","message":"...","req_id":"..."}
```

### GetState

```bash
curl http://localhost:8080/state/int/counter
# → {"value":47,"exists":true,"updated_at":"..."}     — or 404 "not found"
```

```json
// via WebSocket:
{"op":"get_state","topic":"int/counter","req_id":"r2"}
// ← {"type":"state","topic":"int/counter","state":{...},"req_id":"r2"}
```

### Auth (when `--auth=bearer`)

- Header: `Authorization: Bearer <token>`
- WS-friendly: append `?access_token=<token>` to the URL

## HTTP sugar for field ops

Every field op has an HTTP shortcut that constructs the correct event + payload:

```bash
POST /field/set/{topic...}          {"value": ...}        # str/int/time set
POST /field/incr/{topic...}         {"delta": N}          # int_incr — delta defaults to 1
POST /field/decr/{topic...}         {"delta": N}          # int_decr — delta defaults to 1
POST /field/delete/{topic...}       (no body)             # <type>_delete
POST /field/time-now/{topic...}     (no body)             # time_now
POST /field/time-add/{topic...}     {"seconds": N}        # time_add — N can be negative
```

## Topics

`<object_type>/<segment>[/<segment>...]` — each segment `[A-Za-z0-9._-]+`, max 512 chars.
**Object types are lowercase; case matters.** `Pr/x` ≠ `pr/x`.

## Every object type in one table

| Prefix | Kind | Reducer output shape |
|---|---|---|
| `pr/` | event-object | `{title, author, base, head, state, reviewers[], approvals, changes_requested, labels[], checks:{passed,failed,pending}, comments, merged_at, updated_at}` |
| `build/` | event-object | `{status, current_step, steps:[{name,status,duration_ms}], started_at, finished_at, duration_ms}` |
| `deploy/` | event-object | `{env, service, current_version, previous_version, health, in_progress, last_deploy_at, last_success_at}` |
| `job/` | event-object | `{name, percent, eta_seconds, status, logs:[{at,line}](≤50), started_at, finished_at}` |
| `chat/` | event-object | `{room, participants[], recent:[{id,user,text,posted_at,edited?}](≤50)}` |
| `str/` | scalar field | `{value, exists, updated_at}` |
| `int/` | scalar field | `{value, exists, updated_at}` |
| `time/` | scalar field | `{value, exists, updated_at}` (RFC3339) |

## Event types + payload keys

### `pr/…`

| `type` | payload |
|---|---|
| `pr_opened` | `{"title":"...", "author":"...", "base":"main", "head":"sha"}` |
| `pr_sync` | `{"head":"sha"}` |
| `pr_review_requested` | `{"reviewer":"name"}` |
| `pr_reviewed` | `{"state":"approved" \| "changes_requested"}` |
| `pr_commented` | `{}` |
| `pr_labeled` | `{"label":"name"}` |
| `pr_unlabeled` | `{"label":"name"}` |
| `pr_merged` | `{}` |
| `pr_closed` | `{}` |
| `check_run_completed` | `{"conclusion":"success" \| "failure" \| "timed_out" \| "cancelled" \| "pending" \| "queued" \| "in_progress", "name":"..."}` |

### `build/…`

| `type` | payload |
|---|---|
| `build_queued` | `{}` |
| `build_started` | `{}` |
| `step_started` | `{"step":"name"}` |
| `step_finished` | `{"step":"name", "status":"success" \| "failed" \| "skipped"}` |
| `build_finished` | `{"status":"success" \| "failed"}` |

### `deploy/…`

| `type` | payload |
|---|---|
| `deploy_started` | `{"version":"v42", "env":"prod", "service":"api"}` |
| `health_check_pass` | `{}` |
| `health_check_fail` | `{}` |
| `rollback` | `{"to":"v41"}` (optional; omit to swap prev↔current) |
| `deploy_finished` | `{"status":"success"}` (default: success) |

### `job/…`

| `type` | payload |
|---|---|
| `job_started` | `{"name":"reindex"}` |
| `job_progress` | `{"percent":42, "eta_seconds":30}` |
| `job_log` | `{"line":"processing shard 3"}` |
| `job_finished` | `{}` |
| `job_failed` | `{}` |

### `chat/…`

| `type` | payload |
|---|---|
| `user_joined` | `{"user":"alice"}` |
| `user_left` | `{"user":"alice"}` |
| `msg_posted` | `{"id":"m1", "user":"alice", "text":"hey"}` |
| `msg_edited` | `{"id":"m1", "text":"hey!"}` |
| `msg_deleted` | `{"id":"m1"}` |

### `str/…`

| `type` | payload | Effect |
|---|---|---|
| `str_set` | `{"value":"..."}` | `value` ← payload, `exists` ← true |
| `str_set` | `{"value":""}` | `value` ← "", `exists` ← **true** (empty string is still "set") |
| `str_set` | `{}` or missing key | **no-op** (reducer only writes when the key is present) |
| `str_delete` | `{}` | `value` ← "", `exists` ← false |

### `int/…`

| `type` | payload | Effect |
|---|---|---|
| `int_set` | `{"value": N}` | `value` ← N, `exists` ← true |
| `int_incr` | `{"delta": N}` | `value` += N, `exists` ← true |
| `int_incr` | `{}` (no `delta` key) | `value` **+= 1** (default) |
| `int_decr` | `{"delta": N}` | `value` -= N, `exists` ← true |
| `int_decr` | `{}` (no `delta` key) | `value` **-= 1** (default) |
| `int_delete` | `{}` | `value` ← 0, `exists` ← false |

### `time/…`

| `type` | payload | Effect |
|---|---|---|
| `time_set` | `{"value":"2026-09-01T12:00:00Z"}` | parse RFC3339 (nano-precision OK), set value + exists=true |
| `time_now` | `{}` | value ← the server's timestamp on this event (not the client's clock) |
| `time_add` | `{"seconds": N}` | value += N seconds, exists ← true. N may be **negative** to subtract. |
| `time_delete` | `{}` | value ← zero time, exists ← false |

## Math operations — deep dive

The int and time reducers do arithmetic. A few things worth internalising:

### `int_incr` / `int_decr` semantics

```
op            delta         effect                        exists after
------------  ------------  ----------------------------  ------------
int_incr      +5            value += 5                    true
int_incr      -3            value -= 3    (works!)        true
int_incr      omitted       value += 1    (default)       true
int_decr      +5            value -= 5                    true
int_decr      -3            value += 3    (adds!)         true
int_decr      omitted       value -= 1    (default)       true
int_set       +10           value = 10                    true
int_set       -10           value = -10                   true
int_delete    —             value = 0                     false
```

Practical consequences:

- **`int_incr` with a negative delta subtracts.** So `int_decr` is
  redundant — you never *need* it. It exists for readability: publishing
  `int_decr {"delta":3}` reads more clearly than `int_incr {"delta":-3}`.
- **`int_decr` with a negative delta adds** (subtract a negative).
  Confusing but consistent. Prefer positive deltas + the right op name.
- **Incr on an unset topic starts from 0.** No need to `int_set 0` first.
  `int_incr {"delta":5}` on a fresh topic → `value=5, exists=true`.
- **`int_delete` isn't the same as `int_set 0`.**
  - `int_set {"value":0}` → `value=0, exists=**true**`
  - `int_delete` → `value=0, exists=**false**`
  If your consumer needs to distinguish "explicitly zero" from "never
  set", read `exists`.

### `time_add` semantics

```
op           seconds       effect                                exists after
-----------  ------------  ------------------------------------  ------------
time_set     "RFC3339"     value = parsed time                   true
time_now     —             value = event.OccurredAt              true
time_add     +3600         value += 1h                           true
time_add     -60           value -= 1m                           true
time_delete  —             value = 0001-01-01T00:00:00Z         false
```

Gotchas:

- **`time_add` on an unset topic** starts from the Go zero time
  (`0001-01-01T00:00:00Z`), so `time_add {"seconds":3600}` produces
  `0001-01-01T01:00:00Z`, which is almost certainly not what you want.
  Do `time_set` or `time_now` first.
- **`time_set` needs RFC3339.** `2026-09-01` alone won't parse — you
  need at least `2026-09-01T00:00:00Z`. Nanoseconds are OK
  (`2026-09-01T00:00:00.123456789Z`).
- **`time_now` uses the *server* clock** — the client's `OccurredAt`
  field is overwritten server-side, so there's no clock-skew issue when
  clients disagree on "now".

### Concurrent-write guarantees

The broker holds an ingest mutex across `Append → GetState → Apply →
SetState → Hub.Publish`, so **arithmetic on a single topic is atomic**
across any number of concurrent publishers.

- 500 concurrent `int_incr {"delta":1}` from any mix of clients →
  `value=500`. Proven by `internal/broker/field_atomic_test.go`.
- No compare-and-swap primitive needed; no version tokens; no retries.
- Different topics can be updated in parallel (the mutex is process-wide
  at v1, but ingest is cheap so it's rarely the bottleneck; sharding by
  topic hash is trivial if it ever becomes one).

## Typed client helpers (do the right thing automatically)

Every language client exposes helpers so you can't get the type+payload
combo wrong. `Set/Incr/Decr/Get` return `[value, exists]`:

```go   // Go
counter := c.IntField("int/counter"); counter.Set(ctx, 10); counter.Incr(ctx, 5)
v, exists, _ := counter.Get(ctx)
```
```python  # Python
counter = c.int_field("int/counter"); await counter.set(10); await counter.incr(5)
v, exists = await counter.get()
```
```typescript  // TS/JS
const counter = c.intField('int/counter'); await counter.set(10); await counter.incr(5)
const [v, exists] = await counter.get()
```
```rust  // Rust
let counter = c.int_field("int/counter"); counter.set(10).await?; counter.incr(5).await?;
let (v, exists) = counter.get().await?;
```
```java  // Java
IntField counter = c.intField("int/counter"); counter.set(10); counter.incr(5);
Object[] r = counter.get();  // [Long, Boolean]
```
```c  // ESP-IDF
eventwatch_int_set(c, "int/counter", 10);
eventwatch_int_incr(c, "int/counter", 5);
// (no get — read state from the `state_json` param on subscribe)
```
```cpp  // Arduino
ew.intSet("int/counter", 10);
ew.intIncr("int/counter", 5);
```

## Common gotchas

- **Case-sensitive topics.** `Pr/x` and `pr/x` are different topics; the
  reducer registry is keyed by lowercase prefix so mixed-case topics
  bypass the reducer entirely (event stored, no state computed).
- **Event type spelling.** `int_incr` (lowercase, underscore). Any other
  spelling (`Incr`, `increment`, `IntIncr`) is stored but the reducer
  ignores it — nothing changes in `state`.
- **Payload keys are exact.** `{"delta":5}` for incr/decr, `{"value":5}`
  for set, `{"seconds":60}` for time_add. Wrong key → the reducer takes
  its default (`+1` for incr/decr, no-op for set).
- **`state` on live event frames only.** Historical reads
  (`/events/<topic>`, `Store.Read/Latest`) return raw events without
  `state`. Fetch `/state/<topic>` for the current snapshot.
- **`exists=false` after delete.** Callers who care about "explicitly set
  to zero vs never set" must check `exists`, not just `value`.

## See also

- [`wire-protocol.md`](wire-protocol.md) — narrative of the wire format
- [`object-types.md`](object-types.md) — state-shape details per type
- [`demo-widgets.md`](demo-widgets.md) — copy-paste demo scripts for the UI widgets
- [`clients.md`](clients.md) — per-language client usage
