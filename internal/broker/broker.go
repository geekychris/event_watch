// Package broker composes store + hub + reducer registry. It's the only
// package that mutates state in the ingest path, so it serialises Append +
// Reducer + SetState + Hub.Publish per event to keep per-topic delivery order
// stable even under concurrent publishers.
package broker

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/chris/event_watch/internal/computed"
	"github.com/chris/event_watch/internal/core"
	"github.com/chris/event_watch/internal/hub"
	"github.com/chris/event_watch/internal/metrics"
	"github.com/chris/event_watch/internal/store"
)

type Broker struct {
	Store      store.Store
	Hub        *hub.Hub
	Reducers   *computed.Registry
	DefaultTTL time.Duration

	ingest sync.Mutex // serialises Publish across all topics (fine for v1)
}

func New(s store.Store, h *hub.Hub, r *computed.Registry, defaultTTL time.Duration) *Broker {
	b := &Broker{Store: s, Hub: h, Reducers: r, DefaultTTL: defaultTTL}
	h.OnDrop = func(topic string) {
		if ot, err := core.ParseObjectType(topic); err == nil {
			metrics.DroppedSlowConsumer.WithLabelValues(ot).Inc()
		}
	}
	return b
}

// Publish ingests a single event: validate, append, reduce, save state, fan
// out, and update metrics. Source is a low-cardinality tag ("publish",
// "webhook", ...) used only for metrics.
func (b *Broker) Publish(ctx context.Context, e *core.Event, source string) (uint64, error) {
	if err := core.ValidateTopic(e.Topic); err != nil {
		return 0, err
	}
	if e.Type == "" {
		return 0, errors.New("event type required")
	}
	ot, err := core.ParseObjectType(e.Topic)
	if err != nil {
		return 0, err
	}

	b.ingest.Lock()
	defer b.ingest.Unlock()

	seq, err := b.Store.Append(ctx, e)
	if err != nil {
		return 0, err
	}

	// Reduce and save new state (ignore ErrUnknownType — raw stream is enough).
	prev, _ := b.Store.GetState(ctx, e.Topic)
	next, rerr := b.Reducers.Apply(prev, e)
	if rerr == nil && next != nil {
		_ = b.Store.SetState(ctx, e.Topic, next)
	}

	// Meta upsert (object type + TTL).
	_ = b.Store.UpsertMeta(ctx, &core.TopicMeta{Topic: e.Topic, ObjectType: ot, TTL: b.DefaultTTL})

	// Attach the post-reduce state to the fanned-out event. The stored event
	// (already persisted above) does NOT carry this — state is a derived cache.
	// A subscriber gets event + current state in one frame so they can render
	// either without an extra round-trip.
	fanout := *e
	fanout.State = next
	b.Hub.Publish(&fanout)

	metrics.EventsIngested.WithLabelValues(ot, source).Inc()
	metrics.EventsFannedOut.WithLabelValues(ot).Add(float64(b.Hub.SubscriberCount(e.Topic)))
	return seq, nil
}

// Subscribe creates a hub subscription and returns backfill events consistent
// with `from`. `fenceSeq` is the max seq contained in the backfill; the caller
// should send backfill first, then drop any live events whose seq is <= fenceSeq
// (dedupe against events that raced between Read and Subscribe).
func (b *Broker) Subscribe(ctx context.Context, topic string, from core.From) (
	sub *hub.Subscription, backfill []*core.Event, fenceSeq uint64, err error,
) {
	if err := core.ValidateTopic(topic); err != nil {
		return nil, nil, 0, err
	}
	ot, _ := core.ParseObjectType(topic)

	// Subscribe BEFORE reading so no event is missed between the two.
	sub = b.Hub.Subscribe(topic)

	switch from.Kind {
	case core.FromLatest:
		// nothing to backfill.
	case core.FromLastN:
		backfill, err = b.Store.Latest(ctx, topic, int(from.Value))
	case core.FromSeq:
		backfill, err = b.Store.Read(ctx, topic, from.Value, 0)
	}
	if err != nil {
		sub.Close()
		return nil, nil, 0, err
	}
	if len(backfill) > 0 {
		fenceSeq = backfill[len(backfill)-1].Seq
	}

	metrics.Subscriptions.WithLabelValues(ot).Inc()
	return sub, backfill, fenceSeq, nil
}

// Unsubscribe closes the subscription and decrements the object-type gauge.
// Callers can also call sub.Close() directly; this wrapper just also does the
// metric.
func (b *Broker) Unsubscribe(sub *hub.Subscription) {
	if ot, err := core.ParseObjectType(sub.Topic); err == nil {
		metrics.Subscriptions.WithLabelValues(ot).Dec()
	}
	sub.Close()
}

func (b *Broker) GetState(ctx context.Context, topic string) (json.RawMessage, error) {
	return b.Store.GetState(ctx, topic)
}

func (b *Broker) Events(ctx context.Context, topic string, fromSeq uint64, limit int) ([]*core.Event, error) {
	return b.Store.Read(ctx, topic, fromSeq, limit)
}

func (b *Broker) Latest(ctx context.Context, topic string, n int) ([]*core.Event, error) {
	return b.Store.Latest(ctx, topic, n)
}

func (b *Broker) ListTopics(ctx context.Context, prefix string, limit int) ([]string, error) {
	return b.Store.ListTopics(ctx, prefix, limit)
}
