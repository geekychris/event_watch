package transport

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/chris/event_watch/internal/broker"
	"github.com/chris/event_watch/internal/core"
)

// Field HTTP endpoints — sugar over POST /publish for scalar-field ops. All
// routes take {topic...} so nested keys like `str/counters/homepage` work.
// The endpoint chooses the event type and shapes the payload, then delegates
// to broker.Publish just like a normal publish.

func NewFieldStringSetHandler(b *broker.Broker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		topic := r.PathValue("topic")
		body, err := readJSONBody(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		val, _ := body["value"].(string)
		publishField(w, r, b, topic, "str_set", map[string]any{"value": val})
	}
}

func NewFieldSetHandler(b *broker.Broker) http.HandlerFunc {
	// Generic int_set / str_set / time_set dispatched by object type prefix.
	return func(w http.ResponseWriter, r *http.Request) {
		topic := r.PathValue("topic")
		ot, err := core.ParseObjectType(topic)
		if err != nil {
			http.Error(w, "invalid topic", http.StatusBadRequest)
			return
		}
		body, err := readJSONBody(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		typ := ot + "_set"
		publishField(w, r, b, topic, typ, body)
	}
}

func NewFieldIncrHandler(b *broker.Broker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		topic := r.PathValue("topic")
		body, _ := readJSONBody(r)
		delta := int64(1)
		if v, ok := body["delta"].(float64); ok {
			delta = int64(v)
		}
		publishField(w, r, b, topic, "int_incr", map[string]any{"delta": delta})
	}
}

func NewFieldDecrHandler(b *broker.Broker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		topic := r.PathValue("topic")
		body, _ := readJSONBody(r)
		delta := int64(1)
		if v, ok := body["delta"].(float64); ok {
			delta = int64(v)
		}
		publishField(w, r, b, topic, "int_decr", map[string]any{"delta": delta})
	}
}

func NewFieldDeleteHandler(b *broker.Broker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		topic := r.PathValue("topic")
		ot, err := core.ParseObjectType(topic)
		if err != nil {
			http.Error(w, "invalid topic", http.StatusBadRequest)
			return
		}
		publishField(w, r, b, topic, ot+"_delete", nil)
	}
}

func NewFieldTimeNowHandler(b *broker.Broker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		topic := r.PathValue("topic")
		publishField(w, r, b, topic, "time_now", nil)
	}
}

func NewFieldTimeAddHandler(b *broker.Broker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		topic := r.PathValue("topic")
		body, _ := readJSONBody(r)
		secs := int64(0)
		if v, ok := body["seconds"].(float64); ok {
			secs = int64(v)
		}
		publishField(w, r, b, topic, "time_add", map[string]any{"seconds": secs})
	}
}

// -- helpers --

func readJSONBody(r *http.Request) (map[string]any, error) {
	if r.ContentLength == 0 {
		return map[string]any{}, nil
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if len(body) == 0 {
		return map[string]any{}, nil
	}
	m := map[string]any{}
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}
	return m, nil
}

func publishField(w http.ResponseWriter, r *http.Request, b *broker.Broker, topic, typ string, payload map[string]any) {
	p, _ := json.Marshal(payload)
	e := &core.Event{Topic: topic, Type: typ, Payload: p, OccurredAt: time.Now()}
	seq, err := b.Publish(r.Context(), e, "field")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{"seq": seq, "topic": topic, "type": typ})
}
