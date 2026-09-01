package memory

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/chris/event_watch/internal/core"
	"github.com/chris/event_watch/internal/store"
)

func TestAppendAssignsMonotonicSeq(t *testing.T) {
	s := New()
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

func TestReadFromSeqAndLimit(t *testing.T) {
	s := New()
	ctx := context.Background()
	for i := 0; i < 10; i++ {
		_, _ = s.Append(ctx, &core.Event{Topic: "t/x", Type: "e"})
	}
	events, _ := s.Read(ctx, "t/x", 4, 3)
	if len(events) != 3 {
		t.Fatalf("len=%d, want 3", len(events))
	}
	if events[0].Seq != 4 {
		t.Fatalf("first seq=%d, want 4", events[0].Seq)
	}
	if events[2].Seq != 6 {
		t.Fatalf("last seq=%d, want 6", events[2].Seq)
	}
}

func TestLatestReturnsTail(t *testing.T) {
	s := New()
	ctx := context.Background()
	for i := 0; i < 10; i++ {
		_, _ = s.Append(ctx, &core.Event{Topic: "t/x", Type: "e"})
	}
	events, _ := s.Latest(ctx, "t/x", 3)
	if len(events) != 3 || events[0].Seq != 8 || events[2].Seq != 10 {
		t.Fatalf("Latest wrong: %+v", events)
	}
}

func TestStateRoundTrip(t *testing.T) {
	s := New()
	ctx := context.Background()
	want := json.RawMessage(`{"foo":1}`)
	if err := s.SetState(ctx, "t/x", want); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetState(ctx, "t/x")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("got %s, want %s", got, want)
	}
	if _, err := s.GetState(ctx, "does/not/exist"); err != store.ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestExpire(t *testing.T) {
	s := New()
	ctx := context.Background()
	_, _ = s.Append(ctx, &core.Event{Topic: "t/old", Type: "e", OccurredAt: time.Now().Add(-1 * time.Hour)})
	_, _ = s.Append(ctx, &core.Event{Topic: "t/fresh", Type: "e", OccurredAt: time.Now()})
	n, err := s.Expire(ctx, time.Now().Add(-30*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expired=%d, want 1", n)
	}
	topics, _ := s.ListTopics(ctx, "", 0)
	if len(topics) != 1 || topics[0] != "t/fresh" {
		t.Fatalf("remaining topics wrong: %v", topics)
	}
}

func TestConcurrentAppendMonotonic(t *testing.T) {
	s := New()
	ctx := context.Background()
	var wg sync.WaitGroup
	const N = 200
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = s.Append(ctx, &core.Event{Topic: "t/x", Type: "e"})
		}()
	}
	wg.Wait()
	events, _ := s.Read(ctx, "t/x", 0, 0)
	if len(events) != N {
		t.Fatalf("len=%d, want %d", len(events), N)
	}
	for i, e := range events {
		if e.Seq != uint64(i+1) {
			t.Fatalf("events[%d].Seq=%d, want %d", i, e.Seq, i+1)
		}
	}
}
