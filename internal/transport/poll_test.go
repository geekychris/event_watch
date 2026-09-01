package transport

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chris/event_watch/internal/broker"
	"github.com/chris/event_watch/internal/computed"
	"github.com/chris/event_watch/internal/core"
	"github.com/chris/event_watch/internal/hub"
	memstore "github.com/chris/event_watch/internal/store/memory"
)

func newTestBroker() *broker.Broker {
	return broker.New(memstore.New(), hub.New(), computed.NewRegistry(), time.Hour)
}

func decodePoll(t *testing.T, body io.Reader) pollResp {
	t.Helper()
	var pr pollResp
	if err := json.NewDecoder(body).Decode(&pr); err != nil {
		t.Fatal(err)
	}
	return pr
}

func TestPoll_ShortPollReturnsHistorical(t *testing.T) {
	b := newTestBroker()
	for i := 0; i < 3; i++ {
		_, _ = b.Publish(nil, &core.Event{Topic: "chat/x", Type: "msg_posted"}, "test")
	}
	srv := httptest.NewServer(NewPollHandler(b))
	defer srv.Close()

	r, err := http.Get(srv.URL + "?topic=chat/x&from_seq=0&max_wait_ms=0")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	pr := decodePoll(t, r.Body)
	if len(pr.Events) != 3 || pr.LastSeq != 3 {
		t.Fatalf("want 3 events, last_seq=3; got %d, %d", len(pr.Events), pr.LastSeq)
	}
}

func TestPoll_LongPollWakesOnPublish(t *testing.T) {
	b := newTestBroker()
	srv := httptest.NewServer(NewPollHandler(b))
	defer srv.Close()

	done := make(chan pollResp, 1)
	go func() {
		r, err := http.Get(srv.URL + "?topic=chat/x&from_seq=0&max_wait_ms=3000")
		if err != nil {
			t.Errorf("get: %v", err)
			return
		}
		defer r.Body.Close()
		done <- decodePoll(t, r.Body)
	}()

	// Give the handler time to enter the wait.
	time.Sleep(80 * time.Millisecond)
	_, _ = b.Publish(nil, &core.Event{Topic: "chat/x", Type: "msg_posted"}, "test")

	select {
	case pr := <-done:
		if len(pr.Events) != 1 || pr.Events[0].Type != "msg_posted" {
			t.Fatalf("bad poll response: %+v", pr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("long-poll didn't wake up in time")
	}
}

func TestPoll_LongPollDrainsBurst(t *testing.T) {
	b := newTestBroker()
	srv := httptest.NewServer(NewPollHandler(b))
	defer srv.Close()

	done := make(chan pollResp, 1)
	go func() {
		r, err := http.Get(srv.URL + "?topic=chat/x&from_seq=0&max_wait_ms=1000")
		if err != nil {
			t.Errorf("get: %v", err)
			return
		}
		defer r.Body.Close()
		done <- decodePoll(t, r.Body)
	}()

	time.Sleep(80 * time.Millisecond)
	// Fire a burst; the drain should batch them into one response.
	for i := 0; i < 10; i++ {
		_, _ = b.Publish(nil, &core.Event{Topic: "chat/x", Type: "msg_posted"}, "test")
	}

	pr := <-done
	if len(pr.Events) < 5 {
		t.Fatalf("expected batched burst, got %d events", len(pr.Events))
	}
}
