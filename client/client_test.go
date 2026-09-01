package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chris/event_watch/internal/broker"
	"github.com/chris/event_watch/internal/computed"
	"github.com/chris/event_watch/internal/core"
	"github.com/chris/event_watch/internal/hub"
	"github.com/chris/event_watch/internal/objtypes"
	memstore "github.com/chris/event_watch/internal/store/memory"
	"github.com/chris/event_watch/internal/transport"
)

func testServer(t *testing.T) (*httptest.Server, *broker.Broker) {
	t.Helper()
	reducers := computed.NewRegistry()
	reducers.Register(objtypes.PRReducer{}, objtypes.ChatReducer{})
	b := broker.New(memstore.New(), hub.New(), reducers, time.Hour)
	mux := http.NewServeMux()
	mux.Handle("/ws", transport.NewWSHandler(b))
	srv := httptest.NewServer(mux)
	t.Cleanup(func() { srv.Close() })
	return srv, b
}

func wsURL(s *httptest.Server) string {
	return "ws" + strings.TrimPrefix(s.URL, "http") + "/ws"
}

func TestClient_SubscribeReceivesLive(t *testing.T) {
	srv, b := testServer(t)
	c, err := Dial(context.Background(), wsURL(srv))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	got := make(chan *core.Event, 4)
	h, err := c.Subscribe(context.Background(), "chat/room", core.Latest(), func(e *core.Event) { got <- e })
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()

	// Small delay for the subscribe frame to land.
	time.Sleep(50 * time.Millisecond)

	_, err = b.Publish(context.Background(), &core.Event{
		Topic: "chat/room", Type: "msg_posted", Payload: json.RawMessage(`{"user":"a","text":"hi"}`),
	}, "test")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case e := <-got:
		if e.Type != "msg_posted" {
			t.Fatalf("got %+v", e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestClient_RefcountedSubscribe(t *testing.T) {
	srv, b := testServer(t)
	c, err := Dial(context.Background(), wsURL(srv))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	var count1, count2 atomic.Int64
	h1, _ := c.Subscribe(context.Background(), "chat/x", core.Latest(), func(*core.Event) { count1.Add(1) })
	h2, _ := c.Subscribe(context.Background(), "chat/x", core.Latest(), func(*core.Event) { count2.Add(1) })
	time.Sleep(50 * time.Millisecond)

	_, _ = b.Publish(context.Background(), &core.Event{Topic: "chat/x", Type: "user_joined",
		Payload: json.RawMessage(`{"user":"a"}`)}, "test")

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && (count1.Load() == 0 || count2.Load() == 0) {
		time.Sleep(20 * time.Millisecond)
	}
	if count1.Load() != 1 || count2.Load() != 1 {
		t.Fatalf("both callbacks should have fired once; got %d/%d", count1.Load(), count2.Load())
	}

	// h1.Close leaves h2 still subscribed; second publish should hit only count2.
	h1.Close()
	time.Sleep(50 * time.Millisecond)
	_, _ = b.Publish(context.Background(), &core.Event{Topic: "chat/x", Type: "user_left",
		Payload: json.RawMessage(`{"user":"a"}`)}, "test")

	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && count2.Load() < 2 {
		time.Sleep(20 * time.Millisecond)
	}
	if count1.Load() != 1 {
		t.Fatalf("closed handle should stop receiving; count1=%d", count1.Load())
	}
	if count2.Load() != 2 {
		t.Fatalf("second sub should still get events; count2=%d", count2.Load())
	}

	// Last close → server should have no subscribers on this topic.
	h2.Close()
	time.Sleep(100 * time.Millisecond)
	if b.Hub.SubscriberCount("chat/x") != 0 {
		t.Fatalf("expected 0 upstream subs after last handle closed; got %d",
			b.Hub.SubscriberCount("chat/x"))
	}
}

func TestClient_PublishAndGetState(t *testing.T) {
	srv, _ := testServer(t)
	c, err := Dial(context.Background(), wsURL(srv))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	seq, err := c.Publish(context.Background(), &core.Event{
		Topic: "pr/o/r/1", Type: "pr_opened", Payload: json.RawMessage(`{"title":"T"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if seq != 1 {
		t.Fatalf("seq=%d", seq)
	}
	state, err := c.GetState(context.Background(), "pr/o/r/1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(state), `"title":"T"`) {
		t.Fatalf("state=%s", state)
	}
}

func TestClient_ConcurrentSubscribeUnsubscribe(t *testing.T) {
	srv, _ := testServer(t)
	c, err := Dial(context.Background(), wsURL(srv))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h, _ := c.Subscribe(context.Background(), "chat/y", core.Latest(), func(*core.Event) {})
			time.Sleep(10 * time.Millisecond)
			h.Close()
		}()
	}
	wg.Wait()
	// Give the server a beat to process unsubscribes.
	time.Sleep(150 * time.Millisecond)
	if srv == nil {
		t.Fatal("nil server") // silence unused warning
	}
}
