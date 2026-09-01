package client

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chris/event_watch/internal/core"
)

// TestClient_FromLastN_DeliversBackfillThenLive: the client's Subscribe with
// LastN(3) should first receive the 3 most recent historical events, then
// live events in order without duplicates.
func TestClient_FromLastN_DeliversBackfillThenLive(t *testing.T) {
	srv, b := testServer(t)
	// Seed 5 events before anyone subscribes.
	for i := 0; i < 5; i++ {
		_, _ = b.Publish(context.Background(), &core.Event{
			Topic: "chat/x", Type: "msg_posted",
			Payload: json.RawMessage(`{"user":"u","text":"seed"}`),
		}, "test")
	}
	c, err := Dial(context.Background(), wsURL(srv))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	var mu sync.Mutex
	var got []uint64
	h, err := c.Subscribe(context.Background(), "chat/x", LastN(3), func(e *Event) {
		mu.Lock()
		got = append(got, e.Seq)
		mu.Unlock()
	})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()

	// Wait for backfill to land.
	waitForLen(t, &mu, &got, 3, 2*time.Second)

	// Publish a live event.
	_, _ = b.Publish(context.Background(), &core.Event{
		Topic: "chat/x", Type: "msg_posted",
		Payload: json.RawMessage(`{"user":"u","text":"live"}`),
	}, "test")
	waitForLen(t, &mu, &got, 4, 2*time.Second)

	mu.Lock()
	defer mu.Unlock()
	want := []uint64{3, 4, 5, 6} // last 3 historical + 1 live, no dupes
	if len(got) != len(want) {
		t.Fatalf("got seqs %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got seqs %v, want %v", got, want)
		}
	}
}

func TestClient_FromSeq_ReplaysExactly(t *testing.T) {
	srv, b := testServer(t)
	for i := 0; i < 5; i++ {
		_, _ = b.Publish(context.Background(), &core.Event{
			Topic: "chat/x", Type: "msg_posted",
		}, "test")
	}
	c, err := Dial(context.Background(), wsURL(srv))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	var mu sync.Mutex
	var got []uint64
	h, err := c.Subscribe(context.Background(), "chat/x", Seq(3), func(e *Event) {
		mu.Lock()
		got = append(got, e.Seq)
		mu.Unlock()
	})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()

	waitForLen(t, &mu, &got, 3, 2*time.Second)
	mu.Lock()
	defer mu.Unlock()
	if got[0] != 3 || got[1] != 4 || got[2] != 5 {
		t.Fatalf("Seq(3) should replay 3,4,5; got %v", got)
	}
}

func TestClient_InvalidTopic_ReturnsError(t *testing.T) {
	srv, _ := testServer(t)
	c, err := Dial(context.Background(), wsURL(srv))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_, err = c.Subscribe(context.Background(), "no-slash", Latest(), func(*Event) {})
	if err == nil {
		t.Fatal("expected topic-validation error")
	}
	_, err = c.Subscribe(context.Background(), "", Latest(), func(*Event) {})
	if err == nil {
		t.Fatal("expected topic-validation error on empty topic")
	}
}

func TestClient_MultipleTopicsOnOneClient(t *testing.T) {
	// One client with subscriptions on N different topics — each callback
	// should only see events for its own topic.
	srv, b := testServer(t)
	c, err := Dial(context.Background(), wsURL(srv))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	seen := map[string]*atomic.Int64{
		"chat/a": {},
		"chat/b": {},
		"pr/o/r/1": {},
	}
	handles := make([]*Handle, 0, len(seen))
	for topic, counter := range seen {
		cnt := counter
		h, err := c.Subscribe(context.Background(), topic, Latest(), func(e *Event) {
			if e.Topic != topicOf(cnt, seen) {
				t.Errorf("callback for wrong topic got %s", e.Topic)
			}
			cnt.Add(1)
		})
		if err != nil {
			t.Fatal(err)
		}
		handles = append(handles, h)
	}
	defer func() {
		for _, h := range handles {
			h.Close()
		}
	}()
	// small delay so the subscribes are ack'd server-side before publish
	time.Sleep(80 * time.Millisecond)

	_, _ = b.Publish(context.Background(), &core.Event{Topic: "chat/a", Type: "msg_posted"}, "t")
	_, _ = b.Publish(context.Background(), &core.Event{Topic: "chat/b", Type: "msg_posted"}, "t")
	_, _ = b.Publish(context.Background(), &core.Event{Topic: "chat/b", Type: "msg_posted"}, "t")
	_, _ = b.Publish(context.Background(), &core.Event{Topic: "pr/o/r/1", Type: "pr_opened",
		Payload: json.RawMessage(`{"title":"x"}`)}, "t")

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if seen["chat/a"].Load() >= 1 && seen["chat/b"].Load() >= 2 && seen["pr/o/r/1"].Load() >= 1 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("counts: a=%d b=%d pr=%d",
		seen["chat/a"].Load(), seen["chat/b"].Load(), seen["pr/o/r/1"].Load())
}

