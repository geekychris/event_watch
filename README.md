# event_watch

Low-latency pub/sub event notification service. Backend services publish
state changes; UI clients subscribe over WebSocket (or long-poll) and
receive events + a server-maintained "deep state" snapshot produced by
per-object-type reducers.

**📖 Full docs in [`docs/`](docs/)** — overview, architecture with mermaid
diagrams, wire protocol, object types, build guide, client usage for seven
languages, and how to extend.

**Quick reference for event types + payload formats (including math ops):**
[`docs/cheatsheet.md`](docs/cheatsheet.md).

## Quick start

```bash
# no external deps
make run
# → open http://localhost:8080/

# with Redis
make redis-up
make run-redis
```

## Layout

- `cmd/server`      — the backend
- `cmd/testclient`  — scriptable CLI harness
- `internal/`       — server-side packages (store, hub, computed, objtypes, webhook, auth, transport, metrics, archiver, server)
- `client/`         — Go client library (refcounted local dispatch + reconnect)
- `clients/`        — Python / Java / Rust / ESP-IDF / Arduino client libraries (see [`clients/README.md`](clients/README.md))
- `web/`            — htmx UI (embedded)
- `wails-client/`   — Wails desktop app (mirror of the htmx UI, imports `client/`)
- `deploy/`         — Dockerfile + Kustomize manifests for Kubernetes (see [`deploy/k8s/README.md`](deploy/k8s/README.md))
- `docs/`           — the full documentation set

## Object types shipped in v1

| Type   | Topic pattern                    | Notes |
| ------ | -------------------------------- | ----- |
| PR     | `pr/<owner>/<repo>/<num>`        | GitHub PR lifecycle |
| Build  | `build/<pipeline>/<run>`         | per-step timings |
| Deploy | `deploy/<env>/<service>`         | version + health |
| Job    | `job/<uuid>`                     | progress + log tail |
| Chat   | `chat/<room>`                    | participants + recent messages |
| str    | `str/<name>`                     | scalar string field |
| int    | `int/<name>`                     | scalar int with atomic incr/decr |
| time   | `time/<name>`                    | scalar timestamp |

## Auth

Off by default. Enable with:

```bash
--auth=bearer --auth-token=secret
# or
EW_AUTH=bearer EW_AUTH_TOKEN=secret
```

## Contributing / extending

Adding a webhook plugin or a new object type is a one-file change. See
[`docs/extending.md`](docs/extending.md) for worked examples.
