//! Rust client for the event_watch pub/sub service.
//!
//! ```no_run
//! # async fn demo() -> Result<(), Box<dyn std::error::Error + Send + Sync>> {
//! use eventwatch::Client;
//! let c = Client::dial("ws://localhost:8080/ws").await?;
//! let counter = c.int_field("int/counter");
//! counter.set(100).await?;
//! counter.incr(5).await?;
//! let (v, exists) = counter.get().await?;
//! println!("value={v} exists={exists}");
//!
//! let handle = c.subscribe("int/counter", "latest", |ev| {
//!     println!("{} seq={} state={:?}", ev.event_type, ev.seq, ev.state);
//! }).await?;
//! counter.incr(1).await?;
//! tokio::time::sleep(std::time::Duration::from_millis(200)).await;
//! handle.close().await;
//! # Ok(()) }
//! ```

use futures_util::{SinkExt, StreamExt};
use serde::{Deserialize, Serialize};
use serde_json::{json, Value};
use std::collections::HashMap;
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::Arc;
use std::time::Duration;
use tokio::sync::{oneshot, Mutex};
use tokio_tungstenite::{
    connect_async,
    tungstenite::{client::IntoClientRequest, Message},
};

pub mod field;
pub use field::{IntField, StringField, TimeField};

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Event {
    pub id: String,
    pub topic: String,
    #[serde(rename = "type")]
    pub event_type: String,
    pub seq: u64,
    pub occurred_at: String,
    #[serde(default)]
    pub actor: String,
    #[serde(default)]
    pub payload: Option<Value>,
    #[serde(default)]
    pub state: Option<Value>,
}

pub type Callback = Arc<dyn Fn(&Event) + Send + Sync>;

struct TopicSub {
    from_kind: String,
    callbacks: HashMap<u64, Callback>,
    last_seen_seq: u64,
}

struct Shared {
    topics: Mutex<HashMap<String, TopicSub>>,
    pending: Mutex<HashMap<String, oneshot::Sender<Result<Value, String>>>>,
    send_tx: tokio::sync::mpsc::UnboundedSender<Message>,
    next_id: AtomicU64,
    next_req: AtomicU64,
}

#[derive(Clone)]
pub struct Client {
    shared: Arc<Shared>,
}

pub struct Handle {
    shared: Arc<Shared>,
    topic: String,
    id: u64,
}

impl Handle {
    /// Remove this callback. If it was the last on the topic, sends an
    /// unsubscribe frame upstream.
    pub async fn close(self) {
        let mut topics = self.shared.topics.lock().await;
        if let Some(ts) = topics.get_mut(&self.topic) {
            ts.callbacks.remove(&self.id);
            if ts.callbacks.is_empty() {
                topics.remove(&self.topic);
                drop(topics);
                let _ = self
                    .shared
                    .send_tx
                    .send(Message::Text(
                        json!({"op":"unsubscribe","topic": self.topic}).to_string().into(),
                    ));
            }
        }
    }
}

impl Client {
    /// Dial the server and start the read/write loops.
    pub async fn dial(url: &str) -> Result<Self, Box<dyn std::error::Error + Send + Sync>> {
        Self::dial_with_token(url, "").await
    }

    pub async fn dial_with_token(
        url: &str,
        token: &str,
    ) -> Result<Self, Box<dyn std::error::Error + Send + Sync>> {
        let mut target = url.to_string();
        if !token.is_empty() {
            let sep = if target.contains('?') { "&" } else { "?" };
            target = format!("{target}{sep}access_token={token}");
        }
        let mut req = target.into_client_request()?;
        if !token.is_empty() {
            req.headers_mut()
                .insert("Authorization", format!("Bearer {token}").parse().unwrap());
        }
        let (ws, _) = connect_async(req).await?;
        let (mut writer, mut reader) = ws.split();

        let (send_tx, mut send_rx) = tokio::sync::mpsc::unbounded_channel::<Message>();
        let shared = Arc::new(Shared {
            topics: Mutex::new(HashMap::new()),
            pending: Mutex::new(HashMap::new()),
            send_tx,
            next_id: AtomicU64::new(0),
            next_req: AtomicU64::new(0),
        });

        // Writer task.
        tokio::spawn(async move {
            while let Some(m) = send_rx.recv().await {
                if writer.send(m).await.is_err() {
                    break;
                }
            }
        });

        // Reader task.
        let sh = shared.clone();
        tokio::spawn(async move {
            while let Some(msg) = reader.next().await {
                let Ok(m) = msg else { break };
                if let Message::Text(t) = m {
                    if let Ok(v) = serde_json::from_str::<Value>(&t) {
                        dispatch(&sh, v).await;
                    }
                }
            }
        });

        Ok(Client { shared })
    }

