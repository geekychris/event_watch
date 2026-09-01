package transport

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/chris/event_watch/internal/broker"
	"github.com/chris/event_watch/internal/core"
	"github.com/chris/event_watch/internal/hub"
	"github.com/chris/event_watch/internal/metrics"
	"github.com/gorilla/websocket"
)

const (
	writeWait  = 10 * time.Second
	pongWait   = 60 * time.Second
	pingPeriod = 30 * time.Second
	outBuf     = 512
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	// Same-origin only for browsers by default; local dev leaves this
	// permissive because the UI is served from the same origin anyway.
	CheckOrigin: func(r *http.Request) bool { return true },
}

// inMsg is the union of client → server frames. Only the fields relevant to
// the op are populated.
type inMsg struct {
	Op      string          `json:"op"`
	Topic   string          `json:"topic,omitempty"`
	From    string          `json:"from,omitempty"`
	FromSeq uint64          `json:"from_seq,omitempty"`
	ReqID   string          `json:"req_id,omitempty"`
	Type    string          `json:"type,omitempty"`
	Actor   string          `json:"actor,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// outMsg is the union of server → client frames. Zero-valued fields are
// omitted so wire size stays small.
type outMsg struct {
	Type   string          `json:"type"`
	Topic  string          `json:"topic,omitempty"`
	Event  *core.Event     `json:"event,omitempty"`
	State  json.RawMessage `json:"state,omitempty"`
	LastSeq uint64         `json:"last_seq,omitempty"`
	Missed uint64          `json:"missed,omitempty"`
	Msg    string          `json:"message,omitempty"`
	ReqID  string          `json:"req_id,omitempty"`
}

// wsSession is one live client. It owns exactly one WebSocket, one writer
// goroutine (the only one calling ws.WriteJSON), one reader goroutine, and
// one forwarder goroutine per active subscription. All frames destined for
// the client flow through `out`.
type wsSession struct {
	ws     *websocket.Conn
	broker *broker.Broker

	out chan outMsg

	mu   sync.Mutex
	subs map[string]*subForward // topic → forwarder
}

type subForward struct {
	sub    *hub.Subscription
	cancel func()
}

func NewWSHandler(b *broker.Broker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		metrics.ConnectedClients.Inc()
		defer metrics.ConnectedClients.Dec()

		s := &wsSession{
			ws:     conn,
			broker: b,
			out:    make(chan outMsg, outBuf),
			subs:   map[string]*subForward{},
		}
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		defer s.closeAllSubs()
		defer conn.Close()

		conn.SetReadLimit(1 << 20)
		_ = conn.SetReadDeadline(time.Now().Add(pongWait))
		conn.SetPongHandler(func(string) error {
			return conn.SetReadDeadline(time.Now().Add(pongWait))
		})

		go s.writePump(ctx, cancel)
		s.readPump(ctx)
	}
}

func (s *wsSession) send(m outMsg) {
	select {
	case s.out <- m:
	default:
		// Backpressure: the writer is stuck. Best-effort drop; the client
		// will notice via missed pongs and reconnect.
	}
}

func (s *wsSession) writePump(ctx context.Context, cancel context.CancelFunc) {
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			return
		case m := <-s.out:
			_ = s.ws.SetWriteDeadline(time.Now().Add(writeWait))
			if err := s.ws.WriteJSON(m); err != nil {
				return
			}
		case <-ticker.C:
			_ = s.ws.SetWriteDeadline(time.Now().Add(writeWait))
			if err := s.ws.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (s *wsSession) readPump(ctx context.Context) {
	for {
		var m inMsg
		if err := s.ws.ReadJSON(&m); err != nil {
			return
		}
		switch m.Op {
		case "subscribe":
			s.handleSubscribe(ctx, m)
		case "unsubscribe":
			s.handleUnsubscribe(m.Topic)
		case "get_state":
			s.handleGetState(ctx, m)
		case "publish":
			s.handlePublish(ctx, m)
		case "ping":
			s.send(outMsg{Type: "pong"})
		default:
			s.send(outMsg{Type: "error", Msg: "unknown op", ReqID: m.ReqID})
		}
	}
}

func parseFrom(from string, fromSeq uint64) core.From {
	if fromSeq > 0 {
		return core.Seq(fromSeq)
	}
	switch {
	case from == "" || from == "latest":
		return core.Latest()
	case len(from) > 5 && from[:5] == "last:":
		var n uint64
		for _, c := range from[5:] {
			if c < '0' || c > '9' {
				return core.Latest()
			}
			n = n*10 + uint64(c-'0')
		}
		return core.LastN(n)
	}
	return core.Latest()
}

func (s *wsSession) handleSubscribe(ctx context.Context, m inMsg) {
	s.mu.Lock()
	if _, ok := s.subs[m.Topic]; ok {
		s.mu.Unlock()
		s.send(outMsg{Type: "error", Msg: "already subscribed", Topic: m.Topic, ReqID: m.ReqID})
		return
	}
	s.mu.Unlock()

	from := parseFrom(m.From, m.FromSeq)
	sub, backfill, fence, err := s.broker.Subscribe(ctx, m.Topic, from)
	if err != nil {
		s.send(outMsg{Type: "error", Msg: err.Error(), Topic: m.Topic, ReqID: m.ReqID})
		return
	}

	// Send backfill in order.
	for _, e := range backfill {
		s.send(outMsg{Type: "event", Topic: m.Topic, Event: e})
	}
	s.send(outMsg{Type: "ack", Topic: m.Topic, LastSeq: fence, ReqID: m.ReqID})

	fctx, cancel := context.WithCancel(ctx)
	fw := &subForward{sub: sub, cancel: cancel}
	s.mu.Lock()
	s.subs[m.Topic] = fw
	s.mu.Unlock()

	go s.forward(fctx, sub, fence)
}

func (s *wsSession) forward(ctx context.Context, sub *hub.Subscription, fence uint64) {
	lastLag := uint64(0)
	for {
		select {
		case <-ctx.Done():
			return
		case <-sub.Done():
			return
		case e, ok := <-sub.C:
			if !ok {
				return
			}
			if e.Seq <= fence {
				continue // dedupe against backfill
			}
			if lag := sub.Lag(); lag > lastLag {
				s.send(outMsg{Type: "lagging", Topic: sub.Topic, Missed: lag - lastLag})
				lastLag = lag
			}
			s.send(outMsg{Type: "event", Topic: sub.Topic, Event: e})
		}
	}
}

func (s *wsSession) handleUnsubscribe(topic string) {
	s.mu.Lock()
	fw, ok := s.subs[topic]
	if ok {
		delete(s.subs, topic)
	}
	s.mu.Unlock()
	if ok {
		fw.cancel()
		s.broker.Unsubscribe(fw.sub)
	}
}

func (s *wsSession) closeAllSubs() {
	s.mu.Lock()
	subs := s.subs
	s.subs = map[string]*subForward{}
	s.mu.Unlock()
	for _, fw := range subs {
		fw.cancel()
		s.broker.Unsubscribe(fw.sub)
	}
}

func (s *wsSession) handleGetState(ctx context.Context, m inMsg) {
	state, err := s.broker.GetState(ctx, m.Topic)
	if err != nil {
		s.send(outMsg{Type: "error", Msg: err.Error(), Topic: m.Topic, ReqID: m.ReqID})
		return
	}
	s.send(outMsg{Type: "state", Topic: m.Topic, State: state, ReqID: m.ReqID})
}

func (s *wsSession) handlePublish(ctx context.Context, m inMsg) {
	e := &core.Event{Topic: m.Topic, Type: m.Type, Actor: m.Actor, Payload: m.Payload, OccurredAt: time.Now()}
	seq, err := s.broker.Publish(ctx, e, "ws")
	if err != nil {
		s.send(outMsg{Type: "error", Msg: err.Error(), Topic: m.Topic, ReqID: m.ReqID})
		return
	}
	s.send(outMsg{Type: "ack", Topic: m.Topic, LastSeq: seq, ReqID: m.ReqID})
}
