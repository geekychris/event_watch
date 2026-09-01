# wails-client (deferred to phase 2)

A native desktop wrapper around the same `client/` Go package that the htmx
UI uses over WebSocket. Not implemented yet — this directory holds notes for
the follow-up work.

## Scaffolding

```
# one-time
brew install go-task/tap/go-task
go install github.com/wailsapp/wails/v2/cmd/wails@latest

# in this directory
wails init -n eventwatch-desktop -t vanilla -q
cd eventwatch-desktop
go mod edit -replace github.com/chris/event_watch=../..
go get github.com/chris/event_watch
```

## Structure to mirror the htmx page

The Wails app should expose four panels equivalent to the browser UI. Bind a
single Go struct to the frontend so the JS side never talks WebSocket
directly — the Go side owns a `*client.Client` and dispatches events into
Wails runtime events:

```go
type App struct {
    ctx  context.Context
    c    *client.Client
    subs map[string]*client.Handle
}

func (a *App) Connect(url, token string) error { /* Dial, store client */ }

func (a *App) Subscribe(topic string, from string) error {
    h, err := a.c.Subscribe(a.ctx, topic, parseFrom(from), func(e *core.Event) {
        runtime.EventsEmit(a.ctx, "event:"+topic, e)
    })
    if err != nil { return err }
    a.subs[topic] = h
    return nil
}

func (a *App) Unsubscribe(topic string) { /* h.Close() + delete */ }

func (a *App) Publish(topic, typ string, payload string) (uint64, error) { /* ... */ }

func (a *App) GetState(topic string) (json.RawMessage, error) { /* ... */ }
```

The JS side listens with `EventsOn("event:<topic>", ...)` and calls Go
methods through the generated bindings. Reuse the same simulation scripts
that ship in `web/static/app.js`.

## Why deferred

v1 goal is to validate the pub/sub semantics and the client-side refcount
dispatch behaviour. The htmx UI does that with zero extra toolchain. Once
the API surface is stable, wrapping it in Wails is a mechanical exercise
that doesn't influence any earlier design decision — hence phase 2.
