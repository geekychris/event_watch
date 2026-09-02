# event_watch — AWS deployment (CDK)

Two CDK stacks (TypeScript, CDK v2) that stand event_watch up on AWS:

- **EventWatchDev** — HTTP-only ALB, Fargate task in a public subnet,
  ElastiCache `cache.t4g.micro`. Cheapest working shape.
- **EventWatchProd** — larger Fargate task, `cache.t4g.small`, private
  subnets + NAT, optional HTTPS via ACM cert + Route 53 alias.

Both are the same one-file stack (`lib/event-watch-stack.ts`) — the
differences are just constructor args + CDK context.

## What it deploys

```
                       Internet
                          │
                     ┌────▼────┐
                     │   ALB   │  :80 (dev) or :443 (prod, if cert)
                     │  ELBv2  │  idle timeout 3600s (WS-friendly)
                     └────┬────┘
                          │  HTTP :8080
                     ┌────▼────────┐
                     │ ECS Service │  Fargate, 1 task (see note below)
                     │ event_watch │  built from deploy/Dockerfile via
                     │  container  │  ContainerImage.fromAsset()
                     └────┬────────┘
                          │  redis:6379
                     ┌────▼─────────┐
                     │ ElastiCache  │  Redis 7, single node
                     │    Redis     │  (VPC-internal, no public access)
                     └──────────────┘
```

## Prereqs

- AWS CLI v2 + credentials (`aws configure` or an SSO profile)
- Node 22+ (for `npx cdk`)
- Docker (for the local image build — CDK invokes it)
- CDK bootstrapped in the target account+region: `npx cdk bootstrap aws://<acct>/<region>`

## Deploy — dev

```bash
cd deploy/aws
npm install

# offline sanity check (no AWS calls; validates the templates synth):
npx cdk synth EventWatchDev

# real deploy — builds + pushes the docker image, then rolls the ECS service:
npx cdk deploy EventWatchDev
```

After ~5 min the stack outputs an `AlbDnsName`. Point your clients at
`ws://<AlbDnsName>/ws` and `http://<AlbDnsName>/` (browser UI). Same
demo scripts as the local server; same client libraries; same wire
protocol.

## Deploy — prod

```bash
# One-time: create an ACM cert in the SAME region as your stack.
# (Or use `aws acm request-certificate --domain-name events.example.com --validation-method DNS`.)
#
# Then hand its ARN + your hosted-zone details to CDK via -c context:

npx cdk deploy EventWatchProd \
  -c certificateArn=arn:aws:acm:us-east-1:123456789012:certificate/... \
  -c domainName=events.example.com \
  -c hostedZoneName=example.com
```

Prod will:
- create Route 53 A record `events.example.com` → ALB
- open port 443 with your cert
- redirect 80 → 443
- put tasks in private subnets (adds a NAT Gateway, ~$32/mo)

Without those context values, `EventWatchProd` still deploys but stays
HTTP-only on the ALB DNS name.

## After-deploy demo

```bash
# get the ALB DNS
ALB=$(aws cloudformation describe-stacks --stack-name EventWatchDev \
  --query 'Stacks[0].Outputs[?OutputKey==`AlbDnsName`].OutputValue' --output text)

# publish an event
curl -X POST -H 'Content-Type: application/json' http://$ALB/publish \
  -d '{"topic":"int/aws-demo","type":"int_set","payload":{"value":100}}'
curl http://$ALB/state/int/aws-demo
# → {"value":100,"exists":true,"updated_at":"..."}

# open the built-in htmx UI
open http://$ALB/
```

## Cost sketch (us-east-1, list price, always-on)

| Resource | Dev | Prod |
|---|---|---|
| Fargate task (0.25 vCPU / 0.5 GB) | ~$9/mo | — |
| Fargate task (1 vCPU / 2 GB) | — | ~$36/mo |
| ElastiCache cache.t4g.micro | ~$12/mo | — |
| ElastiCache cache.t4g.small | — | ~$24/mo |
| ALB | ~$17/mo + $0.008/LCU-hr | ~$17/mo |
| NAT Gateway (prod only) | — | ~$32/mo |
| CloudWatch Logs (2wk retention) | few $ | few $ |
| **Estimated total** | **~$40/mo** | **~$115/mo** |

Data transfer, request volume, and any extras (Route53 hosted zone
$0.50/mo, ACM cert free) are on top.

## Why replicas = 1?

Same reason as the k8s deployment: the server's fan-out is in-process
only. Running `desiredCount: 2` today would silently split the
subscription graph — a subscriber connected to task A won't see events
published through task B. Multi-task fan-out needs the Redis Pub/Sub
bridge (`Store.Notify` / `Store.Watch` are stubbed in the interface;
nothing calls them yet). Until that lands, `desiredCount: 1`.

Rolling updates are still zero-downtime: `minHealthyPercent: 0` +
`maxHealthyPercent: 200` means CDK brings up a fresh task, waits for
health, drains the old one; WS clients auto-reconnect + resume from
`from_seq=lastSeen+1`.

## Upgrading

```bash
cd deploy/aws
npx cdk deploy EventWatchDev    # rebuilds the image, rolls the service
```

CDK's asset system hashes the docker context and only rebuilds/pushes
when something actually changed. First-run rebuilds; steady-state
`cdk deploy` on unchanged code is fast.

## Tear down

```bash
npx cdk destroy EventWatchDev
```

This removes the ALB, service, cluster, Redis, VPC, log group, and the
CDK-managed ECR repo + images. Anything created outside of CDK (a
Route 53 hosted zone, an ACM cert) is left alone.

## Design decisions worth calling out

- **One stack per env, not multiple stacks.** VPC + data + compute in
  one file keeps the reader (you) in one place. If you ever need to
  share the VPC across services, split it out.
- **Public subnets in dev.** Skips the ~$32/mo NAT Gateway. Fargate task
  gets a public IP; ALB fronts it. Not what you'd do for prod, but the
  right default for a demo/dev stack you spin up + tear down often.
- **Redis in the VPC, not managed elsewhere.** Ties Redis lifecycle to
  the stack (`cdk destroy` cleans up). For prod I'd point at an
  existing ElastiCache cluster and remove the `Redis` construct.
- **`ContainerImage.fromAsset` not a separate ECR repo.** CDK owns image
  lifecycle: `cdk deploy` builds, tags with the content hash, pushes,
  updates the task definition, rolls. No shell-script glue.
- **`idleTimeout: 3600s` on the ALB.** WebSockets stay open — the ALB's
  default 60s would murder them. This matches what NGINX ingress does
  in the k8s manifests.
- **Health check on `/admin/metrics.json`.** Cheap, unauthenticated
  (skips the auth middleware), exercises the metrics registry as a
  cheap "process actually initialized" check.

## Not included (intentional)

- **Autoscaling** — nothing to scale until fan-out is multi-task safe.
- **Redis HA / cluster mode / replicas** — single node matches the
  single task. Add `numCacheClusters: 2` and switch to a
  `ReplicationGroup` when scaling up the fleet.
- **Secrets Manager for the auth token** — currently EW_AUTH is unset
  (auth off). To enable, add a Secret and wire it as a `Secrets.fromSecretsManager`
  entry on the container. Docs at
  <https://docs.aws.amazon.com/cdk/api/v2/docs/aws-cdk-lib.aws_ecs.Secret.html>.
- **CloudFront in front of the ALB** — not necessary for a demo. Add
  one if you want edge caching for the htmx UI's static assets.
- **cdk-nag** — not wired. Add it in `bin/event-watch.ts` and audit
  before production use.
