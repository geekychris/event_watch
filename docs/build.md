# Build / install / run / test

## Prerequisites

- **Go 1.25+** (project pulls in modules that require this)
- **Docker** (only if you want to run the server against Redis)
- **Xcode command-line tools** on macOS (for cgo; already present if you can build any Go binary)

Client libraries have their own toolchain requirements — see
[clients.md](clients.md).

## Get the source

```bash
git clone <repo-url> event_watch
cd event_watch
```

## Build

```bash
make build
# → bin/eventwatch          the server
# → bin/eventwatch-cli      the CLI test harness
```

Or plain Go:

```bash
go build -o bin/eventwatch ./cmd/server
go build -o bin/eventwatch-cli ./cmd/testclient
```

## Run

### With the in-memory store (zero external deps)

```bash
make run
# equivalent to:
go run ./cmd/server --store=memory
```

Server listens on `:8080`. Open <http://localhost:8080/> for the embedded
htmx UI.

### With Redis

```bash
make redis-up      # docker compose up -d redis
make run-redis     # go run ./cmd/server --store=redis --redis-addr=localhost:6379
make redis-down    # tear it down
```

Redis is optional — the in-memory store is functionally identical (same
`Store` interface); Redis just persists across restarts.

## Configuration

All settings are flags with matching `EW_*` env vars (env is a fallback,
flag wins).

| Flag | Env | Default | Purpose |
|---|---|---|---|
| `--addr` | `EW_ADDR` | `:8080` | HTTP listen address |
| `--store` | `EW_STORE` | `memory` | `memory` or `redis` |
| `--redis-addr` | `EW_REDIS_ADDR` | `localhost:6379` | Redis host:port |
| `--redis-password` | `EW_REDIS_PASSWORD` | *(none)* | Redis AUTH |
| `--redis-db` | — | `0` | Redis DB index |
| `--auth` | `EW_AUTH` | *(empty = off)* | `bearer` to enable |
| `--auth-token` | `EW_AUTH_TOKEN` | — | Bearer token (required when `--auth=bearer`) |
| `--default-ttl` | — | `168h` | Default topic TTL (used by archiver) |
| `--archive-interval` | — | `5m` | How often to sweep for expired topics |
| `--github-secret` | `EW_GITHUB_SECRET` | *(none)* | GitHub webhook signing secret |

### Enabling auth

```bash
./bin/eventwatch --auth=bearer --auth-token=$(openssl rand -hex 24)
# ...
curl -H "Authorization: Bearer <token>" http://localhost:8080/topics
```

WS clients that can't set headers (browser JS `WebSocket()`) can pass
`?access_token=<token>` on the URL instead.

## Test

### Full sweep (unit + integration, no external deps)

```bash
make test
# → go test ./...
```

### Under the race detector, N times

```bash
go test -race -count=10 ./...
```

Every non-empty package should show `ok` in every iteration. The full suite
covers ~5100 LOC.

### Redis integration tests

Requires Redis running (see `make redis-up`):

```bash
make test-redis
# → REDIS_ADDR=localhost:6379 go test -tags=redis ./...
```

The Redis tests are gated behind the `redis` build tag so a plain `go test
./...` never depends on Docker. Each test uses a distinct DB index and
FLUSHDBs its target before running.

### Language client integration tests

Each client has its own test target that runs against a live server on
`:8080`. See [clients.md](clients.md) for per-language commands.

## Verify end-to-end (smoke test)

Start the server, then:

```bash
# publish
curl -s -X POST -H 'Content-Type: application/json' \
  -d '{"topic":"int/hello","type":"int_set","payload":{"value":100}}' \
  http://localhost:8080/publish

# read state
curl -s http://localhost:8080/state/int/hello
# → {"value":100,"exists":true,"updated_at":"..."}

# increment via HTTP sugar
curl -s -X POST -H 'Content-Type: application/json' \
  -d '{"delta":5}' http://localhost:8080/field/incr/int/hello

curl -s http://localhost:8080/state/int/hello
# → {"value":105,"exists":true,"updated_at":"..."}

# metrics
curl -s http://localhost:8080/metrics | grep '^ew_'
```

Or open the htmx UI at <http://localhost:8080/> and click through the
Publish, Fields, and Subscribe cards.

## Wails desktop app

```bash
# prerequisites
go install github.com/wailsapp/wails/v2/cmd/wails@latest
# (verify)
wails doctor

# build + hot-reload dev
cd wails-client/eventwatch-desktop
wails dev
```

The window opens with the same four-card UI as the browser. Point it at
`ws://localhost:8080/ws` and connect.

## Common pitfalls

- **Topics are case-sensitive.** `Pr/x` and `pr/x` are different topics, and
  since object-type prefixes (`pr`, `int`, …) are lowercase-only in the
  reducer registry, mixed-case events get stored but never reduced. Use
  lowercase.
- **After adding a new reducer or object type, restart the server.** Reducers
  are compile-time registered; hot changes need a rebuild.
- **`go test` with a stale Redis** — a previous run's data can confuse a
  new test. The Redis integration tests FLUSHDB their target DB before each
  test to avoid this.
- **Wails + Homebrew CMake 4.x** — clashes with ESP-IDF v5.4 (unrelated to
  the server, but relevant if you also touch the ESP-IDF client). Downgrade
  cmake or upgrade IDF.
