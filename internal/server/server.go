// Package server composes every layer and exposes a Run(ctx) that blocks
// until the HTTP server exits.
package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/chris/event_watch/internal/archiver"
	"github.com/chris/event_watch/internal/auth"
	"github.com/chris/event_watch/internal/broker"
	"github.com/chris/event_watch/internal/computed"
	"github.com/chris/event_watch/internal/hub"
	"github.com/chris/event_watch/internal/metrics"
	"github.com/chris/event_watch/internal/objtypes"
	"github.com/chris/event_watch/internal/store"
	memstore "github.com/chris/event_watch/internal/store/memory"
	redisstore "github.com/chris/event_watch/internal/store/redis"
	"github.com/chris/event_watch/internal/transport"
	"github.com/chris/event_watch/internal/webhook"
	ghwebhook "github.com/chris/event_watch/internal/webhook/github"
	"github.com/chris/event_watch/web"
)

func Run(ctx context.Context, cfg *Config) error {
	// Store.
	var st store.Store
	switch cfg.Store {
	case "memory":
		st = memstore.New()
		log.Printf("store: memory")
	case "redis":
		rs, err := redisstore.New(ctx, redisstore.Options{
			Addr: cfg.RedisAddr, Password: cfg.RedisPassword, DB: cfg.RedisDB,
		})
		if err != nil {
			return fmt.Errorf("redis store: %w", err)
		}
		st = rs
		log.Printf("store: redis (%s)", cfg.RedisAddr)
	default:
		return fmt.Errorf("unknown store: %q", cfg.Store)
	}
	defer st.Close()

	// Reducers.
	reducers := computed.NewRegistry()
	reducers.Register(
		objtypes.PRReducer{}, objtypes.BuildReducer{}, objtypes.DeployReducer{},
		objtypes.JobReducer{}, objtypes.ChatReducer{},
		objtypes.StringReducer{}, objtypes.IntReducer{}, objtypes.TimeReducer{},
	)

	// Hub + broker.
	h := hub.New()
	b := broker.New(st, h, reducers, cfg.DefaultTTL)

	// Webhook plugins.
	whreg := webhook.NewRegistry()
	whreg.Register(ghwebhook.New(cfg.GitHubSecret))

	// Auth middleware.
	var authenticator auth.Authenticator = auth.NoopAuthenticator{}
	switch cfg.Auth {
	case "", "none":
		// stay with noop
	case "bearer":
		if cfg.AuthToken == "" {
			return errors.New("--auth=bearer requires --auth-token")
		}
		authenticator = auth.NewBearer(cfg.AuthToken)
		log.Printf("auth: bearer")
	default:
		return fmt.Errorf("unknown auth mode: %q", cfg.Auth)
	}

	// Routes.
	mux := http.NewServeMux()
	protect := func(h http.Handler) http.Handler { return auth.Middleware(authenticator, h) }
	mux.Handle("GET /ws", protect(transport.NewWSHandler(b)))
	mux.Handle("POST /publish", protect(transport.NewPublishHandler(b)))
	mux.Handle("POST /webhook/{plugin}", transport.NewWebhookHandler(b, whreg)) // signature-verified per plugin
	mux.Handle("GET /poll", protect(transport.NewPollHandler(b)))
	mux.Handle("GET /state/{topic...}", protect(transport.NewStateHandler(b)))
	mux.Handle("GET /events/{topic...}", protect(transport.NewEventsHandler(b)))
	mux.Handle("GET /topics", protect(transport.NewTopicsHandler(b)))
	mux.Handle("POST /field/set/{topic...}", protect(transport.NewFieldSetHandler(b)))
	mux.Handle("POST /field/incr/{topic...}", protect(transport.NewFieldIncrHandler(b)))
	mux.Handle("POST /field/decr/{topic...}", protect(transport.NewFieldDecrHandler(b)))
	mux.Handle("POST /field/delete/{topic...}", protect(transport.NewFieldDeleteHandler(b)))
	mux.Handle("POST /field/time-now/{topic...}", protect(transport.NewFieldTimeNowHandler(b)))
	mux.Handle("POST /field/time-add/{topic...}", protect(transport.NewFieldTimeAddHandler(b)))
	mux.Handle("GET /admin/metrics.json", transport.NewMetricsSnapshotHandler())
	mux.Handle("GET /metrics", metrics.Handler())
	mux.Handle("GET /", web.Handler())

	// Archiver.
	arch := archiver.New(st, cfg.ArchiveInterval)
	archCtx, archCancel := context.WithCancel(ctx)
	go arch.Run(archCtx)
	defer archCancel()

	// Topic-count gauge tick.
	go topicGaugeLoop(ctx, st)

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("listening on %s", cfg.Addr)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func topicGaugeLoop(ctx context.Context, st store.Store) {
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			topics, err := st.ListTopics(ctx, "", 0)
			if err == nil {
				metrics.TopicsTotal.Set(float64(len(topics)))
			}
		}
	}
}
