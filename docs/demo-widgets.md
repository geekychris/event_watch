# Testing / demoing the UI widgets

Both the React app (`clients/react/`) and the Wails desktop app include
two demo widgets that illustrate the two common UI-integration patterns:

- **Widget 1 — Entity list** (deep integration): every row is one topic;
  the row renders the reduced state and updates on every event.
- **Widget 2 — Notification** (shallow / change-data-capture): watch one
  topic; ignore payload; count + flash on every event.

This doc shows exactly what event types and payload JSON to send so each
widget shows something meaningful. Everything is copy-pasteable.

## Prereqs

- Server running on `:8080` (`make run` from repo root)
- One UI open — either the React app (`cd clients/react && npm run dev`,
  then open <http://localhost:5173>) or the Wails app (`cd
  wails-client/eventwatch-desktop && wails dev`)
- Click **Connect** in the UI so its status pill shows `connected`

All the `curl` snippets below go through `POST /publish` on the server;
they're independent of which UI you're testing.

---

## Widget 1 — Entity list

### What state it renders

The row-rendering code (`EntityListWidget.tsx` in React, the entity-list
JS in the Wails `main.js`) is written for **PR-shaped state**:

```json
{
  "title":     "Add feature",         // shown as bold row title
  "author":    "alice",                // shown as "by alice"
  "state":     "open|closed|merged",   // shown as coloured badge
  "approvals": 1,                      // shown as "✓ 1"
  "reviewers": ["bob"],                // count shown as "👀 1"
  "comments":  3,                      // shown as "💬 3"
  "checks":    { "passed":1, "failed":0, "pending":0 },
  "updated_at": "2026-09-01T..."       // shown as "3s ago" (relative)
}
```

That's exactly the state produced by the built-in **PR reducer**. So any
topic of the form `pr/<owner>/<repo>/<num>` will render with all the
signals. Other object types (`build/`, `deploy/`, `job/`, `chat/`,
`str/`, `int/`, `time/`) will render partially — missing fields show
as `—`. If you want widget 1 to look nice for a different object type,
add rendering logic for that type's state shape (see the reducer output
tables in [`docs/object-types.md`](object-types.md)).

### Event types that drive it — the full PR lifecycle

These are the events the PR reducer knows. Send any subset:

| Event type | Payload shape | Effect on the rendered row |
|---|---|---|
| `pr_opened` | `{"title": "...", "author": "...", "base": "main", "head": "sha"}` | title + author appear; state → open |
| `pr_sync` | `{"head": "newsha"}` | (head updates internally; row unchanged visually) |
| `pr_review_requested` | `{"reviewer": "bob"}` | 👀 count increments |
| `pr_reviewed` | `{"state": "approved"}` or `{"state":"changes_requested"}` | ✓ or changes-requested count increments |
| `pr_commented` | `{}` | 💬 count increments |
| `pr_labeled` | `{"label": "bug"}` | label array grows (not shown in default renderer) |
| `pr_merged` | `{}` | state → merged, badge turns green |
| `pr_closed` | `{}` | state → closed, badge turns red |
| `check_run_completed` | `{"conclusion": "success"}` (or `failure`/`timed_out`/`cancelled`/`pending`) | checks passed/failed/pending increments |

### Full copy-paste demo

**1. In the UI:** Widget 1 → topic input already reads `pr/octo/hello/1`
→ click **Add to list**. A row appears with `(no title yet)`.

**2. In a shell**, drive the PR lifecycle:

```bash
S=http://localhost:8080
T=pr/octo/hello/1
H='Content-Type: application/json'

curl -s -X POST -H "$H" $S/publish -d "{
  \"topic\":\"$T\",\"type\":\"pr_opened\",
  \"payload\":{\"title\":\"Add feature X\",\"author\":\"alice\",\"base\":\"main\",\"head\":\"abc123\"}
}"

curl -s -X POST -H "$H" $S/publish -d "{
  \"topic\":\"$T\",\"type\":\"pr_review_requested\",
  \"payload\":{\"reviewer\":\"bob\"}
}"

curl -s -X POST -H "$H" $S/publish -d "{
  \"topic\":\"$T\",\"type\":\"check_run_completed\",
  \"payload\":{\"conclusion\":\"success\",\"name\":\"unit-tests\"}
}"

curl -s -X POST -H "$H" $S/publish -d "{
  \"topic\":\"$T\",\"type\":\"pr_commented\"
}"

curl -s -X POST -H "$H" $S/publish -d "{
  \"topic\":\"$T\",\"type\":\"pr_reviewed\",
  \"payload\":{\"state\":\"approved\"}
}"