    /// Register cb for events on topic. Multiple subscribes share one upstream
    /// subscription; Handle.close() decrements the local refcount and unsubscribes
    /// when the last handle goes away.
    pub async fn subscribe<F>(
        &self,
        topic: &str,
        from_kind: &str,
        cb: F,
    ) -> Result<Handle, Box<dyn std::error::Error + Send + Sync>>
    where
        F: Fn(&Event) + Send + Sync + 'static,
    {
        let id = self.shared.next_id.fetch_add(1, Ordering::SeqCst) + 1;
        let mut topics = self.shared.topics.lock().await;
        let first = !topics.contains_key(topic);
        let ts = topics.entry(topic.to_string()).or_insert_with(|| TopicSub {
            from_kind: from_kind.to_string(),
            callbacks: HashMap::new(),
            last_seen_seq: 0,
        });
        ts.callbacks.insert(id, Arc::new(cb));
        if first {
            self.shared.send_tx.send(Message::Text(
                subscribe_frame(topic, from_kind, 0).to_string().into(),
            ))?;
        }
        Ok(Handle {
            shared: self.shared.clone(),
            topic: topic.to_string(),
            id,
        })
    }

    /// Publish one event. Returns the assigned seq.
    pub async fn publish(
        &self,
        topic: &str,
        event_type: &str,
        payload: Option<Value>,
    ) -> Result<u64, Box<dyn std::error::Error + Send + Sync>> {
        let mut frame = json!({"op":"publish","topic":topic,"type":event_type});
        if let Some(p) = payload {
            frame["payload"] = p;
        }
        let resp = self.request(frame).await?;
        Ok(resp
            .get("last_seq")
            .and_then(|v| v.as_u64())
            .unwrap_or(0))
    }

    /// Current computed state, or None if the topic has no state yet.
    pub async fn get_state(
        &self,
        topic: &str,
    ) -> Result<Option<Value>, Box<dyn std::error::Error + Send + Sync>> {
        let frame = json!({"op":"get_state","topic":topic});
        match self.request(frame).await {
            Ok(resp) => Ok(resp.get("state").cloned()),
            Err(e) => {
                if e.to_string().to_lowercase().contains("not found") {
                    Ok(None)
                } else {
                    Err(e)
                }
            }
        }
    }

    async fn request(
        &self,
        mut frame: Value,
    ) -> Result<Value, Box<dyn std::error::Error + Send + Sync>> {
        let n = self.shared.next_req.fetch_add(1, Ordering::SeqCst) + 1;
        let req_id = format!("r{n}");
        frame["req_id"] = json!(req_id);
        let (tx, rx) = oneshot::channel::<Result<Value, String>>();
        self.shared.pending.lock().await.insert(req_id.clone(), tx);
        self.shared
            .send_tx
            .send(Message::Text(frame.to_string().into()))?;
        match tokio::time::timeout(Duration::from_secs(10), rx).await {
            Ok(Ok(Ok(v))) => Ok(v),
            Ok(Ok(Err(e))) => Err(e.into()),
            _ => {
                self.shared.pending.lock().await.remove(&req_id);
                Err("request timed out".into())
            }
        }
    }

    pub fn int_field(&self, topic: &str) -> IntField {
        IntField::new(self.clone(), topic.to_string())
    }
    pub fn string_field(&self, topic: &str) -> StringField {
        StringField::new(self.clone(), topic.to_string())
    }
    pub fn time_field(&self, topic: &str) -> TimeField {
        TimeField::new(self.clone(), topic.to_string())
    }
}

fn subscribe_frame(topic: &str, from_kind: &str, resume_seq: u64) -> Value {
    let mut f = json!({"op":"subscribe","topic":topic});
    if resume_seq > 0 {
        f["from_seq"] = json!(resume_seq);
    } else if from_kind.starts_with("last:") || from_kind.starts_with("seq:") {
        f["from"] = json!(from_kind);
    } else {
        f["from"] = json!("latest");
    }
    f
}

async fn dispatch(shared: &Arc<Shared>, frame: Value) {
    let typ = frame.get("type").and_then(|v| v.as_str()).unwrap_or("");
    match typ {
        "event" => {
            if let Some(ev_val) = frame.get("event") {
                if let Ok(ev) = serde_json::from_value::<Event>(ev_val.clone()) {
                    let mut topics = shared.topics.lock().await;
                    if let Some(ts) = topics.get_mut(&ev.topic) {
                        if ev.seq > ts.last_seen_seq {
                            ts.last_seen_seq = ev.seq;
                        }
                        let cbs: Vec<Callback> = ts.callbacks.values().cloned().collect();
                        drop(topics);
                        for cb in cbs {
                            cb(&ev);
                        }
                    }
                }
            }
        }
        "state" | "ack" | "error" => {
            if let Some(rid) = frame.get("req_id").and_then(|v| v.as_str()) {
                if let Some(tx) = shared.pending.lock().await.remove(rid) {
                    if typ == "error" {
                        let msg = frame
                            .get("message")
                            .and_then(|v| v.as_str())
                            .unwrap_or("error")
                            .to_string();
                        let _ = tx.send(Err(msg));
                    } else {
                        let _ = tx.send(Ok(frame));
                    }
                }
            }
        }
        _ => {}
    }
}
