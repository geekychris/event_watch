package transport

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chris/event_watch/internal/broker"
	"github.com/chris/event_watch/internal/computed"
	"github.com/chris/event_watch/internal/hub"
	"github.com/chris/event_watch/internal/objtypes"
	memstore "github.com/chris/event_watch/internal/store/memory"
)

func fieldTestBroker() *broker.Broker {
	reg := computed.NewRegistry()
	reg.Register(objtypes.StringReducer{}, objtypes.IntReducer{}, objtypes.TimeReducer{})
	return broker.New(memstore.New(), hub.New(), reg, time.Hour)
}

func fieldMux(b *broker.Broker) *http.ServeMux {
	m := http.NewServeMux()
	m.Handle("POST /field/set/{topic...}", NewFieldSetHandler(b))
	m.Handle("POST /field/incr/{topic...}", NewFieldIncrHandler(b))
	m.Handle("POST /field/decr/{topic...}", NewFieldDecrHandler(b))
	m.Handle("POST /field/delete/{topic...}", NewFieldDeleteHandler(b))
	m.Handle("POST /field/time-now/{topic...}", NewFieldTimeNowHandler(b))
	m.Handle("POST /field/time-add/{topic...}", NewFieldTimeAddHandler(b))
	m.Handle("GET /state/{topic...}", NewStateHandler(b))
	return m
}

func do(t *testing.T, srv *httptest.Server, method, path, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, srv.URL+path, bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func mustState[T any](t *testing.T, srv *httptest.Server, topic string) T {
	t.Helper()
	resp, err := http.Get(srv.URL + "/state/" + topic)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var out T
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode %s: %v (body=%s)", topic, err, body)
	}
	return out
}

func TestField_StringSetRoundTrip(t *testing.T) {
	srv := httptest.NewServer(fieldMux(fieldTestBroker()))
	defer srv.Close()

	r := do(t, srv, "POST", "/field/set/str/name", `{"value":"alice"}`)
	r.Body.Close()
	if r.StatusCode != 200 {
		t.Fatalf("set status=%d", r.StatusCode)
	}
	s := mustState[objtypes.StringFieldState](t, srv, "str/name")
	if s.Value != "alice" || !s.Exists {
		t.Fatalf("state=%+v", s)
	}
}

func TestField_IntIncrDecr(t *testing.T) {
	srv := httptest.NewServer(fieldMux(fieldTestBroker()))
	defer srv.Close()

	do(t, srv, "POST", "/field/set/int/counter", `{"value":10}`).Body.Close()
	do(t, srv, "POST", "/field/incr/int/counter", `{"delta":3}`).Body.Close()
	do(t, srv, "POST", "/field/incr/int/counter", ``).Body.Close() // default +1
	do(t, srv, "POST", "/field/decr/int/counter", ``).Body.Close() // default -1
	s := mustState[objtypes.IntFieldState](t, srv, "int/counter")
	if s.Value != 13 || !s.Exists {
		t.Fatalf("state=%+v (want 13, exists)", s)
	}
}

func TestField_Delete(t *testing.T) {
	srv := httptest.NewServer(fieldMux(fieldTestBroker()))
	defer srv.Close()

	do(t, srv, "POST", "/field/set/str/x", `{"value":"hi"}`).Body.Close()
	do(t, srv, "POST", "/field/delete/str/x", ``).Body.Close()
	s := mustState[objtypes.StringFieldState](t, srv, "str/x")
	if s.Value != "" || s.Exists {
		t.Fatalf("state after delete=%+v", s)
	}
}

func TestField_TimeNowAndAdd(t *testing.T) {
	srv := httptest.NewServer(fieldMux(fieldTestBroker()))
	defer srv.Close()

	do(t, srv, "POST", "/field/time-now/time/last_seen", ``).Body.Close()
	do(t, srv, "POST", "/field/time-add/time/last_seen", `{"seconds":3600}`).Body.Close()
	s := mustState[objtypes.TimeFieldState](t, srv, "time/last_seen")
	if !s.Exists || s.Value.IsZero() {
		t.Fatalf("state=%+v", s)
	}
}
