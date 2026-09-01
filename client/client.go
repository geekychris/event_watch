// Package client is the Go SDK for event_watch. One Client wraps one WebSocket
// connection; Subscribe returns a refcounted Handle so N callbacks on the
// same topic share a single upstream subscription. Reconnect is automatic:
// on drop the client re-subscribes each topic from the last seq it observed,
// so callers see no gap unless the server has already trimmed past it.
package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chris/event_watch/internal/core"
	"github.com/gorilla/websocket"
)

// Callback receives a single event. Callbacks run on the client's dispatch
// goroutine — do heavy work asynchronously or you'll back up other observers.
type Callback func(*core.Event)

// Handle is an active subscription for one callback. Close removes it and,
// if it was the last callback for the topic, sends unsubscribe upstream.
type Handle struct {
	c     *Client
	topic string
	id    uint64
}

func (h *Handle) Close() {
	if h == nil || h.c == nil {
		return
	}
	h.c.removeCallback(h.topic, h.id)
}

// Option configures a Client at Dial time.
type Option func(*Client)

func WithAuthToken(tok string) Option { return func(c *Client) { c.token = tok } }

// WithReconnect sets the initial and max backoff. Defaults: 100ms / 30s.
func WithReconnect(initial, max time.Duration) Option {
	return func(c *Client) { c.backoffInitial = initial; c.backoffMax = max }
}

type Client struct {
	url            string
	token          string
	backoffInitial time.Duration
	backoffMax     time.Duration

	sendCh chan outFrame
	done   chan struct{}
	closed atomic.Bool

	mu       sync.Mutex
	topics   map[string]*topicSub
	nextID   uint64
	pending  map[string]chan pendingResp
	nextReq  uint64
	dialTime time.Time
}

type topicSub struct {
	from        core.From
	callbacks   map[uint64]Callback
	lastSeenSeq uint64
}

