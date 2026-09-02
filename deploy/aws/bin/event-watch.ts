#!/usr/bin/env node
// CDK app entry. Two stacks: EventWatchDev (public, small, no cert) and
// EventWatchProd (adds HTTPS if you pass a cert ARN + hosted-zone name via
// CDK context). Everything else is the same shape so `cdk diff` between
// them is meaningful.
import 'source-map-support/register';
import * as cdk from 'aws-cdk-lib';
import { EventWatchStack } from '../lib/event-watch-stack';

const app = new cdk.App();

const env = {
  account: process.env.CDK_DEFAULT_ACCOUNT,
  region:  process.env.CDK_DEFAULT_REGION || 'us-east-1',
};

// --- Dev: HTTP only, cheapest options ------------------------------------
new EventWatchStack(app, 'EventWatchDev', {
  env,
  description: 'event_watch (dev) — HTTP-only ALB + Fargate + ElastiCache Redis',
  tags: { Env: 'dev', Project: 'event-watch' },

  // Cost/simplicity knobs for dev:
  cpu:            256,               // 0.25 vCPU
  memoryMiB:      512,
  redisNodeType:  'cache.t4g.micro',
  desiredCount:   1,                 // see EventWatchStack comment
  publicSubnets:  true,              // saves NAT gateway cost
});

// --- Prod: pass -c certificateArn=... -c domainName=... -c hostedZoneName=...
// to enable HTTPS + custom domain. Without them, prod deploys HTTP-only.
new EventWatchStack(app, 'EventWatchProd', {
  env,
  description: 'event_watch (prod) — HTTPS ALB + Fargate + ElastiCache Redis',
  tags: { Env: 'prod', Project: 'event-watch' },

  cpu:            1024,              // 1 vCPU
  memoryMiB:      2048,
  redisNodeType:  'cache.t4g.small',
  desiredCount:   1,                 // see EventWatchStack comment
  publicSubnets:  false,             // private subnets for tasks; adds NAT
  certificateArn: app.node.tryGetContext('certificateArn'),
  domainName:     app.node.tryGetContext('domainName'),
  hostedZoneName: app.node.tryGetContext('hostedZoneName'),
});

app.synth();
