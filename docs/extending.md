# Extending event_watch

Two natural extension points: **new object types (reducers)** and **new
webhook plugins**. Both are compile-time registrations — restart the server
after adding either.

## Add a new object type

Everything the server knows about an object type lives in one Go file
implementing the `computed.Reducer` interface:

```go
type Reducer interface {
    ObjectType() string
    Apply(state json.RawMessage, e *core.Event) (json.RawMessage, error)
}
```

`ObjectType()` returns the topic prefix (first path segment). `Apply` folds
the current state and an event into new state. Both must be pure and cheap;
they run under the broker's ingest mutex.

### Worked example: `order/<id>` — a customer order

Say you want to track orders with events like `order_placed`, `order_paid`,
`order_shipped`, `order_delivered`, `order_cancelled`.

**Step 1** — create `internal/objtypes/order.go`:

```go
package objtypes

import (
    "encoding/json"
    "time"

    "github.com/chris/event_watch/internal/core"
)

type OrderState struct {
    Customer    string    `json:"customer,omitempty"`
    Items       []string  `json:"items,omitempty"`
    TotalCents  int64     `json:"total_cents"`
    Status      string    `json:"status"`     // placed|paid|shipped|delivered|cancelled
    PlacedAt    time.Time `json:"placed_at,omitempty"`
    PaidAt      time.Time `json:"paid_at,omitempty"`
    ShippedAt   time.Time `json:"shipped_at,omitempty"`
    DeliveredAt time.Time `json:"delivered_at,omitempty"`
    Tracking    string    `json:"tracking,omitempty"`
}

type OrderReducer struct{}

func (OrderReducer) ObjectType() string { return "order" }

func (OrderReducer) Apply(raw json.RawMessage, e *core.Event) (json.RawMessage, error) {
    var s OrderState
    if len(raw) > 0 {
        if err := json.Unmarshal(raw, &s); err != nil {
            return raw, err
        }
    }
    p := map[string]any{}
    if len(e.Payload) > 0 {
        _ = json.Unmarshal(e.Payload, &p)
    }
    getStr := func(k string) string { v, _ := p[k].(string); return v }

    switch e.Type {
    case "order_placed":
        if v := getStr("customer"); v != "" { s.Customer = v }
        if xs, ok := p["items"].([]any); ok {
            s.Items = nil
            for _, x := range xs {
                if str, ok := x.(string); ok { s.Items = append(s.Items, str) }
            }
        }
        if v, ok := p["total_cents"].(float64); ok { s.TotalCents = int64(v) }
        s.Status = "placed"
        s.PlacedAt = e.OccurredAt
    case "order_paid":
        if s.Status == "placed" { s.Status = "paid"; s.PaidAt = e.OccurredAt }
    case "order_shipped":
        if v := getStr("tracking"); v != "" { s.Tracking = v }
        s.Status = "shipped"
        s.ShippedAt = e.OccurredAt
    case "order_delivered":
        s.Status = "delivered"
        s.DeliveredAt = e.OccurredAt
    case "order_cancelled":
        s.Status = "cancelled"
    }
    return json.Marshal(s)
}
```

**Step 2** — register in `internal/server/server.go`:

```go
reducers.Register(
    objtypes.PRReducer{}, objtypes.BuildReducer{}, objtypes.DeployReducer{},
    objtypes.JobReducer{}, objtypes.ChatReducer{},
    objtypes.StringReducer{}, objtypes.IntReducer{}, objtypes.TimeReducer{},
    objtypes.OrderReducer{},          // ← new
)
```

**Step 3** — add a reducer test in `internal/objtypes/order_test.go`
(follow the shape of the existing `reducers_test.go`).

**Step 4** — rebuild the server. New topic prefix `order/<id>` is now
reduced. Publish with:

```bash
curl -X POST -H 'Content-Type: application/json' -d '{
  "topic": "order/abc123",
  "type":  "order_placed",
  "payload": {"customer":"alice","items":["hat","shirt"],"total_cents":4500}
}' http://localhost:8080/publish
```

That's it. Subscribe, state, metrics, TTL — all inherited automatically.

### When to lean scalar-field instead

If your "type" is just one value (counter, timestamp, flag), you probably
don't need a new reducer at all — use `int/`, `str/`, or `time/` topics with
their existing reducers.

Reducer for its own type when you have **multiple related fields that
change together** (an order's status + timestamps, a build's steps + timings).

---

## Add a webhook plugin

Webhook plugins turn a provider's raw HTTP payload into one or more events.
The interface:

```go
type WebhookPlugin interface {
    Name() string
    Verify(*http.Request) error            // signature check
    Transform(*http.Request) ([]*core.Event, error)
}
```

Register a plugin and it's mounted at `POST /webhook/<name>` automatically.

### Worked example: GitLab merge requests

**Step 1** — create `internal/webhook/gitlab/gitlab.go`:

