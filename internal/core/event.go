package core

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"time"
)

// Event is the unit of information observers receive. Seq is monotonic per
// topic and is assigned by the Store on Append.
//
// State is populated only on live fan-out (broker sets it after reducer
// application). Historical reads via Store.Read / Store.Latest return events
// with State empty — the raw event stream is what's stored; state is a
// derived cache maintained per topic.
type Event struct {
	ID         string          `json:"id"`
	Topic      string          `json:"topic"`
	Type       string          `json:"type"`
	Seq        uint64          `json:"seq"`
	OccurredAt time.Time       `json:"occurred_at"`
	Actor      string          `json:"actor,omitempty"`
	Payload    json.RawMessage `json:"payload,omitempty"`
	State      json.RawMessage `json:"state,omitempty"`
}

// TopicMeta is per-topic bookkeeping. Written by the ingest path; read by the
// archiver and by clients enumerating topics.
type TopicMeta struct {
	Topic       string        `json:"topic"`
	ObjectType  string        `json:"object_type"`
	TTL         time.Duration `json:"ttl_ns"`
	CreatedAt   time.Time     `json:"created_at"`
	LastEventAt time.Time     `json:"last_event_at"`
	LastSeq     uint64        `json:"last_seq"`
}

// NewID returns a random 16-byte hex identifier. Not a ULID but adequate for
// idempotency + debug logging — Seq is what callers actually order by.
func NewID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
