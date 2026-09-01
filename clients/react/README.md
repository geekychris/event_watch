# @eventwatch/react-app

React + TypeScript demo app for event_watch. Full parity with the Wails
desktop app (same four operational cards) plus **two extra widgets** that
show two different UI integration patterns:

1. **Entity list** — the "deep" integration: many subscriptions in one
   component, each row is one topic, each row's state comes straight from
   the reducer via `event.state`.
2. **Notification** — the "shallow" / change-data-capture integration: you
   only care that *something* changed; ignore payload and state.

Uses the [`@eventwatch/browser`](../browser) client library via a
`file:../browser` dependency — no publish step needed to develop locally.

## Prereqs

- Node.js 22+ (or 20+ with `--experimental-websocket`)
- A running event_watch server (`make run` from the repo root)

## Run

```bash
cd clients/react
npm install
npm run dev
# → http://localhost:5173
```

Vite proxies `/admin`, `/topics`, `/state`, `/events` to the server on
`:8080` so the browser sees them as same-origin (no CORS setup needed).
WebSocket goes direct — you configure its URL in the Connect card.

## Build

```bash
npm run build
# → dist/  (162 kB JS, 51 kB gzipped)
npm run preview
```

The built `dist/` folder is a plain static site — drop it behind any web
server, or embed it into the Go binary alongside the htmx UI if you want
to ship one bundle.

## How to demo the widgets

> **Detailed testing guide** (exact event types, payload JSON for every
> event, expected UI state after each publish, gotchas): see
> **[`docs/demo-widgets.md`](../../docs/demo-widgets.md)**.

Short version — start the server and this app; open two browser tabs (both
at `http://localhost:5173`); connect both to `ws://localhost:8080/ws`.

### Widget 1 — Entity list (deep integration)

**Setup on tab A:**
1. Scroll to "Widget 1 — Entity list".
2. Leave the topic as `pr/octo/hello/1` and click **Add to list**. A row
   appears with `(no title yet)`.
3. Add a second row: change the topic to `pr/octo/hello/2` and click
   **Add to list**.

**Drive events from tab B (or the CLI / curl / another client):**
- In the "Publish / simulate" card, click **PR** → this fires the canned
  PR lifecycle on `pr/octo/hello/1`.
- Watch tab A's row for `pr/octo/hello/1` populate in real time: title
  fills in, state moves `open` → `merged`, approvals increment, checks
  update, "updated" ticks.
- The second row (`pr/octo/hello/2`) stays quiet — proving each row has
  its own independent subscription.

**Try it live from a shell:**
```bash
curl -s -X POST -H 'Content-Type: application/json' \
  -d '{"topic":"pr/octo/hello/2","type":"pr_opened",
       "payload":{"title":"My PR","author":"you","base":"main","head":"deadbeef"}}' \
  http://localhost:8080/publish
```
Row 2 updates immediately.

Click the **×** button on a row to remove it — the client sends
`unsubscribe` upstream (visible in the server's `ew_subscriptions` metric
dropping).

### Widget 2 — Notification (CDC)

**Setup on tab A:**
1. Scroll to "Widget 2 — Notification".
2. Leave the topic as `chat/notifications`.

**Fire events from tab B or a shell:**
```bash
for i in $(seq 1 5); do
  curl -s -X POST -H 'Content-Type: application/json' \
    -d "{\"topic\":\"chat/notifications\",\"type\":\"ping\",\"payload\":{\"i\":$i}}" \
    http://localhost:8080/publish
  sleep 1
done
```

Watch tab A: the panel pulses on each event, the counter climbs by 1, and
"Last: ping · Ns ago" updates in real time.

Change the watch topic in Widget 2 to `chat/other` and the widget quietly
stops seeing the `chat/notifications` publishes — proof it's actually
subscribed to what you told it.

## How the integration works

Two small custom hooks do all the work:

### `useSubscribedState<T>(topic)` — for the entity list

```tsx
const state = useSubscribedState<PRState>(topic);
```

- Seeds with an initial `GetState(topic)` on first render.
- Subscribes for the component's lifetime; unsubscribes on unmount.
- Every live event updates `state` from `event.state` (post-reduce
  snapshot the server attaches for free).

Component code is just: subscribe once, render `state?.title` etc., done.
No `useEffect` boilerplate at the callsite.

### `useNotification(topic)` — for the CDC widget

```tsx
const { count, lastAt, lastEventType } = useNotification(topic);
```

- Subscribes for the component's lifetime.
- Ignores payload and state entirely.
- Returns a bumping counter and the timestamp of the last event.

Perfect for cache invalidation triggers, "please refresh" nudges, activity
indicators, etc.

Both hooks are backed by the same `Client` instance from `useClient()` —
the `<ClientProvider>` at the app root manages one WebSocket for the whole
tree. Refcounted subscription semantics mean two components subscribing to
the same topic share one upstream sub.

## Testing (integration-level, no framework)

Component-level tests live upstream in the browser client
(`clients/browser/test/`) — they cover the `Client` class end-to-end
against a live server (subscribe, publish, refcount, reconnect resume).
The React layer is intentionally thin (two hooks, six components); its
correctness is proven by manual demo of the two widgets above.

If you want unit tests here, add `vitest` + `@testing-library/react` and
follow the pattern in the browser client tests.