curl -s -X POST -H "$H" $S/publish -d "{
  \"topic\":\"$T\",\"type\":\"pr_merged\"
}"
```

**3. What you should see in the row**, in order:

| After event | Row shows |
|---|---|
| (initial add) | topic column filled; everything else `—` or `(no title yet)` |
| `pr_opened` | **Add feature X** / by alice · `open` badge |
| `pr_review_requested` | `open` badge · 👀 1 |
| `check_run_completed` | 👀 1 · 1 ok / 0 fail / 0 pending |
| `pr_commented` | 👀 1 · 💬 1 · 1 ok / 0 fail / 0 pending |
| `pr_reviewed` (approved) | ✓ 1 · 👀 1 · 💬 1 · 1 ok / 0 fail / 0 pending |
| `pr_merged` | `merged` (green) badge; ✓ 1 · 👀 1 · 💬 1 · 1 ok / 0 fail / 0 pending |

The "Updated" column ticks in real time (`just now` → `2s ago` → …).

### One-liner: fire the whole lifecycle at once

There's a scripted "PR" button in the Publish card of both UIs. Add a row
to Widget 1 first (topic `pr/octo/hello/1`), then click **PR** in the
Publish/simulate card — the row goes from empty to fully merged in about
1.5s as all six events fire in sequence.

Or from the CLI:

```bash
bin/eventwatch-cli simulate --url=ws://localhost:8080/ws --kind=pr
```

### Two rows at once — proving independence

Add a second row: `pr/octo/hello/2`. Then publish an event to only one
of them:

```bash
curl -s -X POST -H "$H" $S/publish -d '{
  "topic":"pr/octo/hello/2","type":"pr_opened",
  "payload":{"title":"Second PR","author":"you","base":"main","head":"deadbeef"}
}'
```

Row 2 populates; row 1 stays put. Each row has an independent subscription.

### Verifying it's really subscribed

While the row is present, check the server's metrics endpoint:

```bash
curl -s http://localhost:8080/admin/metrics.json | python3 -m json.tool
```

Look at `subscriptions_by_type.pr` — it goes up when you add rows and
down when you click ×.

---

## Widget 2 — Notification

### What state it renders

The widget renders **exactly one thing**: how many events have arrived
on the watched topic. It does not read `payload` or `state`. Any event
type and any payload will make it flash.

### Copy-paste demo

**1. In the UI:** Widget 2 → topic input reads `chat/notifications` by
default. Leave it.

**2. In a shell**, fire five events one second apart:

```bash
S=http://localhost:8080
H='Content-Type: application/json'

for i in $(seq 1 5); do
  curl -s -X POST -H "$H" $S/publish -d "{
    \"topic\":\"chat/notifications\",\"type\":\"anything\",
    \"payload\":{\"n\":$i}
  }" > /dev/null
  echo "sent $i"
  sleep 1
done
```

**3. What you should see:**

- The blue panel **pulses** on each event (500ms).
- The big counter climbs `0 → 1 → 2 → 3 → 4 → 5`.
- The "Last: `anything` · Ns ago" line updates.

The event `type` is arbitrary — the widget doesn't care what it is; try
`type: "cache_invalidated"`, `type: "task_finished"`, or anything else.

### One-liner burst

```bash
for i in $(seq 1 20); do
  curl -s -X POST -H "$H" http://localhost:8080/publish -d "{
    \"topic\":\"chat/notifications\",\"type\":\"tick\"
  }" > /dev/null &
done
wait
```

The panel will flash rapidly and the counter jumps by 20.

### Filtering by topic

Change Widget 2's topic to `chat/other` and send events to
`chat/notifications` — nothing should happen. Send to `chat/other` and
the panel starts pulsing again. This proves it's subscribed to what
you told it.

---

## Verifying from the server side

At any point, `curl http://localhost:8080/admin/metrics.json` shows what's
happening:

```json
{
  "connected_clients": 1,               // your UI's WebSocket
  "subscriptions_by_type": {"pr": 2, "chat": 1},   // 2 PR rows + 1 CDC watch
  "ingested_by_type":     {"pr/publish": 6, "chat/publish": 5},
  "fanned_out_by_type":   {"pr": 6, "chat": 5},
  "dropped_by_type":      {},
  "topics": 3
}
```

`ingested` counts every publish; `fanned_out` counts every delivery to a
subscriber; `dropped` counts events that couldn't be delivered because a
subscriber was too slow (their per-sub buffer was full — see
[`architecture.md`](architecture.md#fan-out--one-hub-per-sub-buffered-channels-drop-and-mark)).

You can also watch the raw event stream for a topic without going through
the UI:

```bash
bin/eventwatch-cli subscribe --url=ws://localhost:8080/ws --topic=pr/octo/hello/1 --from=latest
# → prints each event as JSON, one per line
```

## Common gotchas

- **Topic case-sensitivity.** `Pr/octo/hello/1` is a *different* topic
  from `pr/octo/hello/1`. Use lowercase for object-type prefixes.
- **Event type spelling.** Widget 1 only shows PR-shaped state, which
  means the PR reducer must have run. `pr_opened` works; `PROpened`,
  `open_pr`, or `opened` don't (no matching reducer branch → event is
  stored but state doesn't change).
- **Payload key names matter.** `{"reviewer": "bob"}` works;
  `{"user": "bob"}` doesn't (reducer reads `reviewer`, ignores `user`).
- **Empty topic list on widget 1.** If you click **Add to list** without
  typing a topic, nothing happens (empty check). Type a topic first.
- **Widget 1 shows partial rendering on non-PR topics.** Adding a
  `build/ci/42` topic and firing `build_*` events will populate `updated`
  and nothing else, because the row template only reads PR fields.
  Either restrict widget 1 to PR topics or extend its render logic (see
  [`docs/object-types.md`](object-types.md) for other state shapes).

## What to try next

- Kill the server mid-demo, restart it (`make run`), watch the UI's
  reconnect status pill flip to `connecting…` and then back to
  `connected`. Widget 1 rows re-subscribe with `from_seq=lastSeen+1`
  — no gap unless the server has already trimmed past that seq.
- Enable auth: restart the server with
  `--auth=bearer --auth-token=s3cr3t`. The UI's Connect card now needs
  the token filled in. Every `curl` above needs
  `-H "Authorization: Bearer s3cr3t"`.
- Point the same UI at a Kubernetes-deployed server (`kubectl port-forward
  svc/event-watch 8080:80 -n event-watch`). Same URL, same demo.
