# eventwatch — Rust

Tokio + tokio-tungstenite client for event_watch. Same API surface as the Go
client (Connect, Subscribe, Publish, GetState, Fields).

## Use

```toml
# Cargo.toml
[dependencies]
eventwatch = { path = "clients/rust" }
tokio = { version = "1", features = ["macros", "rt-multi-thread"] }
```

```rust
use eventwatch::Client;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    let c = Client::dial("ws://localhost:8080/ws").await?;
    let counter = c.int_field("int/counter");
    counter.set(100).await?;
    counter.incr(5).await?;          // → 105
    let (v, exists) = counter.get().await?;
    println!("value={v} exists={exists}");

    let handle = c.subscribe("int/counter", "latest", |ev| {
        println!("{} seq={} state={:?}", ev.event_type, ev.seq, ev.state);
    }).await?;
    counter.incr(1).await?;
    tokio::time::sleep(std::time::Duration::from_millis(200)).await;
    handle.close().await;
    Ok(())
}
```

## Test

Requires event_watch running on `:8080`.

```bash
cd clients/rust
cargo test
```