type outFrame struct {
	Op      string          `json:"op"`
	Topic   string          `json:"topic,omitempty"`
	From    string          `json:"from,omitempty"`
	FromSeq uint64          `json:"from_seq,omitempty"`
	Type    string          `json:"type,omitempty"`
	Actor   string          `json:"actor,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
	ReqID   string          `json:"req_id,omitempty"`
}

type inFrame struct {
	Type   string          `json:"type"`
	Topic  string          `json:"topic,omitempty"`
	Event  *core.Event     `json:"event,omitempty"`
	State  json.RawMessage `json:"state,omitempty"`
	LastSeq uint64         `json:"last_seq,omitempty"`
	Missed uint64          `json:"missed,omitempty"`
	Msg    string          `json:"message,omitempty"`
	ReqID  string          `json:"req_id,omitempty"`
}

type pendingResp struct {
	frame inFrame
	err   error
}

// Dial connects to url (ws:// or wss://) and starts the reader/writer loops.
// The returned Client will attempt to reconnect on drop until Close is called.
func Dial(ctx context.Context, url string, opts ...Option) (*Client, error) {
	c := &Client{
		url:            url,
		backoffInitial: 100 * time.Millisecond,
		backoffMax:     30 * time.Second,
		sendCh:         make(chan outFrame, 256),
		done:           make(chan struct{}),
		topics:         map[string]*topicSub{},
		pending:        map[string]chan pendingResp{},
	}
	for _, o := range opts {
		o(c)
	}
	// One eager dial so the caller learns immediately if the URL is bad. The
	// long-running goroutine takes over from here.
	conn, err := c.dial(ctx)
	if err != nil {
		return nil, err
	}
	go c.run(conn)
	return c, nil
}

func (c *Client) Close() error {
	if !c.closed.CompareAndSwap(false, true) {
		return nil
	}
	close(c.done)
	return nil
}

func (c *Client) dial(ctx context.Context) (*websocket.Conn, error) {
	u := c.url
	if c.token != "" {
		sep := "?"
		if strings.Contains(u, "?") {
			sep = "&"
		}
		u = u + sep + "access_token=" + c.token
	}
	d := websocket.DefaultDialer
	header := http.Header{}
	if c.token != "" {
		header.Set("Authorization", "Bearer "+c.token)
	}
	conn, _, err := d.DialContext(ctx, u, header)
	return conn, err
}

// run owns the WS connection. On disconnect it retries with exponential
// backoff and re-subscribes every topic that still has callbacks.
func (c *Client) run(initial *websocket.Conn) {
	conn := initial
	backoff := c.backoffInitial
	for {
		if conn == nil {
			select {
			case <-c.done:
				return
			case <-time.After(backoff):
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			nc, err := c.dial(ctx)
			cancel()
			if err != nil {
				backoff = min(backoff*2, c.backoffMax)
				continue
			}
			conn = nc
			backoff = c.backoffInitial
		}

		// Reset backoff, re-issue subscriptions.
		c.resubscribeAll()

		writerDone := make(chan struct{})
		readerDone := make(chan struct{})
		go c.writer(conn, writerDone)
		go c.reader(conn, readerDone)

		select {
		case <-c.done:
			_ = conn.Close()
			<-writerDone
			<-readerDone
			return
		case <-readerDone:
			_ = conn.Close()
			<-writerDone
			conn = nil
		}
	}
}

func min(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

func (c *Client) writer(conn *websocket.Conn, done chan struct{}) {
	defer close(done)
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-c.done:
			return
		case f, ok := <-c.sendCh:
			if !ok {
				return
			}
			_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if err := conn.WriteJSON(f); err != nil {
				return
			}
		case <-ticker.C:
			_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (c *Client) reader(conn *websocket.Conn, done chan struct{}) {
	defer close(done)
	_ = conn.SetReadDeadline(time.Now().Add(70 * time.Second))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(70 * time.Second))
	})
	for {
		var f inFrame
		if err := conn.ReadJSON(&f); err != nil {
			return
		}
		c.dispatch(f)
	}
}

func (c *Client) dispatch(f inFrame) {
	switch f.Type {
	case "event":
		if f.Event == nil {
			return
		}
		c.mu.Lock()
		ts, ok := c.topics[f.Topic]
		var cbs []Callback
		if ok {
			if f.Event.Seq > ts.lastSeenSeq {
				ts.lastSeenSeq = f.Event.Seq
			}
			cbs = make([]Callback, 0, len(ts.callbacks))
			for _, cb := range ts.callbacks {
				cbs = append(cbs, cb)
			}
		}
		c.mu.Unlock()
		for _, cb := range cbs {
			cb(f.Event)
		}
	case "state", "ack", "error":
		if f.ReqID != "" {
			c.mu.Lock()
			ch, ok := c.pending[f.ReqID]
			if ok {
				delete(c.pending, f.ReqID)
			}
			c.mu.Unlock()
			if ok {
				var pr pendingResp
				pr.frame = f
				if f.Type == "error" {
					pr.err = errors.New(f.Msg)
				}
				select {
				case ch <- pr:
				default:
				}
			}
		}
	case "lagging":
		// Left to the caller to notice — could trigger a GetState refresh.
	case "pong":
	}
}

func (c *Client) send(f outFrame) {
	select {
	case c.sendCh <- f:
	case <-c.done:
	}
}

// -- request/response over WS --

func (c *Client) request(ctx context.Context, f outFrame) (inFrame, error) {
	c.mu.Lock()
	c.nextReq++
	f.ReqID = fmt.Sprintf("r%d", c.nextReq)
	ch := make(chan pendingResp, 1)
	c.pending[f.ReqID] = ch
	c.mu.Unlock()

	c.send(f)

	select {
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, f.ReqID)
		c.mu.Unlock()
		return inFrame{}, ctx.Err()
	case pr := <-ch:
		return pr.frame, pr.err
	}
}

// -- public API --

// Subscribe registers cb for events on topic. If this is the first callback
// for topic, an upstream subscription is opened with `from`. If further
// callbacks arrive for the same topic they share the existing upstream sub
// (and receive live events only — the `from` argument is ignored on
// subsequent calls for the same topic).
func (c *Client) Subscribe(ctx context.Context, topic string, from core.From, cb Callback) (*Handle, error) {
	if err := core.ValidateTopic(topic); err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	ts, ok := c.topics[topic]
	firstCB := !ok
	if firstCB {
		ts = &topicSub{from: from, callbacks: map[uint64]Callback{}}
		c.topics[topic] = ts
	}
	ts.callbacks[id] = cb
	c.mu.Unlock()

	if firstCB {
		c.send(subscribeFrame(topic, from, 0))
	}
	return &Handle{c: c, topic: topic, id: id}, nil
}

func subscribeFrame(topic string, from core.From, resumeSeq uint64) outFrame {
	f := outFrame{Op: "subscribe", Topic: topic}
	switch {
	case resumeSeq > 0:
		f.FromSeq = resumeSeq
	case from.Kind == core.FromLatest:
		f.From = "latest"
	case from.Kind == core.FromLastN:
		f.From = fmt.Sprintf("last:%d", from.Value)
	case from.Kind == core.FromSeq:
		f.FromSeq = from.Value
	}
	return f
}

func (c *Client) removeCallback(topic string, id uint64) {
	c.mu.Lock()
	ts, ok := c.topics[topic]
	if !ok {
		c.mu.Unlock()
		return
	}
	delete(ts.callbacks, id)
	last := len(ts.callbacks) == 0
	if last {
		delete(c.topics, topic)
	}
	c.mu.Unlock()
	if last {
		c.send(outFrame{Op: "unsubscribe", Topic: topic})
	}
}

// Publish sends a single event; returns the assigned seq. Blocks until the
// server acks.
func (c *Client) Publish(ctx context.Context, e *core.Event) (uint64, error) {
	f, err := c.request(ctx, outFrame{
		Op: "publish", Topic: e.Topic, Type: e.Type, Actor: e.Actor, Payload: e.Payload,
	})
	if err != nil {
		return 0, err
	}
	return f.LastSeq, nil
}

// GetState fetches the current computed state for topic.
func (c *Client) GetState(ctx context.Context, topic string) (json.RawMessage, error) {
	f, err := c.request(ctx, outFrame{Op: "get_state", Topic: topic})
	if err != nil {
		return nil, err
	}
	return f.State, nil
}

// resubscribeAll fires a subscribe for every topic we still care about,
// using the seq of the last event we saw so we don't miss anything.
func (c *Client) resubscribeAll() {
	c.mu.Lock()
	frames := make([]outFrame, 0, len(c.topics))
	for topic, ts := range c.topics {
		resume := uint64(0)
		if ts.lastSeenSeq > 0 {
			resume = ts.lastSeenSeq + 1
		}
		frames = append(frames, subscribeFrame(topic, ts.from, resume))
	}
	c.mu.Unlock()
	for _, f := range frames {
		c.send(f)
	}
}