```go
package gitlab

import (
    "crypto/subtle"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "net/http"

    "github.com/chris/event_watch/internal/core"
)

type Plugin struct{ secret string }

func New(secret string) *Plugin { return &Plugin{secret: secret} }

func (p *Plugin) Name() string { return "gitlab" }

func (p *Plugin) Verify(r *http.Request) error {
    if p.secret == "" { return nil }
    got := r.Header.Get("X-Gitlab-Token")
    if got == "" { return errors.New("missing X-Gitlab-Token") }
    if subtle.ConstantTimeCompare([]byte(got), []byte(p.secret)) != 1 {
        return errors.New("token mismatch")
    }
    return nil
}

func (p *Plugin) Transform(r *http.Request) ([]*core.Event, error) {
    kind := r.Header.Get("X-Gitlab-Event")
    body, err := io.ReadAll(io.LimitReader(r.Body, 5<<20))
    if err != nil { return nil, err }
    var raw map[string]any
    if err := json.Unmarshal(body, &raw); err != nil { return nil, err }

    if kind != "Merge Request Hook" { return nil, nil }
    project, _ := raw["project"].(map[string]any)
    ns   := stringField(project, "namespace")   // helpers similar to github/
    name := stringField(project, "name")
    attrs, _ := raw["object_attributes"].(map[string]any)
    iid := int64Field(attrs, "iid")
    if ns == "" || name == "" || iid == 0 {
        return nil, errors.New("payload missing merge_request fields")
    }
    topic := fmt.Sprintf("pr/%s/%s/%d", ns, name, iid)
    // Map action → event type. Reuse the same pr_* vocabulary so PRs from
    // GitHub and GitLab share the same reducer and thus the same state
    // shape, unified per topic.
    switch stringField(attrs, "action") {
    case "open":    return []*core.Event{{Topic: topic, Type: "pr_opened",
        Payload: mustMarshal(map[string]any{"title": stringField(attrs, "title")})}}, nil
    case "merge":   return []*core.Event{{Topic: topic, Type: "pr_merged"}}, nil
    case "close":   return []*core.Event{{Topic: topic, Type: "pr_closed"}}, nil
    }
    return nil, nil
}
```

(Utility helpers `stringField`, `int64Field`, `mustMarshal` — copy from
`internal/webhook/github/github.go`, they're one-liners.)

**Step 2** — register in `internal/server/server.go`:

```go
whreg := webhook.NewRegistry()
whreg.Register(ghwebhook.New(cfg.GitHubSecret))
whreg.Register(gitlabwebhook.New(cfg.GitLabSecret))    // ← new
```

Add the corresponding config knob in `internal/server/config.go`
(`--gitlab-secret`, `EW_GITLAB_SECRET`).

**Step 3** — test with a canned payload:

```bash
curl -X POST -H 'X-Gitlab-Event: Merge Request Hook' \
     -H 'X-Gitlab-Token: your-secret' \
     -d @gitlab-mr-open.json \
     http://localhost:8080/webhook/gitlab
```

The event lands on `pr/<ns>/<name>/<iid>` and gets reduced by the same PR
reducer as GitHub — that's the whole point of the object-type layer.

---

## Add a new HTTP sugar endpoint

The field endpoints (`POST /field/incr/...`) are thin wrappers that pick
the correct event type + payload shape and delegate to `broker.Publish`.
Adding your own is symmetric.

### Example: `POST /order/{topic...}/ship` with `{tracking}`

```go
// internal/transport/order.go
package transport

import (
    "net/http"
    "time"

    "github.com/chris/event_watch/internal/broker"
    "github.com/chris/event_watch/internal/core"
)

func NewOrderShipHandler(b *broker.Broker) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        topic := r.PathValue("topic")
        body, err := readJSONBody(r)
        if err != nil { http.Error(w, err.Error(), 400); return }
        e := &core.Event{
            Topic: topic, Type: "order_shipped",
            Payload: mustPayload(map[string]any{"tracking": body["tracking"]}),
            OccurredAt: time.Now(),
        }
        seq, err := b.Publish(r.Context(), e, "http")
        if err != nil { http.Error(w, err.Error(), 400); return }
        writeJSON(w, map[string]any{"seq": seq, "topic": topic})
    }
}
```

(reuse `readJSONBody` and `writeJSON` from `internal/transport/field.go`.)

Wire the route in `internal/server/server.go`:

```go
mux.Handle("POST /order/ship/{topic...}", protect(transport.NewOrderShipHandler(b)))
```

Same mental model as the field endpoints — just a nicer verb around
`broker.Publish`.

---

## Testing patterns to follow

- **Unit tests** for reducers should fold a sequence of events and assert
  the final state (see `internal/objtypes/reducers_test.go`).
- **Broker tests** for atomicity should fire N concurrent publishes and
  assert the reduced value equals N (see `field_atomic_test.go`).
- **HTTP tests** wire up a `httptest.NewServer(fieldMux(broker))` and
  round-trip via `curl`-style requests (see `internal/transport/field_test.go`).
- **Client tests** stand up an httptest WS server + full broker and assert
  end-to-end delivery (see `client/backfill_test.go`).

All follow the pattern of "build the smallest broker/store composition
that exercises your change" — no mocks, no big fixtures.

## Where NOT to extend

- **Don't put policy inside the transport handlers.** Keep them thin
  translators; every real decision belongs in the broker or a reducer.
- **Don't couple reducers to storage.** They receive `json.RawMessage` in
  and out; the Store handles persistence. This makes reducer tests trivial.
- **Don't reach into `internal/*` from a client library.** Only the top-level
  `client/` package is the public surface — that's why `Event` and `From`
  are re-exported from there.
