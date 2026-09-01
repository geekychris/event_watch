// Package hub is the in-process fan-out registry. Subscribers receive a
// buffered channel of *core.Event; slow consumers (channel full) drop the
// event and get a Lag() count so the transport layer can send a "lagging"
// frame.
package hub

import (
	"sync"
	"sync/atomic"

	"github.com/chris/event_watch/internal/core"
)

const DefaultBuffer = 256

// Subscription is one observer of one topic. Close() removes it from the hub;
// consumers should select on both C and Done() so they wake up on close.
// Note: C is intentionally never closed — publishers may hold a snapshot of
// subs that raced with Close, so closing the channel could panic-on-send.
type Subscription struct {
	Topic string
	C     chan *core.Event

	hub    *Hub
	id     uint64
	done   chan struct{}
	closed atomic.Bool
	lag    atomic.Uint64
}

func (s *Subscription) Lag() uint64          { return s.lag.Load() }
func (s *Subscription) Done() <-chan struct{} { return s.done }

func (s *Subscription) Close() {
	if s.closed.Swap(true) {
		return
	}
	s.hub.remove(s.Topic, s.id)
	close(s.done)
}

type Hub struct {
	mu     sync.RWMutex
	next   uint64
	topics map[string]map[uint64]*Subscription
	buf    int

	// OnDrop is called (non-blocking) each time an event is dropped for a
	// slow subscriber. Servers use this to increment a metric.
	OnDrop func(topic string)
}

func New() *Hub {
	return &Hub{topics: make(map[string]map[uint64]*Subscription), buf: DefaultBuffer}
}

func (h *Hub) SubscriberCount(topic string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.topics[topic])
}

func (h *Hub) Subscribe(topic string) *Subscription {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.next++
	sub := &Subscription{
		Topic: topic,
		C:     make(chan *core.Event, h.buf),
		done:  make(chan struct{}),
		hub:   h,
		id:    h.next,
	}
	subs, ok := h.topics[topic]
	if !ok {
		subs = make(map[uint64]*Subscription)
		h.topics[topic] = subs
	}
	subs[sub.id] = sub
	return sub
}

func (h *Hub) remove(topic string, id uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	subs, ok := h.topics[topic]
	if !ok {
		return
	}
	delete(subs, id)
	if len(subs) == 0 {
		delete(h.topics, topic)
	}
}

// Publish fans e out to every subscription for e.Topic. Non-blocking: a
// subscription whose channel is full has the event dropped and its Lag
// counter incremented.
func (h *Hub) Publish(e *core.Event) {
	h.mu.RLock()
	subs := h.topics[e.Topic]
	// Snapshot subs so we don't hold the lock during sends.
	list := make([]*Subscription, 0, len(subs))
	for _, s := range subs {
		list = append(list, s)
	}
	h.mu.RUnlock()

	for _, s := range list {
		if s.closed.Load() {
			continue
		}
		select {
		case s.C <- e:
		case <-s.done:
		default:
			s.lag.Add(1)
			if h.OnDrop != nil {
				h.OnDrop(e.Topic)
			}
		}
	}
}
