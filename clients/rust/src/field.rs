//! Typed helpers over Publish + GetState for scalar-field topics.
//!
//! Nothing here is fundamentally different from calling `Client::publish` with
//! the right event type — the helpers just fix up the type strings and
//! payload shapes so callers can't get them wrong.

use crate::Client;
use serde_json::json;

pub type BoxError = Box<dyn std::error::Error + Send + Sync>;

pub struct StringField {
    c: Client,
    pub topic: String,
}

impl StringField {
    pub(crate) fn new(c: Client, topic: String) -> Self { Self { c, topic } }

    pub async fn set(&self, value: &str) -> Result<u64, BoxError> {
        self.c.publish(&self.topic, "str_set", Some(json!({"value": value}))).await
    }
    pub async fn delete(&self) -> Result<u64, BoxError> {
        self.c.publish(&self.topic, "str_delete", None).await
    }
    pub async fn get(&self) -> Result<(String, bool), BoxError> {
        let s = self.c.get_state(&self.topic).await?;
        match s {
            None => Ok((String::new(), false)),
            Some(v) => Ok((
                v.get("value").and_then(|x| x.as_str()).unwrap_or("").to_string(),
                v.get("exists").and_then(|x| x.as_bool()).unwrap_or(false),
            )),
        }
    }
}

pub struct IntField {
    c: Client,
    pub topic: String,
}

impl IntField {
    pub(crate) fn new(c: Client, topic: String) -> Self { Self { c, topic } }

    pub async fn set(&self, value: i64) -> Result<u64, BoxError> {
        self.c.publish(&self.topic, "int_set", Some(json!({"value": value}))).await
    }
    pub async fn incr(&self, delta: i64) -> Result<u64, BoxError> {
        self.c.publish(&self.topic, "int_incr", Some(json!({"delta": delta}))).await
    }
    pub async fn decr(&self, delta: i64) -> Result<u64, BoxError> {
        self.c.publish(&self.topic, "int_decr", Some(json!({"delta": delta}))).await
    }
    pub async fn delete(&self) -> Result<u64, BoxError> {
        self.c.publish(&self.topic, "int_delete", None).await
    }
    pub async fn get(&self) -> Result<(i64, bool), BoxError> {
        let s = self.c.get_state(&self.topic).await?;
        match s {
            None => Ok((0, false)),
            Some(v) => Ok((
                v.get("value").and_then(|x| x.as_i64()).unwrap_or(0),
                v.get("exists").and_then(|x| x.as_bool()).unwrap_or(false),
            )),
        }
    }
}

pub struct TimeField {
    c: Client,
    pub topic: String,
}

impl TimeField {
    pub(crate) fn new(c: Client, topic: String) -> Self { Self { c, topic } }

    /// value must be RFC3339 (e.g. "2026-01-01T00:00:00Z").
    pub async fn set_rfc3339(&self, value: &str) -> Result<u64, BoxError> {
        self.c.publish(&self.topic, "time_set", Some(json!({"value": value}))).await
    }
    pub async fn now(&self) -> Result<u64, BoxError> {
        self.c.publish(&self.topic, "time_now", None).await
    }
    pub async fn add(&self, seconds: i64) -> Result<u64, BoxError> {
        self.c.publish(&self.topic, "time_add", Some(json!({"seconds": seconds}))).await
    }
    pub async fn delete(&self) -> Result<u64, BoxError> {
        self.c.publish(&self.topic, "time_delete", None).await
    }
    /// Returns (rfc3339-string, exists). Parsing to chrono is left to callers.
    pub async fn get(&self) -> Result<(String, bool), BoxError> {
        let s = self.c.get_state(&self.topic).await?;
        match s {
            None => Ok((String::new(), false)),
            Some(v) => Ok((
                v.get("value").and_then(|x| x.as_str()).unwrap_or("").to_string(),
                v.get("exists").and_then(|x| x.as_bool()).unwrap_or(false),
            )),
        }
    }
}
