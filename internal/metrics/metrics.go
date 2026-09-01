// Package metrics defines the process-wide Prometheus registry and exposes
// helpers that keep label cardinality low (object_type only, never per-topic).
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	dto "github.com/prometheus/client_model/go"
)

var (
	ConnectedClients = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "ew_connected_clients", Help: "Number of WebSocket clients currently connected.",
	})
	Subscriptions = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ew_subscriptions", Help: "Active server-side subscriptions by object type.",
	}, []string{"object_type"})
	EventsIngested = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "ew_events_ingested_total", Help: "Events accepted for storage and fan-out.",
	}, []string{"object_type", "source"})
	EventsFannedOut = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "ew_events_fanned_out_total", Help: "Successful hub deliveries to subscribers.",
	}, []string{"object_type"})
	DroppedSlowConsumer = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "ew_dropped_slow_consumer_total", Help: "Events dropped because a subscriber's buffer was full.",
	}, []string{"object_type"})
	StoreOpDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "ew_store_op_duration_seconds", Help: "Store operation latencies.",
		Buckets: prometheus.ExponentialBuckets(0.0001, 2, 14),
	}, []string{"op"})
	WSPingLag = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name: "ew_ws_ping_lag_seconds", Help: "Round-trip lag on WS ping/pong.",
		Buckets: prometheus.ExponentialBuckets(0.001, 2, 14),
	})
	TopicsTotal = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "ew_topics_total", Help: "Number of live topics in the store.",
	})
)

func init() {
	prometheus.MustRegister(
		ConnectedClients,
		Subscriptions,
		EventsIngested,
		EventsFannedOut,
		DroppedSlowConsumer,
		StoreOpDuration,
		WSPingLag,
		TopicsTotal,
	)
}

// Handler returns the Prometheus text-format handler.
func Handler() http.Handler { return promhttp.Handler() }

// Snapshot is the JSON representation exposed at /admin/metrics.json for the
// htmx UI. Not the same shape as the Prometheus format; it's a small
// hand-rolled dashboard payload.
type Snapshot struct {
	ConnectedClients     float64            `json:"connected_clients"`
	SubscriptionsByType  map[string]float64 `json:"subscriptions_by_type"`
	IngestedByType       map[string]float64 `json:"ingested_by_type"`
	FannedOutByType      map[string]float64 `json:"fanned_out_by_type"`
	DroppedByType        map[string]float64 `json:"dropped_by_type"`
	Topics               float64            `json:"topics"`
}

func Read() Snapshot {
	snap := Snapshot{
		SubscriptionsByType: map[string]float64{},
		IngestedByType:      map[string]float64{},
		FannedOutByType:     map[string]float64{},
		DroppedByType:       map[string]float64{},
	}
	gatherers := prometheus.Gatherer(prometheus.DefaultGatherer)
	mfs, err := gatherers.Gather()
	if err != nil {
		return snap
	}
	for _, mf := range mfs {
		switch mf.GetName() {
		case "ew_connected_clients":
			for _, m := range mf.Metric {
				snap.ConnectedClients = m.GetGauge().GetValue()
			}
		case "ew_topics_total":
			for _, m := range mf.Metric {
				snap.Topics = m.GetGauge().GetValue()
			}
		case "ew_subscriptions":
			for _, m := range mf.Metric {
				snap.SubscriptionsByType[labelVal(m.Label, "object_type")] = m.GetGauge().GetValue()
			}
		case "ew_events_ingested_total":
			for _, m := range mf.Metric {
				k := labelVal(m.Label, "object_type") + "/" + labelVal(m.Label, "source")
				snap.IngestedByType[k] += m.GetCounter().GetValue()
			}
		case "ew_events_fanned_out_total":
			for _, m := range mf.Metric {
				snap.FannedOutByType[labelVal(m.Label, "object_type")] += m.GetCounter().GetValue()
			}
		case "ew_dropped_slow_consumer_total":
			for _, m := range mf.Metric {
				snap.DroppedByType[labelVal(m.Label, "object_type")] += m.GetCounter().GetValue()
			}
		}
	}
	return snap
}

func labelVal(labels []*dto.LabelPair, name string) string {
	for _, l := range labels {
		if l.GetName() == name {
			return l.GetValue()
		}
	}
	return ""
}
