// Package store defines the persistence interface for event streams,
// per-topic computed state, and topic metadata. Two implementations ship:
// memory (default, zero-dep) and redis.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/chris/event_watch/internal/core"
)

var ErrNotFound = errors.New("not found")

type Store interface {
	// Append persists e and returns the assigned monotonic per-topic seq.
	// The caller is responsible for having set OccurredAt, Topic, Type and
	// (optionally) Payload; Seq and ID are assigned here if unset.
	Append(ctx context.Context, e *core.Event) (uint64, error)

	// Read returns up to limit events with Seq >= fromSeq, oldest first.
	Read(ctx context.Context, topic string, fromSeq uint64, limit int) ([]*core.Event, error)

	// Latest returns the most recent n events for topic, oldest first.
	Latest(ctx context.Context, topic string, n int) ([]*core.Event, error)

	GetState(ctx context.Context, topic string) (json.RawMessage, error)
	SetState(ctx context.Context, topic string, state json.RawMessage) error

	GetMeta(ctx context.Context, topic string) (*core.TopicMeta, error)
	UpsertMeta(ctx context.Context, meta *core.TopicMeta) error

	ListTopics(ctx context.Context, prefix string, limit int) ([]string, error)

	// Expire deletes topics whose LastEventAt is strictly before cutoff and
	// returns the number of topics removed.
	Expire(ctx context.Context, cutoff time.Time) (int, error)

	// DeleteTopic removes a single topic (stream + state + meta). Idempotent.
	DeleteTopic(ctx context.Context, topic string) error

	Close() error
}
