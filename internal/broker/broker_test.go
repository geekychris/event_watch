package broker

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/chris/event_watch/internal/computed"
	"github.com/chris/event_watch/internal/core"
	"github.com/chris/event_watch/internal/hub"
	"github.com/chris/event_watch/internal/objtypes"
	memstore "github.com/chris/event_watch/internal/store/memory"
)

func newBroker() *Broker {
	reg := computed.NewRegistry()
	reg.Register(objtypes.PRReducer{}, objtypes.ChatReducer{})
	return New(memstore.New(), hub.New(), reg, time.Hour)
}

func TestPublish_AssignsMonotonicSeq_ConcurrentPublishers(t *testing.T) {
	b := newBroker()
	const N = 200
	var wg sync.WaitGroup
	seqs := make([]uint64, N)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			seq, err := b.Publish(context.Background(), &core.Event{
				Topic: "chat/x", Type: "msg_posted",
				Payload: json.RawMessage(`{"user":"u","text":"hi"}`),
			}, "test")
			if err != nil {
				t.Errorf("publish: %v", err)
				return
			}
			seqs[i] = seq
		}(i)
	}
	wg.Wait()
	// The set of assigned seqs must be exactly {1..N}, no duplicates and no gaps.
	seen := make(map[uint64]bool, N)
	for _, s := range seqs {
		if s < 1 || s > N {
			t.Fatalf("seq out of range: %d", s)
		}
		if seen[s] {
			t.Fatalf("duplicate seq: %d", s)
		}
		seen[s] = true
	}
	if len(seen) != N {
		t.Fatalf("expected %d unique seqs, got %d", N, len(seen))
	}
}

func TestPublish_InvokesReducerAndPersistsState(t *testing.T) {
	b := newBroker()
	_, err := b.Publish(context.Background(), &core.Event{
		Topic: "pr/o/r/1", Type: "pr_opened",
		Payload: json.RawMessage(`{"title":"T","author":"alice"}`),
	}, "test")
	if err != nil {
		t.Fatal(err)
	}
	state, err := b.GetState(context.Background(), "pr/o/r/1")
	if err != nil {
		t.Fatal(err)
	}
	var s objtypes.PRState
	if err := json.Unmarshal(state, &s); err != nil {
		t.Fatal(err)
	}
	if s.Title != "T" || s.State != "open" {
		t.Fatalf("state not reduced correctly: %+v", s)
	}
}

func TestPublish_RejectsInvalidTopic(t *testing.T) {
	b := newBroker()
	if _, err := b.Publish(context.Background(), &core.Event{Topic: "nope", Type: "x"}, "test"); err == nil {
		t.Fatal("expected invalid-topic error")
	}
}

func TestPublish_RejectsEmptyType(t *testing.T) {
	b := newBroker()
	if _, err := b.Publish(context.Background(), &core.Event{Topic: "chat/x", Type: ""}, "test"); err == nil {
		t.Fatal("expected empty-type error")
	}
}

func TestPublish_UnknownReducer_StillPersistsEvent(t *testing.T) {
	// Broker uses only PR + Chat reducers; publish onto build/... — no
	// reducer registered but the event and stream should still land.
	b := newBroker()
	seq, err := b.Publish(context.Background(), &core.Event{
		Topic: "build/ci/1", Type: "build_started",
	}, "test")
	if err != nil {
		t.Fatal(err)
	}
	if seq != 1 {
		t.Fatalf("seq=%d", seq)
	}
	events, _ := b.Events(context.Background(), "build/ci/1", 0, 0)
	if len(events) != 1 {
		t.Fatalf("expected event stored anyway; got %d", len(events))
	}
}

func TestSubscribe_FromLatest_NoBackfill(t *testing.T) {
	b := newBroker()
	_, _ = b.Publish(context.Background(), &core.Event{Topic: "chat/x", Type: "user_joined",
		Payload: json.RawMessage(`{"user":"a"}`)}, "test")
	sub, backfill, fence, err := b.Subscribe(context.Background(), "chat/x", core.Latest())
	if err != nil {
		t.Fatal(err)
	}
	defer b.Unsubscribe(sub)
	if len(backfill) != 0 || fence != 0 {
		t.Fatalf("FromLatest should not backfill; got %d/fence=%d", len(backfill), fence)
	}
}

