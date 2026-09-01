// Wails-side bindings. Every exported method on *App is callable from JS as
// window.go.main.App.<Method>. Live events are pushed via runtime.EventsEmit
// on channels named "event:<topic>" and "lag:<topic>" — the frontend
// subscribes with window.runtime.EventsOn.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"

	"github.com/chris/event_watch/client"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

type App struct {
	ctx context.Context

	mu     sync.Mutex
	client *client.Client
	subs   map[string]*client.Handle
	// wsURL is kept so REST-style helpers (metrics, topics) can hit the
	// same host without asking the user to type it twice.
	baseHTTP string
	token    string
}

func NewApp() *App { return &App{subs: map[string]*client.Handle{}} }

func (a *App) startup(ctx context.Context) { a.ctx = ctx }

// -- lifecycle --

// Connect dials the WebSocket and stores the client for later Subscribe /
// Publish calls. Idempotent: calling twice with different URLs disconnects
// the previous session first.
func (a *App) Connect(url, token string) error {
	log.Printf("[ew] Connect(url=%q token=%q)", url, token)
	a.mu.Lock()
	if a.client != nil {
		for topic, h := range a.subs {
			h.Close()
			delete(a.subs, topic)
		}
		_ = a.client.Close()
		a.client = nil
	}
	a.mu.Unlock()

	c, err := client.Dial(a.ctx, url, client.WithAuthToken(token))
	if err != nil {
		log.Printf("[ew] Connect failed: %v", err)
		return err
	}
	a.mu.Lock()
	a.client = c
	a.token = token
	a.baseHTTP = wsToHTTP(url)
	a.mu.Unlock()
	log.Printf("[ew] Connect ok, baseHTTP=%s", a.baseHTTP)
	return nil
}

func (a *App) Disconnect() {
	a.mu.Lock()
	defer a.mu.Unlock()
	for topic, h := range a.subs {
		h.Close()
		delete(a.subs, topic)
	}
	if a.client != nil {
		_ = a.client.Close()
		a.client = nil
	}
}

// -- subscribe --

// Subscribe registers interest in topic. Live events arrive as runtime events
// named "event:<topic>". `from` is "latest", "last:N", or "seq:N".
func (a *App) Subscribe(topic, from string) error {
	log.Printf("[ew] Subscribe(topic=%q from=%q)", topic, from)
	a.mu.Lock()
	c := a.client
	if _, exists := a.subs[topic]; exists {
		a.mu.Unlock()
		log.Printf("[ew] Subscribe rejected: already subscribed")
		return fmt.Errorf("already subscribed to %s", topic)
	}
	a.mu.Unlock()
	if c == nil {
		log.Printf("[ew] Subscribe rejected: not connected")
		return fmt.Errorf("not connected")
	}

	h, err := c.Subscribe(a.ctx, topic, parseFrom(from), func(e *client.Event) {
		log.Printf("[ew] event delivered: topic=%s type=%s seq=%d", e.Topic, e.Type, e.Seq)
		wailsruntime.EventsEmit(a.ctx, "event:"+e.Topic, e)
	})
	if err != nil {
		log.Printf("[ew] Subscribe failed: %v", err)
		return err
	}
	a.mu.Lock()
	a.subs[topic] = h
	a.mu.Unlock()
	log.Printf("[ew] Subscribe ok")
	return nil
}

func (a *App) Unsubscribe(topic string) {
	a.mu.Lock()
	h, ok := a.subs[topic]
	if ok {
		delete(a.subs, topic)
	}
	a.mu.Unlock()
	if ok {
		h.Close()
	}
}

// -- publish + state --

func (a *App) Publish(topic, typ string, payloadJSON string) (uint64, error) {
	log.Printf("[ew] Publish(topic=%q type=%q payload=%s)", topic, typ, payloadJSON)
	a.mu.Lock()
	c := a.client
	a.mu.Unlock()
	if c == nil {
		return 0, fmt.Errorf("not connected")
	}
	seq, err := c.Publish(a.ctx, &client.Event{
		Topic: topic, Type: typ, Payload: json.RawMessage(payloadJSON),
	})
	if err != nil {
		log.Printf("[ew] Publish failed: %v", err)
		return 0, err
	}
	log.Printf("[ew] Publish ok seq=%d", seq)
	return seq, nil
}

// GetState returns the current computed state as a JSON string (frontend
// pretty-prints it). Returns "" if the topic has no state yet — a "not
// found" from the server is a normal condition, not an error.
func (a *App) GetState(topic string) (string, error) {
	log.Printf("[ew] GetState(topic=%q)", topic)
	a.mu.Lock()
	c := a.client
	a.mu.Unlock()
	if c == nil {
		return "", fmt.Errorf("not connected")
	}
	state, err := c.GetState(a.ctx, topic)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "not found") {
			log.Printf("[ew] GetState: no state yet for %s", topic)
			return "", nil
		}
		log.Printf("[ew] GetState failed: %v", err)
		return "", err
	}
	return string(state), nil
}

