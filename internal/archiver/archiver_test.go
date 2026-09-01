package archiver

import (
	"context"
	"testing"
	"time"

	"github.com/chris/event_watch/internal/core"
	memstore "github.com/chris/event_watch/internal/store/memory"
)

func TestSweep_ExpiresOldTopics(t *testing.T) {
	s := memstore.New()
	ctx := context.Background()
	old := &core.Event{Topic: "chat/old", Type: "msg_posted", OccurredAt: time.Now().Add(-2 * time.Hour)}
	fresh := &core.Event{Topic: "chat/fresh", Type: "msg_posted", OccurredAt: time.Now()}
	_, _ = s.Append(ctx, old)
	_, _ = s.Append(ctx, fresh)

	a := &Archiver{Store: s, DefaultTTL: time.Hour}
	if n := a.sweep(ctx); n != 1 {
		t.Fatalf("expected 1 expired; got %d", n)
	}
	topics, _ := s.ListTopics(ctx, "", 0)
	if len(topics) != 1 || topics[0] != "chat/fresh" {
		t.Fatalf("expected only chat/fresh to remain; got %v", topics)
	}
}

func TestSweep_PerTopicTTL_BeatsDefault(t *testing.T) {
	s := memstore.New()
	ctx := context.Background()
	// A topic whose per-topic TTL (1s) has expired even though the default
	// TTL (24h) hasn't.
	e := &core.Event{Topic: "job/x", Type: "job_started", OccurredAt: time.Now().Add(-1 * time.Minute)}
	_, _ = s.Append(ctx, e)
	_ = s.UpsertMeta(ctx, &core.TopicMeta{Topic: "job/x", ObjectType: "job", TTL: time.Second})

	a := &Archiver{Store: s, DefaultTTL: 24 * time.Hour}
	if n := a.sweep(ctx); n != 1 {
		t.Fatalf("expected 1 expired via per-topic TTL; got %d", n)
	}
	topics, _ := s.ListTopics(ctx, "", 0)
	if len(topics) != 0 {
		t.Fatalf("expected 0 topics; got %v", topics)
	}
}

func TestSweep_KeepsFreshTopics(t *testing.T) {
	s := memstore.New()
	ctx := context.Background()
	_, _ = s.Append(ctx, &core.Event{Topic: "chat/y", Type: "msg_posted", OccurredAt: time.Now()})
	a := &Archiver{Store: s, DefaultTTL: time.Hour}
	if n := a.sweep(ctx); n != 0 {
		t.Fatalf("expected 0 expired; got %d", n)
	}
	if got, _ := s.ListTopics(ctx, "", 0); len(got) != 1 {
		t.Fatalf("topic should remain: %v", got)
	}
}

func TestSweep_Idempotent(t *testing.T) {
	s := memstore.New()
	ctx := context.Background()
	_, _ = s.Append(ctx, &core.Event{Topic: "chat/old", Type: "msg_posted", OccurredAt: time.Now().Add(-2 * time.Hour)})
	a := &Archiver{Store: s, DefaultTTL: time.Hour}
	_ = a.sweep(ctx)
	// A second sweep on the now-empty store should be a no-op, not a crash.
	if n := a.sweep(ctx); n != 0 {
		t.Fatalf("second sweep expired %d; want 0", n)
	}
}
