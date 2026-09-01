// Package archiver runs a background sweep that expires topics whose
// LastEventAt is older than their configured TTL.
package archiver

import (
	"context"
	"log"
	"time"

	"github.com/chris/event_watch/internal/store"
)

type Archiver struct {
	Store    store.Store
	Interval time.Duration
	// DefaultTTL applies to any topic whose meta has TTL == 0.
	DefaultTTL time.Duration
}

func New(s store.Store, interval time.Duration) *Archiver {
	return &Archiver{Store: s, Interval: interval, DefaultTTL: 168 * time.Hour}
}

func (a *Archiver) Run(ctx context.Context) {
	if a.Interval <= 0 {
		return
	}
	t := time.NewTicker(a.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			a.sweep(ctx)
		}
	}
}

func (a *Archiver) sweep(ctx context.Context) int {
	topics, err := a.Store.ListTopics(ctx, "", 0)
	if err != nil {
		return 0
	}
	now := time.Now()
	expired := 0
	for _, t := range topics {
		meta, err := a.Store.GetMeta(ctx, t)
		if err != nil {
			continue
		}
		ttl := meta.TTL
		if ttl == 0 {
			ttl = a.DefaultTTL
		}
		if !meta.LastEventAt.IsZero() && now.Sub(meta.LastEventAt) > ttl {
			if err := a.Store.DeleteTopic(ctx, t); err == nil {
				expired++
			}
		}
	}
	if expired > 0 {
		log.Printf("archiver: expired %d topic(s)", expired)
	}
	return expired
}