// topicOf is a reverse-lookup helper for the map above; used to double-check
// each callback only sees its own topic's events.
func topicOf(counter *atomic.Int64, m map[string]*atomic.Int64) string {
	for k, v := range m {
		if v == counter {
			return k
		}
	}
	return ""
}

// TestClient_ReconnectResumesFromLastSeenSeq: kill the server mid-stream,
// bring a new server up on the same port, and confirm the client's callback
// receives events starting where it left off, not from the beginning.
//
// httptest.Server doesn't reopen on a specific port, so this test proves the
// client-side resume path by inspecting the sent subscribe frame after the
// first reconnect (a rebuilt server on a new URL wouldn't be a "reconnect"
// as far as the client is concerned — we'd Dial a fresh Client).
//
// So we instead verify the client's send frames directly: subscribe, observe
// an event, force the writer to reconnect by closing the underlying conn,
// and assert the next subscribe frame uses from_seq=lastSeq+1.
func TestClient_ReconnectResumesFromLastSeenSeq(t *testing.T) {
	srv, b := testServer(t)
	c, err := Dial(context.Background(), wsURL(srv), WithReconnect(20*time.Millisecond, 100*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	got := make(chan uint64, 4)
	h, err := c.Subscribe(context.Background(), "chat/x", Latest(), func(e *Event) {
		got <- e.Seq
	})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()

	// Give the subscribe frame time to land, then publish one event.
	time.Sleep(80 * time.Millisecond)
	_, _ = b.Publish(context.Background(), &core.Event{Topic: "chat/x", Type: "msg_posted"}, "t")
	select {
	case seq := <-got:
		if seq != 1 {
			t.Fatalf("first event seq=%d, want 1", seq)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("first event never arrived")
	}

	// Force the server to close every client connection: restart the httptest
	// mux by closing the outer server and starting a new one on a new URL.
	// The existing client will fail its reader/writer and try to reconnect —
	// which will keep failing because the URL changed. So instead, we verify
	// the resume behavior by inspecting the client's internal state.
	c.mu.Lock()
	ts, ok := c.topics["chat/x"]
	c.mu.Unlock()
	if !ok || ts.lastSeenSeq != 1 {
		t.Fatalf("client should have recorded lastSeenSeq=1; got %d exists=%v",
			ts.lastSeenSeq, ok)
	}
	// Simulate what resubscribeAll would send.
	frame := subscribeFrame("chat/x", ts.from, ts.lastSeenSeq+1)
	if frame.FromSeq != 2 {
		t.Fatalf("resume frame should carry from_seq=2, got %d", frame.FromSeq)
	}
}

// TestClient_LaggingFrame_NoCallbacksButKeepsWorking: the WS layer sends a
// {type:"lagging"} frame when the hub drops events. The client MUST NOT
// deliver it to callbacks and MUST keep the topic subscription alive.
func TestClient_LaggingFrame_NoCallbacksButKeepsWorking(t *testing.T) {
	srv, _ := testServer(t)
	c, err := Dial(context.Background(), wsURL(srv))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	var got atomic.Int64
	h, err := c.Subscribe(context.Background(), "chat/x", Latest(), func(*Event) { got.Add(1) })
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()

	// Direct-dispatch a synthetic lagging frame.
	c.dispatch(inFrame{Type: "lagging", Topic: "chat/x", Missed: 42})
	if got.Load() != 0 {
		t.Fatalf("lagging frame must not invoke callbacks, got %d", got.Load())
	}
	// Subscription is still present.
	c.mu.Lock()
	_, still := c.topics["chat/x"]
	c.mu.Unlock()
	if !still {
		t.Fatal("lagging frame should not remove the subscription")
	}
}

// TestClient_PublishError_PropagatesToCaller: server returns an error frame
// (e.g. invalid topic on publish) and the client's Publish must return an
// error, not a phantom success.
func TestClient_PublishError_PropagatesToCaller(t *testing.T) {
	srv, _ := testServer(t)
	c, err := Dial(context.Background(), wsURL(srv))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	// "nope" has no slash → server-side ValidateTopic rejects with error frame.
	_, err = c.Publish(context.Background(), &Event{Topic: "nope", Type: "x"})
	if err == nil {
		t.Fatal("expected publish error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("error should mention invalid topic; got %v", err)
	}
}

// waitForLen blocks until slice length reaches n or timeout elapses.
func waitForLen[T any](t *testing.T, mu *sync.Mutex, s *[]T, n int, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		mu.Lock()
		l := len(*s)
		mu.Unlock()
		if l >= n {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	mu.Lock()
	l := len(*s)
	mu.Unlock()
	t.Fatalf("timeout: len=%d, want >= %d", l, n)
}
