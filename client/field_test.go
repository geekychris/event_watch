package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chris/event_watch/internal/broker"
	"github.com/chris/event_watch/internal/computed"
	"github.com/chris/event_watch/internal/hub"
	"github.com/chris/event_watch/internal/objtypes"
	memstore "github.com/chris/event_watch/internal/store/memory"
	"github.com/chris/event_watch/internal/transport"
)

func fieldTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	reg := computed.NewRegistry()
	reg.Register(objtypes.StringReducer{}, objtypes.IntReducer{}, objtypes.TimeReducer{})
	b := broker.New(memstore.New(), hub.New(), reg, time.Hour)
	mux := http.NewServeMux()
	mux.Handle("/ws", transport.NewWSHandler(b))
	srv := httptest.NewServer(mux)
	t.Cleanup(func() { srv.Close() })
	return srv
}

func fieldWsURL(s *httptest.Server) string {
	return "ws" + strings.TrimPrefix(s.URL, "http") + "/ws"
}

func TestStringField_ClientRoundTrip(t *testing.T) {
	srv := fieldTestServer(t)
	c, err := Dial(context.Background(), fieldWsURL(srv))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	f := c.StringField("str/name")
	if _, err := f.Set(context.Background(), "alice"); err != nil {
		t.Fatal(err)
	}
	v, ok, err := f.Get(context.Background())
	if err != nil || !ok || v != "alice" {
		t.Fatalf("Get: v=%q ok=%v err=%v", v, ok, err)
	}
	if _, err := f.Delete(context.Background()); err != nil {
		t.Fatal(err)
	}
	v, ok, _ = f.Get(context.Background())
	if ok || v != "" {
		t.Fatalf("after delete: v=%q ok=%v", v, ok)
	}
}

func TestIntField_ClientRoundTrip(t *testing.T) {
	srv := fieldTestServer(t)
	c, err := Dial(context.Background(), fieldWsURL(srv))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	f := c.IntField("int/counter")
	_, _ = f.Set(context.Background(), 10)
	_, _ = f.Incr(context.Background(), 3)
	_, _ = f.Incr(context.Background(), 1)
	_, _ = f.Decr(context.Background(), 4)
	v, ok, err := f.Get(context.Background())
	if err != nil || !ok || v != 10 {
		t.Fatalf("Get: v=%d ok=%v err=%v; want 10", v, ok, err)
	}
}

func TestTimeField_ClientRoundTrip(t *testing.T) {
	srv := fieldTestServer(t)
	c, err := Dial(context.Background(), fieldWsURL(srv))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	f := c.TimeField("time/last")
	when := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	_, _ = f.Set(context.Background(), when)
	_, _ = f.Add(context.Background(), time.Hour)
	got, ok, err := f.Get(context.Background())
	if err != nil || !ok {
		t.Fatalf("Get err=%v ok=%v", err, ok)
	}
	want := when.Add(time.Hour)
	if !got.Equal(want) {
		t.Fatalf("time got=%v want=%v", got, want)
	}
}

// Live event frames must carry the post-reduce state so a subscriber can
// render either "raw op" or "current value" without an extra round trip.
func TestField_SubscribeCarriesPostReduceState(t *testing.T) {
	srv := fieldTestServer(t)
	c, err := Dial(context.Background(), fieldWsURL(srv))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	got := make(chan *Event, 4)
	h, err := c.Subscribe(context.Background(), "int/hits", Latest(), func(e *Event) { got <- e })
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	time.Sleep(50 * time.Millisecond)

	f := c.IntField("int/hits")
	_, _ = f.Set(context.Background(), 100)
	_, _ = f.Incr(context.Background(), 5)
	_, _ = f.Decr(context.Background(), 3)

	want := []int64{100, 105, 102}
	for i := 0; i < 3; i++ {
		select {
		case e := <-got:
			if len(e.State) == 0 {
				t.Fatalf("event %d has no state attached", i)
			}
			var s struct {
				Value int64 `json:"value"`
			}
			if err := json.Unmarshal(e.State, &s); err != nil {
				t.Fatalf("event %d state parse: %v", i, err)
			}
			if s.Value != want[i] {
				t.Fatalf("event %d value=%d, want %d", i, s.Value, want[i])
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("timeout at event %d", i)
		}
	}
}

func TestField_SubscribeReceivesMutations(t *testing.T) {
	srv := fieldTestServer(t)
	c, err := Dial(context.Background(), fieldWsURL(srv))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	got := make(chan *Event, 4)
	h, err := c.Subscribe(context.Background(), "int/hits", Latest(), func(e *Event) { got <- e })
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	time.Sleep(50 * time.Millisecond)

	f := c.IntField("int/hits")
	_, _ = f.Incr(context.Background(), 1)
	_, _ = f.Incr(context.Background(), 1)

	seenTypes := map[string]int{}
	deadline := time.After(2 * time.Second)
	for i := 0; i < 2; i++ {
		select {
		case e := <-got:
			seenTypes[e.Type]++
		case <-deadline:
			t.Fatalf("timeout after %d events, seen=%v", i, seenTypes)
		}
	}
	if seenTypes["int_incr"] != 2 {
		t.Fatalf("expected 2 int_incr events, got %v", seenTypes)
	}
}
