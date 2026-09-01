package memory

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/chris/event_watch/internal/core"
	"github.com/chris/event_watch/internal/store"
)

type topicData struct {
	events []*core.Event
	state  json.RawMessage
	meta   core.TopicMeta
}

type Store struct {
	mu     sync.RWMutex
	topics map[string]*topicData
}

func New() *Store {
	return &Store{topics: make(map[string]*topicData)}
}

func (s *Store) get(topic string) *topicData {
	td, ok := s.topics[topic]
	if !ok {
		td = &topicData{meta: core.TopicMeta{Topic: topic, CreatedAt: time.Now()}}
		s.topics[topic] = td
	}
	return td
}

func (s *Store) Append(ctx context.Context, e *core.Event) (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	td := s.get(e.Topic)
	seq := uint64(len(td.events)) + 1
	e.Seq = seq
	if e.ID == "" {
		e.ID = core.NewID()
	}
	if e.OccurredAt.IsZero() {
		e.OccurredAt = time.Now()
	}
	td.events = append(td.events, e)
	td.meta.LastEventAt = e.OccurredAt
	td.meta.LastSeq = seq
	if td.meta.ObjectType == "" {
		if ot, err := core.ParseObjectType(e.Topic); err == nil {
			td.meta.ObjectType = ot
		}
	}
	return seq, nil
}

func (s *Store) Read(ctx context.Context, topic string, fromSeq uint64, limit int) ([]*core.Event, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	td, ok := s.topics[topic]
	if !ok {
		return nil, nil
	}
	// events are seq-ordered by construction; binary search on Seq.
	i := sort.Search(len(td.events), func(i int) bool { return td.events[i].Seq >= fromSeq })
	out := td.events[i:]
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	// copy slice header so caller can't mutate our backing array
	cp := make([]*core.Event, len(out))
	copy(cp, out)
	return cp, nil
}

func (s *Store) Latest(ctx context.Context, topic string, n int) ([]*core.Event, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	td, ok := s.topics[topic]
	if !ok {
		return nil, nil
	}
	start := 0
	if n > 0 && len(td.events) > n {
		start = len(td.events) - n
	}
	out := td.events[start:]
	cp := make([]*core.Event, len(out))
	copy(cp, out)
	return cp, nil
}

func (s *Store) GetState(ctx context.Context, topic string) (json.RawMessage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	td, ok := s.topics[topic]
	if !ok || td.state == nil {
		return nil, store.ErrNotFound
	}
	cp := make(json.RawMessage, len(td.state))
	copy(cp, td.state)
	return cp, nil
}

func (s *Store) SetState(ctx context.Context, topic string, state json.RawMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	td := s.get(topic)
	cp := make(json.RawMessage, len(state))
	copy(cp, state)
	td.state = cp
	return nil
}

func (s *Store) GetMeta(ctx context.Context, topic string) (*core.TopicMeta, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	td, ok := s.topics[topic]
	if !ok {
		return nil, store.ErrNotFound
	}
	m := td.meta
	return &m, nil
}

func (s *Store) UpsertMeta(ctx context.Context, meta *core.TopicMeta) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	td := s.get(meta.Topic)
	// Preserve creation time and last-event fields set by Append.
	if td.meta.CreatedAt.IsZero() {
		td.meta.CreatedAt = time.Now()
	}
	if meta.ObjectType != "" {
		td.meta.ObjectType = meta.ObjectType
	}
	if meta.TTL != 0 {
		td.meta.TTL = meta.TTL
	}
	return nil
}

func (s *Store) ListTopics(ctx context.Context, prefix string, limit int) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.topics))
	for t := range s.topics {
		if prefix == "" || strings.HasPrefix(t, prefix) {
			out = append(out, t)
		}
	}
	sort.Strings(out)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *Store) Expire(ctx context.Context, cutoff time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for topic, td := range s.topics {
		if !td.meta.LastEventAt.IsZero() && td.meta.LastEventAt.Before(cutoff) {
			delete(s.topics, topic)
			n++
		}
	}
	return n, nil
}

func (s *Store) DeleteTopic(ctx context.Context, topic string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.topics, topic)
	return nil
}

func (s *Store) Close() error { return nil }
