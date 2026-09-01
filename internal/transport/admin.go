package transport

import (
	"net/http"
	"strconv"

	"github.com/chris/event_watch/internal/broker"
	"github.com/chris/event_watch/internal/metrics"
)

func NewStateHandler(b *broker.Broker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		topic := r.PathValue("topic")
		state, err := b.GetState(r.Context(), topic)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if state == nil {
			_, _ = w.Write([]byte("null"))
			return
		}
		_, _ = w.Write(state)
	}
}

func NewEventsHandler(b *broker.Broker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		topic := r.PathValue("topic")
		q := r.URL.Query()
		fromSeq, _ := strconv.ParseUint(q.Get("from_seq"), 10, 64)
		limit, _ := strconv.Atoi(q.Get("limit"))
		events, err := b.Events(r.Context(), topic, fromSeq, limit)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, map[string]any{"events": events})
	}
}

func NewTopicsHandler(b *broker.Broker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		prefix := r.URL.Query().Get("prefix")
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		topics, err := b.ListTopics(r.Context(), prefix, limit)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{"topics": topics})
	}
}

func NewMetricsSnapshotHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, metrics.Read())
	}
}
