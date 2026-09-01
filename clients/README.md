# event_watch client libraries

Client libraries for the event_watch pub/sub server. Same wire protocol
(single WebSocket, JSON frames), seven different language / platform bindings.

| Client | Location | Tests | Full parity? |
|---|---|---|---|
| **Go** | `../client/` (top-level module) | 15 tests, `go test ./client/...` | ✅ everything |
| **Python** | `python/` | 4 pytest, `pytest -q` | ✅ everything |
| **Java** | `java/` (Maven, JDK 11+) | 3 JUnit, `mvn test` | ✅ everything |
| **Rust** | `rust/` (Cargo) | 3 tokio, `cargo test` | ✅ everything |
| **Browser (JS)** | `browser/` (`@eventwatch/browser`, ESM) | 7 node --test | ✅ everything |
| **ESP-IDF** | `esp-idf/eventwatch/` | example sketch | 🔸 no request/response, no refcount |
| **Arduino** | `arduino/eventwatch/` | example sketches | 🔸 no request/response, no refcount |

## Same wire protocol

Every client talks the WebSocket frames documented in the top-level plan:

```
→  {"op":"subscribe","topic":"int/counter","from":"latest"}
→  {"op":"publish","topic":"int/counter","type":"int_incr","payload":{"delta":5}}
→  {"op":"get_state","topic":"int/counter","req_id":"r1"}

←  {"type":"event","topic":"int/counter","event":{"type":"int_incr","seq":5,
     "payload":{"delta":5},"state":{"value":105,"exists":true}}}
←  {"type":"state","topic":"int/counter","state":{...},"req_id":"r1"}
←  {"type":"ack","topic":"int/counter","last_seq":5,"req_id":"r1"}
```

## Field arithmetic snippets

**Python**
```python
counter = c.int_field("int/counter")
await counter.set(100); await counter.incr(5); await counter.decr(3)
v, exists = await counter.get()  # (102, True)
```

**Java**
```java
IntField counter = c.intField("int/counter");
counter.set(100); counter.incr(5); counter.decr(3);
Object[] g = counter.get();  // { Long, Boolean }
```

**Rust**
```rust
let counter = c.int_field("int/counter");
counter.set(100).await?; counter.incr(5).await?; counter.decr(3).await?;
let (v, exists) = counter.get().await?;  // (102, true)
```

**ESP-IDF / Arduino** — fire-and-forget arithmetic; read current values via
the `state` param delivered on subscribe:
```c
eventwatch_int_incr(c, "int/esp32/uptime_seconds", 1);
```
```cpp
ew.intIncr("int/esp32/uptime_seconds", 1);
```

## What "full parity" means

- **Full parity** clients (Go / Python / Java / Rust) support: connect + reconnect
  with per-topic resume seq, subscribe (with refcounted local dispatch: N callbacks
  on one topic share one upstream subscription), publish (request/response over WS
  with an assigned seq), get_state (request/response), and typed field helpers
  with `.get()` returning `(value, exists)`.
- **MCU clients** (ESP-IDF / Arduino) skip refcounted dispatch (one callback for
  all subscribed topics), skip request/response over WS (no `get_state` — read
  the current value from the `state` param delivered inline with each event),
  and cap at ~8-16 concurrent subscriptions to stay small. They DO auto-reconnect
  and re-issue subscribes with `from_seq=lastSeq+1`.

## Running the tests

Every client's tests hit a live event_watch server on `ws://localhost:8080/ws`.
Start the server first (see the top-level README) then:

```bash
cd clients/python && pytest -q
cd clients/java   && mvn test
cd clients/rust   && cargo test
```

ESP-IDF and Arduino builds require their respective toolchains — see each
subdirectory's README for build instructions.
