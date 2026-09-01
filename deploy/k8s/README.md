# event_watch — Kubernetes deployment

Kustomize manifests for the server + Redis. Two overlays ship: `dev` (fast
iteration, ephemeral storage, one replica) and `prod` (persistent Redis,
ingress + ServiceMonitor extras, prod-sized resources).

```
deploy/
  Dockerfile                        # multi-stage, non-root, distroless
  .dockerignore
  k8s/
    base/                           # namespace + configmap + secret example
                                    # + Redis StatefulSet + server Deployment
    overlays/
      dev/                          # emptyDir Redis, no ingress, minimal deps
      prod/                         # PVC Redis, ingress, ServiceMonitor
    extras/
      ingress.yaml                  # WS-friendly NGINX ingress example
      servicemonitor.yaml           # kube-prometheus-stack scrape config
    README.md                       # this file
```

## One-shot deploy (dev, on kind/minikube/docker-desktop)

```bash
# 1. Build the image
docker build -f deploy/Dockerfile -t event_watch:dev .

# 2. Load it into your local cluster (kind example)
kind load docker-image event_watch:dev

# 3. Apply
kubectl apply -k deploy/k8s/overlays/dev

# 4. Wait + port-forward
kubectl -n event-watch rollout status deploy/event-watch
kubectl -n event-watch port-forward svc/event-watch 8080:80
# → open http://localhost:8080/
```

## Prod deploy

```bash
# 1. Build for your target arch and push to your registry
docker buildx build --platform linux/amd64,linux/arm64 \
  -f deploy/Dockerfile \
  -t ghcr.io/geekychris/event_watch:v0.1.0 \
  --push .

# 2. Create the auth Secret from a real token (never commit real secrets)
kubectl create namespace event-watch
kubectl create secret generic event-watch-secret \
  --namespace event-watch \
  --from-literal=EW_AUTH_TOKEN="$(openssl rand -hex 24)" \
  --from-literal=EW_GITHUB_SECRET="$(openssl rand -hex 24)"

# 3. Edit overlays/prod (or copy to overlays/<yourname>) and adjust:
#    - image tag in `images:`
#    - ingress host in ../../extras/ingress.yaml
#    - remove the ServiceMonitor line from ../../extras/kustomization.yaml
#      if you don't run Prometheus Operator

# 4. Deploy
kubectl apply -k deploy/k8s/overlays/prod
kubectl -n event-watch rollout status deploy/event-watch
```

## What gets deployed

| Kind | Name | Purpose |
|---|---|---|
| `Namespace` | `event-watch` | isolation |
| `ConfigMap` | `event-watch-config` | non-secret env (`EW_ADDR`, `EW_STORE`, `EW_REDIS_ADDR`, TTL, archive interval) |
| `Secret` | `event-watch-secret` | `EW_AUTH_TOKEN`, `EW_GITHUB_SECRET` — created by you, `optional: true` on the Deployment |
| `Service` | `event-watch` | ClusterIP → server pod on port 8080 |
| `Deployment` | `event-watch` | the server, replicas=1 (see note below) |
| `Service` | `event-watch-redis` | headless service for the StatefulSet |
| `StatefulSet` | `event-watch-redis` | single-pod Redis 7 with a PVC (or emptyDir in dev) |
| `Ingress`† | `event-watch` | WS-friendly NGINX example |
| `ServiceMonitor`† | `event-watch` | kube-prometheus-stack scrape config |

† = prod overlay only.

## Why `replicas: 1`?

The server's fan-out is in-process only right now — a subscriber connected
to pod A will not see events published to pod B. Multi-pod fan-out requires
wiring the Redis Pub/Sub bridge (the `Store` interface already exposes
`Notify` and `Watch` for this; nothing calls them yet). Until that's in
place, running more than one replica silently splits your subscription
graph. If you want horizontal scale on the read path (many concurrent
subscribers), file an issue and this becomes the top priority.

The Deployment uses `strategy: RollingUpdate` with `maxUnavailable: 0` +
`maxSurge: 1` so image rolls still cause zero-downtime — a new pod comes
up, its readiness probe passes, the old one drains for
`terminationGracePeriodSeconds` (30s), WS clients reconnect + resume.

## Configuration knobs

All server settings are env vars on the Deployment (via the ConfigMap +
Secret envFrom). Full list in [docs/build.md](../../docs/build.md).

Common tweaks:

- **Enable auth**: set `EW_AUTH=bearer` in `configmap.yaml`, populate `EW_AUTH_TOKEN` in the Secret, restart the pod.
- **Change TTL**: `EW_DEFAULT_TTL: 24h` (Go duration).
- **Change archive interval**: `EW_ARCHIVE_INTERVAL: 10m`.
- **Point at an external Redis** (e.g. ElastiCache): set `EW_REDIS_ADDR: my-redis.abc.cache.amazonaws.com:6379` in ConfigMap, add `EW_REDIS_PASSWORD` to the Secret, and remove the Redis StatefulSet + Service from the overlay's resource list.

## Upgrading

```bash
# 1. Build and push new image
docker buildx build --platform linux/amd64,linux/arm64 \
  -f deploy/Dockerfile \
  -t ghcr.io/geekychris/event_watch:v0.2.0 \
  --push .

# 2. Bump the tag in your overlay
sed -i '' 's|v0.1.0|v0.2.0|' deploy/k8s/overlays/prod/kustomization.yaml

# 3. Apply
kubectl apply -k deploy/k8s/overlays/prod
```

The rolling update is transparent to clients — WS reconnects auto-resume
from `lastSeen+1`.

## Troubleshooting

**Pods stuck `Pending`** — usually the PVC. Check `kubectl get pvc -n event-watch`. If your cluster has no default StorageClass, add one or use the dev overlay (emptyDir).

**Server can't reach Redis** — `kubectl -n event-watch logs deploy/event-watch` will say `dial tcp: lookup event-watch-redis`. Confirm the Redis pod is `Ready`, and that the ConfigMap `EW_REDIS_ADDR` matches the Service name.

**WebSocket disconnects after 60s on the ingress** — your ingress controller is closing idle upgraded connections. See the annotations in `extras/ingress.yaml` and adapt for your controller (Traefik uses different annotation names; GCE/GKE Ingress needs `BackendConfig` with `timeoutSec: 3600`).

**ServiceMonitor "no matches for kind"** — you don't have the Prometheus Operator installed. Remove `servicemonitor.yaml` from `extras/kustomization.yaml`.

## Verify without a cluster

```bash
kubectl kustomize deploy/k8s/overlays/dev  | kubectl apply --dry-run=client -f -
kubectl kustomize deploy/k8s/overlays/prod | kubectl apply --dry-run=client -f -
```
