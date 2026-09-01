//go:build redis

// Integration tests for the Redis store. Run with:
//
//   docker compose up -d redis
//   REDIS_ADDR=localhost:6379 go test -tags=redis ./internal/store/redis/...
//
// Each test uses a distinct DB index to avoid stepping on other tests running
// in parallel; if you point at an external Redis be aware these tests FLUSHDB
// their target DB before running.
package redis

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/chris/event_watch/internal/core"
	"github.com/chris/event_watch/internal/store"
)

func addrOrSkip(t *testing.T) string {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		t.Skip("REDIS_ADDR not set")
	}
	return addr
}

func newTestStore(t *testing.T, db int) *Store {
	t.Helper()
	s, err := New(context.Background(), Options{Addr: addrOrSkip(t), DB: db})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := s.c.FlushDB(context.Background()).Err(); err != nil {
		t.Fatalf("flushdb: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestAppendAssignsMonotonicSeq(t *testing.T) {
	s := newTestStore(t, 15)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		seq, err := s.Append(ctx, &core.Event{Topic: "pr/o/r/1", Type: "x"})
		if err != nil {
			t.Fatal(err)
		}
		if seq != uint64(i+1) {
			t.Fatalf("seq=%d, want %d", seq, i+1)
		}
	}
}

func TestReadAndLatest(t *testing.T) {
	s := newTestStore(t, 15)
	ctx := context.Background()
	for i := 0; i < 10; i++ {
		_, _ = s.Append(ctx, &core.Event{Topic: "t/x", Type: "e"})
	}
	events, _ := s.Read(ctx, "t/x", 4, 3)
	if len(events) != 3 || events[0].Seq != 4 || events[2].Seq != 6 {
		t.Fatalf("Read wrong: %+v", events)
	}
	latest, _ := s.Latest(ctx, "t/x", 3)
	if len(latest) != 3 || latest[0].Seq != 8 || latest[2].Seq != 10 {
		t.Fatalf("Latest wrong: %+v", latest)
	}
}

func TestStateRoundTripAndNotFound(t *testing.T) {
	s := newTestStore(t, 15)
	ctx := context.Background()
	if _, err := s.GetState(ctx, "nope/x"); err != store.ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	want := json.RawMessage(`{"foo":1}`)
	if err := s.SetState(ctx, "t/x", want); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetState(ctx, "t/x")
	if err != nil || string(got) != string(want) {
		t.Fatalf("got=%s err=%v", got, err)
	}
}

func TestMetaRoundTrip(t *testing.T) {
	s := newTestStore(t, 15)
	ctx := context.Background()
	// Append writes object_type + last_seq + last_event_at into meta.
	_, _ = s.Append(ctx, &core.Event{Topic: "pr/o/r/1", Type: "pr_opened", OccurredAt: time.Now()})
	// UpsertMeta writes TTL.
	if err := s.UpsertMeta(ctx, &core.TopicMeta{Topic: "pr/o/r/1", TTL: 30 * time.Minute}); err != nil {
		t.Fatal(err)
	}
	meta, err := s.GetMeta(ctx, "pr/o/r/1")
	if err != nil {
		t.Fatal(err)
	}
	if meta.ObjectType != "pr" || meta.LastSeq != 1 {
		t.Fatalf("meta wrong: %+v", meta)
	}
	if meta.TTL != 30*time.Minute {
		t.Fatalf("TTL not persisted: %v", meta.TTL)
	}
	if meta.LastEventAt.IsZero() {
		t.Fatal("LastEventAt not persisted")
	}
}

func TestListTopicsFilterAndDelete(t *testing.T) {
	s := newTestStore(t, 15)
	ctx := context.Background()
	_, _ = s.Append(ctx, &core.Event{Topic: "pr/o/r/1", Type: "x"})
	_, _ = s.Append(ctx, &core.Event{Topic: "pr/o/r/2", Type: "x"})
	_, _ = s.Append(ctx, &core.Event{Topic: "chat/room", Type: "x"})

	prs, _ := s.ListTopics(ctx, "pr/", 0)
	if len(prs) != 2 {
		t.Fatalf("pr topics=%v", prs)
	}
	all, _ := s.ListTopics(ctx, "", 0)
	if len(all) != 3 {
		t.Fatalf("all topics=%v", all)
	}
	if err := s.DeleteTopic(ctx, "pr/o/r/1"); err != nil {
		t.Fatal(err)
	}
	after, _ := s.ListTopics(ctx, "pr/", 0)
	if len(after) != 1 || after[0] != "pr/o/r/2" {
		t.Fatalf("after delete: %v", after)
	}
	// DeleteTopic is idempotent.
	if err := s.DeleteTopic(ctx, "pr/o/r/1"); err != nil {
		t.Fatalf("second delete errored: %v", err)
	}
}

func TestExpireByCutoff(t *testing.T) {
	s := newTestStore(t, 15)
	ctx := context.Background()
	old := time.Now().Add(-2 * time.Hour)
	fresh := time.Now()
	_, _ = s.Append(ctx, &core.Event{Topic: "t/old", Type: "e", OccurredAt: old})
	_, _ = s.Append(ctx, &core.Event{Topic: "t/fresh", Type: "e", OccurredAt: fresh})

	n, err := s.Expire(ctx, time.Now().Add(-1*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expired=%d, want 1", n)
	}
	all, _ := s.ListTopics(ctx, "", 0)
	if len(all) != 1 || all[0] != "t/fresh" {
		t.Fatalf("remaining=%v", all)
	}
}

func TestConcurrentAppendMonotonic(t *testing.T) {
	s := newTestStore(t, 15)
	ctx := context.Background()
	const N = 100
	var wg sync.WaitGroup
	seqs := make([]uint64, N)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			seq, err := s.Append(ctx, &core.Event{Topic: "t/x", Type: "e"})
			if err != nil {
				t.Errorf("append: %v", err)
				return
			}
			seqs[i] = seq
		}(i)
	}
	wg.Wait()
	seen := make(map[uint64]bool, N)
	for _, s := range seqs {
		if s < 1 || s > N {
			t.Fatalf("seq out of range: %d", s)
		}
		if seen[s] {
			t.Fatalf("dup seq: %d", s)
		}
		seen[s] = true
	}
}