func TestSubscribe_FromLastN_ReturnsTail(t *testing.T) {
	b := newBroker()
	for i := 0; i < 5; i++ {
		_, _ = b.Publish(context.Background(), &core.Event{Topic: "chat/x", Type: "msg_posted"}, "test")
	}
	sub, backfill, fence, err := b.Subscribe(context.Background(), "chat/x", core.LastN(2))
	if err != nil {
		t.Fatal(err)
	}
	defer b.Unsubscribe(sub)
	if len(backfill) != 2 || backfill[0].Seq != 4 || backfill[1].Seq != 5 || fence != 5 {
		t.Fatalf("backfill wrong: len=%d seqs=%v fence=%d", len(backfill),
			seqsOf(backfill), fence)
	}
}

func TestSubscribe_FromSeq_ReturnsSuffix(t *testing.T) {
	b := newBroker()
	for i := 0; i < 5; i++ {
		_, _ = b.Publish(context.Background(), &core.Event{Topic: "chat/x", Type: "msg_posted"}, "test")
	}
	sub, backfill, fence, err := b.Subscribe(context.Background(), "chat/x", core.Seq(3))
	if err != nil {
		t.Fatal(err)
	}
	defer b.Unsubscribe(sub)
	if len(backfill) != 3 || backfill[0].Seq != 3 || fence != 5 {
		t.Fatalf("backfill wrong: seqs=%v fence=%d", seqsOf(backfill), fence)
	}
}

func TestSubscribe_LiveEventsFlowThroughHub(t *testing.T) {
	b := newBroker()
	sub, _, _, err := b.Subscribe(context.Background(), "chat/x", core.Latest())
	if err != nil {
		t.Fatal(err)
	}
	defer b.Unsubscribe(sub)
	_, _ = b.Publish(context.Background(), &core.Event{Topic: "chat/x", Type: "msg_posted"}, "test")
	select {
	case e := <-sub.C:
		if e.Type != "msg_posted" {
			t.Fatalf("got %+v", e)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

func TestPublish_FanoutIncludesPostReduceState(t *testing.T) {
	// Live subscribers should receive the event WITH the current computed
	// state attached, so they can render either without a round-trip.
	// Historical reads should NOT carry state — the raw stream is what's
	// persisted; state is a derived cache maintained separately.
	b := newBroker()
	sub, _, _, err := b.Subscribe(context.Background(), "chat/x", core.Latest())
	if err != nil {
		t.Fatal(err)
	}
	defer b.Unsubscribe(sub)

	_, _ = b.Publish(context.Background(), &core.Event{
		Topic: "chat/x", Type: "user_joined",
		Payload: json.RawMessage(`{"user":"alice"}`),
	}, "test")

	select {
	case e := <-sub.C:
		if len(e.State) == 0 {
			t.Fatalf("live event should carry state; got empty")
		}
		var s map[string]any
		if err := json.Unmarshal(e.State, &s); err != nil {
			t.Fatalf("state not JSON: %v", err)
		}
		parts, _ := s["participants"].([]any)
		if len(parts) != 1 || parts[0].(string) != "alice" {
			t.Fatalf("state should show alice as participant; got %v", s)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event")
	}

	// The persisted event must NOT carry state.
	historical, err := b.Events(context.Background(), "chat/x", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(historical) != 1 {
		t.Fatalf("want 1 stored event, got %d", len(historical))
	}
	if len(historical[0].State) != 0 {
		t.Fatalf("historical read should not carry state; got %s", historical[0].State)
	}
}

func TestSubscribe_FenceDedup_LiveEventInBackfillIsSkippedByCaller(t *testing.T) {
	// The caller-side dedupe: fence == max backfill seq. Any event on sub.C
	// with seq <= fence should be dropped by the caller (this test just
	// verifies the fence is set correctly for that policy to work).
	b := newBroker()
	for i := 0; i < 3; i++ {
		_, _ = b.Publish(context.Background(), &core.Event{Topic: "chat/x", Type: "msg_posted"}, "test")
	}
	sub, backfill, fence, err := b.Subscribe(context.Background(), "chat/x", core.Seq(1))
	if err != nil {
		t.Fatal(err)
	}
	defer b.Unsubscribe(sub)
	if fence != 3 || len(backfill) != 3 {
		t.Fatalf("fence=%d backfill=%d", fence, len(backfill))
	}
}

func seqsOf(es []*core.Event) []uint64 {
	out := make([]uint64, len(es))
	for i, e := range es {
		out[i] = e.Seq
	}
	return out
}
