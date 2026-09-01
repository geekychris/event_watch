package transport

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/chris/event_watch/internal/broker"
	"github.com/chris/event_watch/internal/core"
	"github.com/chris/event_watch/internal/hub"
)

type pollResp struct {
	Events  []*core.Event `json:"events"`
	LastSeq uint64        `json:"last_seq"`
}

// NewPollHandler implements GET /poll?topic=<t>[&topic=...]&from_seq=<n>&max_wait_ms=<0..60000>.
// Short-poll when max_wait_ms=0; otherwise blocks up to that duration waiting
// for at least one event, then returns everything currently available.
func NewPollHandler(b *broker.Broker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		topics := q["topic"]
		if len(topics) == 0 {
			http.Error(w, "at least one topic= required", http.StatusBadRequest)
			return
		}
		fromSeq, _ := strconv.ParseUint(q.Get("from_seq"), 10, 64)
		maxWait, _ := strconv.Atoi(q.Get("max_wait_ms"))
		if maxWait < 0 {
			maxWait = 0
		}
		if maxWait > 60000 {
			maxWait = 60000
		}

		out := pollResp{Events: []*core.Event{}}
		ctx := r.Context()

		// First try a synchronous fetch.
		for _, t := range topics {
			events, err := b.Events(ctx, t, fromSeq, 0)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			out.Events = append(out.Events, events...)
		}
		if len(out.Events) > 0 || maxWait == 0 {
			out.LastSeq = maxSeq(out.Events)
			writeJSON(w, out)
			return
		}

		// Long-poll path: subscribe to each topic, wait up to maxWait for one
		// event.
		subs := make([]*hub.Subscription, 0, len(topics))
		defer func() {
			for _, s := range subs {
				b.Unsubscribe(s)
			}
		}()
		for _, t := range topics {
			sub, _, _, err := b.Subscribe(ctx, t, core.Latest())
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			subs = append(subs, sub)
		}

		waitCtx, cancel := context.WithTimeout(ctx, time.Duration(maxWait)*time.Millisecond)
		defer cancel()

		// Each forwarder loops until waitCtx expires, so no event is ever
		// silently dropped: whichever sub delivers first wakes the outer
		// select, and further events accumulate in `merged` for the drain.
		merged := make(chan *core.Event, 128)
		for _, s := range subs {
			go func(sub *hub.Subscription) {
				for {
					select {
					case e := <-sub.C:
						select {
						case merged <- e:
						case <-waitCtx.Done():
							return
						}
					case <-waitCtx.Done():
						return
					}
				}
			}(s)
		}

		// Wait for the first event, or the deadline.
		select {
		case e := <-merged:
			out.Events = append(out.Events, e)
		case <-waitCtx.Done():
		}
		// After the first event, keep batching for up to `batchWindow` so a
		// burst of events on one poll produces one response instead of forcing
		// the client to re-poll N times. Bounded by waitCtx so we never exceed
		// the caller's max_wait.
		if len(out.Events) > 0 {
			batchCtx, batchCancel := context.WithTimeout(waitCtx, 20*time.Millisecond)
			for {
				select {
				case e := <-merged:
					out.Events = append(out.Events, e)
					continue
				case <-batchCtx.Done():
				}
				break
			}
			batchCancel()
		}
		out.LastSeq = maxSeq(out.Events)
		writeJSON(w, out)
	}
}

func maxSeq(events []*core.Event) uint64 {
	var m uint64
	for _, e := range events {
		if e.Seq > m {
			m = e.Seq
		}
	}
	return m
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
