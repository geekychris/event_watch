.PHONY: build test test-redis vet run run-redis redis-up redis-down clean

BIN := bin

build:
	@mkdir -p $(BIN)
	go build -o $(BIN)/eventwatch ./cmd/server
	go build -o $(BIN)/eventwatch-cli ./cmd/testclient

test:
	go test ./...

test-redis:
	REDIS_ADDR=localhost:6379 go test -tags=redis ./...

vet:
	go vet ./...

run:
	go run ./cmd/server --store=memory

run-redis:
	go run ./cmd/server --store=redis --redis-addr=localhost:6379

redis-up:
	docker compose up -d redis

redis-down:
	docker compose down

clean:
	rm -rf $(BIN)
