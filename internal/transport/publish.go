package transport

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/chris/event_watch/internal/broker"
	"github.com/chris/event_watch/internal/core"
	"github.com/chris/event_watch/internal/webhook"
)

type publishReq struct {
	Topic   string          `json:"topic"`
	Type    string          `json:"type"`
	Actor   string          `json:"actor,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type publishResp struct {
	ID    string `json:"id"`
	Seq   uint64 `json:"seq"`
	Topic string `json:"topic"`
}

// NewPublishHandler returns POST /publish, ingesting a single event.
func NewPublishHandler(b *broker.Broker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var req publishReq
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		e := &core.Event{Topic: req.Topic, Type: req.Type, Actor: req.Actor, Payload: req.Payload, OccurredAt: time.Now()}
		seq, err := b.Publish(r.Context(), e, "publish")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(publishResp{ID: e.ID, Seq: seq, Topic: e.Topic})
	}
}

// NewWebhookHandler returns POST /webhook/{plugin}. The plugin decides
// signature verification and event mapping.
func NewWebhookHandler(b *broker.Broker, reg *webhook.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name := r.PathValue("plugin")
		p, ok := reg.Get(name)
		if !ok {
			http.Error(w, "unknown plugin", http.StatusNotFound)
			return
		}
		if err := p.Verify(r); err != nil {
			http.Error(w, "signature invalid", http.StatusUnauthorized)
			return
		}
		events, err := p.Transform(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		for _, e := range events {
			if e.OccurredAt.IsZero() {
				e.OccurredAt = time.Now()
			}
			if _, err := b.Publish(r.Context(), e, "webhook"); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}
		w.WriteHeader(http.StatusAccepted)
	}
}