// -- metrics + topics (over HTTP; the WS protocol has no admin ops) --

func (a *App) ListTopics() ([]string, error) {
	body, err := a.httpGET("/topics")
	if err != nil {
		return nil, err
	}
	var wire struct {
		Topics []string `json:"topics"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, err
	}
	return wire.Topics, nil
}

func (a *App) Metrics() (string, error) {
	body, err := a.httpGET("/admin/metrics.json")
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// -- field ops --

// SetString publishes str_set for topic. Topic must start with "str/".
func (a *App) SetString(topic, value string) (uint64, error) {
	log.Printf("[ew] SetString(topic=%q value=%q)", topic, value)
	return a.publishField(topic, "str_set", map[string]any{"value": value})
}

// SetInt publishes int_set for topic. Topic must start with "int/".
func (a *App) SetInt(topic string, value int64) (uint64, error) {
	log.Printf("[ew] SetInt(topic=%q value=%d)", topic, value)
	return a.publishField(topic, "int_set", map[string]any{"value": value})
}

// IncrInt publishes int_incr for topic (delta may be negative).
func (a *App) IncrInt(topic string, delta int64) (uint64, error) {
	log.Printf("[ew] IncrInt(topic=%q delta=%d)", topic, delta)
	return a.publishField(topic, "int_incr", map[string]any{"delta": delta})
}

// DecrInt publishes int_decr for topic.
func (a *App) DecrInt(topic string, delta int64) (uint64, error) {
	log.Printf("[ew] DecrInt(topic=%q delta=%d)", topic, delta)
	return a.publishField(topic, "int_decr", map[string]any{"delta": delta})
}

// TimeNow publishes time_now for topic. Topic must start with "time/".
func (a *App) TimeNow(topic string) (uint64, error) {
	log.Printf("[ew] TimeNow(topic=%q)", topic)
	return a.publishField(topic, "time_now", nil)
}

// DeleteField dispatches to the type-appropriate delete op based on the
// topic's first segment: str/int/time.
func (a *App) DeleteField(topic string) (uint64, error) {
	log.Printf("[ew] DeleteField(topic=%q)", topic)
	i := strings.IndexByte(topic, '/')
	if i <= 0 {
		return 0, fmt.Errorf("invalid topic %q", topic)
	}
	switch topic[:i] {
	case "str":
		return a.publishField(topic, "str_delete", nil)
	case "int":
		return a.publishField(topic, "int_delete", nil)
	case "time":
		return a.publishField(topic, "time_delete", nil)
	}
	return 0, fmt.Errorf("delete not supported for object type %q", topic[:i])
}

func (a *App) publishField(topic, typ string, payload map[string]any) (uint64, error) {
	a.mu.Lock()
	c := a.client
	a.mu.Unlock()
	if c == nil {
		return 0, fmt.Errorf("not connected")
	}
	p, _ := json.Marshal(payload)
	return c.Publish(a.ctx, &client.Event{Topic: topic, Type: typ, Payload: p})
}

func (a *App) httpGET(path string) ([]byte, error) {
	a.mu.Lock()
	base := a.baseHTTP
	token := a.token
	a.mu.Unlock()
	if base == "" {
		return nil, fmt.Errorf("not connected")
	}
	req, err := http.NewRequestWithContext(a.ctx, "GET", base+path, nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("http %d", resp.StatusCode)
	}
	buf := make([]byte, 0, 8192)
	tmp := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if err != nil {
			break
		}
	}
	return buf, nil
}

// -- helpers --

func parseFrom(s string) client.From {
	switch {
	case s == "" || s == "latest":
		return client.Latest()
	case strings.HasPrefix(s, "last:"):
		var n uint64
		fmt.Sscanf(s[5:], "%d", &n)
		return client.LastN(n)
	case strings.HasPrefix(s, "seq:"):
		var n uint64
		fmt.Sscanf(s[4:], "%d", &n)
		return client.Seq(n)
	}
	return client.Latest()
}

func wsToHTTP(u string) string {
	// strip trailing /ws and swap scheme
	u = strings.TrimSuffix(u, "/ws")
	if strings.HasPrefix(u, "ws://") {
		return "http://" + strings.TrimPrefix(u, "ws://")
	}
	if strings.HasPrefix(u, "wss://") {
		return "https://" + strings.TrimPrefix(u, "wss://")
	}
	return u
}
