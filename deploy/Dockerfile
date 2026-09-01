# Multi-arch multi-stage build for the event_watch server.
#
#   docker buildx build --platform linux/amd64,linux/arm64 \
#     -f deploy/Dockerfile -t <registry>/event_watch:<tag> --push .
#
# Result: distroless static image, non-root uid 65532, ~15 MB.

ARG GO_VERSION=1.25
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine AS build
ARG TARGETOS
ARG TARGETARCH

WORKDIR /src

# Cache module downloads separately from source.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

# Strip debug info; disable cgo so the binary is fully static (works in distroless).
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags='-s -w' -o /out/eventwatch ./cmd/server

# ---------- runtime ----------

FROM gcr.io/distroless/static-debian12:nonroot AS runtime

# Metadata
LABEL org.opencontainers.image.title="event_watch"
LABEL org.opencontainers.image.description="Low-latency pub/sub notification service"
LABEL org.opencontainers.image.source="https://github.com/geekychris/event_watch"
LABEL org.opencontainers.image.licenses="MIT"

USER nonroot:nonroot
WORKDIR /
COPY --from=build /out/eventwatch /eventwatch

# Sensible defaults; every flag is also readable from an env var (EW_*).
ENV EW_ADDR=":8080"
ENV EW_STORE="memory"

EXPOSE 8080
ENTRYPOINT ["/eventwatch"]
