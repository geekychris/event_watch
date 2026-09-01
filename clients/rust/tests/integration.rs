//! Integration tests. Require a running event_watch server on :8080
//! (override with EW_URL env var). Skipped silently if the dial fails.

use eventwatch::Client;
use std::sync::atomic::{AtomicI64, Ordering};
use std::sync::Arc;
use tokio::time::{sleep, Duration};

fn server_url() -> String {
    std::env::var("EW_URL").unwrap_or_else(|_| "ws://localhost:8080/ws".to_string())
}

async fn dial() -> Option<Client> {
    match Client::dial(&server_url()).await {
        Ok(c) => Some(c),
        Err(_) => {
            eprintln!("event_watch server not reachable — skipping");
            None
        }
    }
}

#[tokio::test]
async fn int_field_arithmetic() {
    let Some(c) = dial().await else { return };
    let f = c.int_field("int/rusttest/counter");
    f.set(100).await.unwrap();
    f.incr(5).await.unwrap();
    f.incr(1).await.unwrap();
    f.decr(4).await.unwrap();
    let (v, ok) = f.get().await.unwrap();
    assert_eq!(v, 102);
    assert!(ok);
}

#[tokio::test]
async fn string_field_set_delete() {
    let Some(c) = dial().await else { return };
    let f = c.string_field("str/rusttest/name");
    f.set("alice").await.unwrap();
    let (v, ok) = f.get().await.unwrap();
    assert_eq!(v, "alice");
    assert!(ok);
    f.delete().await.unwrap();
    let (v, ok) = f.get().await.unwrap();
    assert_eq!(v, "");
    assert!(!ok);
}

#[tokio::test]
async fn subscribe_receives_events_with_state() {
    let Some(c) = dial().await else { return };
    let last = Arc::new(AtomicI64::new(0));
    let count = Arc::new(AtomicI64::new(0));
    let last_c = last.clone();
    let count_c = count.clone();

    let h = c
        .subscribe("int/rusttest/live", "latest", move |ev| {
            if let Some(state) = &ev.state {
                if let Some(v) = state.get("value").and_then(|x| x.as_i64()) {
                    last_c.store(v, Ordering::SeqCst);
                }
            }
            count_c.fetch_add(1, Ordering::SeqCst);
        })
        .await
        .unwrap();

    sleep(Duration::from_millis(100)).await;
    let f = c.int_field("int/rusttest/live");
    f.set(10).await.unwrap();
    f.incr(5).await.unwrap();
    f.decr(2).await.unwrap();

    for _ in 0..30 {
        if count.load(Ordering::SeqCst) >= 3 { break }
        sleep(Duration::from_millis(50)).await;
    }
    assert_eq!(count.load(Ordering::SeqCst), 3, "expected 3 events");
    assert_eq!(last.load(Ordering::SeqCst), 13, "final value should be 13");
    h.close().await;
}
