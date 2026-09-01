package hub

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chris/event_watch/internal/core"
)

func TestFanOutToMultipleSubscribers(t *testing.T) {
	h := New()
	subs := make([]*Subscription, 5)
	for i := range subs {
		subs[i] = h.Subscribe("t/x")
	}
	e := &core.Event{Topic: "t/x", Seq: 1}
	h.Publish(e)
	for i, s := range subs {
		select {
		case got := <-s.C:
			if got != e {
				t.Errorf("sub %d got wrong event", i)
			}
		case <-time.After(time.Second):
			t.Fatalf("sub %d timed out", i)
		}
	}
}

func TestCloseRemovesFromRegistry(t *testing.T) {
	h := New()
	s := h.Subscribe("t/x")
	if h.SubscriberCount("t/x") != 1 {
		t.Fatal("want 1 sub")
	}
	s.Close()
	if h.SubscriberCount("t/x") != 0 {
		t.Fatalf("want 0 subs after Close, got %d", h.SubscriberCount("t/x"))
	}
	// Double-close is safe.
	s.Close()
}

func TestSlowSubscriberDrops(t *testing.T) {
	h := New()
	var drops atomic.Uint64
	h.OnDrop = func(string) { drops.Add(1) }
	s := h.Subscribe("t/x")
	// Fill buffer + one over.
	for i := 0; i < DefaultBuffer+10; i++ {
		h.Publish(&core.Event{Topic: "t/x", Seq: uint64(i + 1)})
	}
	if drops.Load() < 10 {
		t.Fatalf("expected >= 10 drops, got %d", drops.Load())
	}
	if s.Lag() < 10 {
		t.Fatalf("expected sub lag >= 10, got %d", s.Lag())
	}
}

func TestPublishIsolatedByTopic(t *testing.T) {
	h := New()
	a := h.Subscribe("t/a")
	b := h.Subscribe("t/b")
	h.Publish(&core.Event{Topic: "t/a", Seq: 1})
	select {
	case <-a.C:
	case <-time.After(time.Second):
		t.Fatal("a did not receive")
	}
	select {
	case e := <-b.C:
		t.Fatalf("b received cross-topic event %+v", e)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestConcurrentSubscribeAndPublish(t *testing.T) {
	h := New()
	var wg sync.WaitGroup
	stop := make(chan struct{})
	// Publisher.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				h.Publish(&core.Event{Topic: "t/x"})
			}
		}
	}()
	// Churn of subscribers.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s := h.Subscribe("t/x")
			time.Sleep(10 * time.Millisecond)
			s.Close()
		}()
	}
	time.Sleep(200 * time.Millisecond)
	close(stop)
	wg.Wait()
	if h.SubscriberCount("t/x") != 0 {
		t.Fatalf("expected all subs cleaned up, %d remain", h.SubscriberCount("t/x"))
	}
}
