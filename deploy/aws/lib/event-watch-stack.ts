// One stack that stands up event_watch on AWS:
//   * VPC (2 AZs)
//   * ECR — image built + pushed automatically via ContainerImage.fromAsset
//   * ElastiCache Redis (single-node) — durable event + state store
//   * ECS Fargate service — one task by default (in-process fan-out; multi-
//     replica needs the Redis Pub/Sub bridge that's stubbed in the Store
//     interface but not yet wired)
//   * Application Load Balancer with WS-friendly idle timeout (3600s)
//   * CloudWatch log group for container stdout
//   * Optional HTTPS via passed cert ARN + Route 53 alias
//
// The stack is intentionally one file so it's easy to read end-to-end. If
// you want to slice it into network / data / compute, do that later.
import * as cdk from 'aws-cdk-lib';
import * as ec2 from 'aws-cdk-lib/aws-ec2';
import * as ecs from 'aws-cdk-lib/aws-ecs';
import * as elbv2 from 'aws-cdk-lib/aws-elasticloadbalancingv2';
import * as elasticache from 'aws-cdk-lib/aws-elasticache';
import * as logs from 'aws-cdk-lib/aws-logs';
import * as ecr_assets from 'aws-cdk-lib/aws-ecr-assets';
import * as route53 from 'aws-cdk-lib/aws-route53';
import * as route53_targets from 'aws-cdk-lib/aws-route53-targets';
import * as acm from 'aws-cdk-lib/aws-certificatemanager';
import { Construct } from 'constructs';
import * as path from 'path';

export interface EventWatchStackProps extends cdk.StackProps {
  cpu: number;               // Fargate CPU units (256 = 0.25 vCPU)
  memoryMiB: number;         // Fargate memory
  redisNodeType: string;     // e.g. 'cache.t4g.micro'
  desiredCount: number;      // >1 requires Pub/Sub fan-out — see comments
  publicSubnets: boolean;    // put tasks in public subnets (saves NAT)
  certificateArn?: string;   // optional ACM cert (must be in same region)
  domainName?: string;       // optional FQDN to point at the ALB
  hostedZoneName?: string;   // e.g. "example.com" — must already exist
}

export class EventWatchStack extends cdk.Stack {
  constructor(scope: Construct, id: string, props: EventWatchStackProps) {
    super(scope, id, props);

    // ------ 1. Network -----------------------------------------------------
    const vpc = new ec2.Vpc(this, 'Vpc', {
      maxAzs: 2,
      // For dev: only public subnets (no NAT gateway = no $32/mo).
      // For prod: public (for ALB) + private-with-egress (for tasks + Redis).
      subnetConfiguration: props.publicSubnets
        ? [{ name: 'public', subnetType: ec2.SubnetType.PUBLIC, cidrMask: 24 }]
        : [
            { name: 'public',  subnetType: ec2.SubnetType.PUBLIC,           cidrMask: 24 },
            { name: 'private', subnetType: ec2.SubnetType.PRIVATE_WITH_EGRESS, cidrMask: 24 },
          ],
      natGateways: props.publicSubnets ? 0 : 1,
    });

    // ------ 2. Redis (ElastiCache) ----------------------------------------
    // Single node — matches our replicas=1 constraint. Move to a replication
    // group when the Pub/Sub fan-out bridge lands and we can run >1 task.
    const redisSg = new ec2.SecurityGroup(this, 'RedisSg', {
      vpc, description: 'event_watch redis', allowAllOutbound: false,
    });
    const redisSubnetGroup = new elasticache.CfnSubnetGroup(this, 'RedisSubnetGroup', {
      description: 'event_watch redis subnets',
      subnetIds: props.publicSubnets
        ? vpc.publicSubnets.map((s) => s.subnetId)
        : vpc.privateSubnets.map((s) => s.subnetId),
    });
    const redis = new elasticache.CfnCacheCluster(this, 'Redis', {
      engine: 'redis',
      cacheNodeType: props.redisNodeType,
      numCacheNodes: 1,
      engineVersion: '7.1',
      cacheSubnetGroupName: redisSubnetGroup.ref,
      vpcSecurityGroupIds: [redisSg.securityGroupId],
      // No public access — tasks and Redis share the VPC.
    });

    // ------ 3. Fargate service -------------------------------------------
    const logGroup = new logs.LogGroup(this, 'ServerLogs', {
      retention: logs.RetentionDays.TWO_WEEKS,
      removalPolicy: cdk.RemovalPolicy.DESTROY,
    });

    const cluster = new ecs.Cluster(this, 'Cluster', { vpc, containerInsights: false });

    // CDK builds the image from the repo root (../..) using deploy/Dockerfile
    // and pushes it to a CDK-managed ECR repo. Every `cdk deploy` produces a
    // new immutable tag and rolls the service.
    const image = ecs.ContainerImage.fromDockerImageAsset(
      new ecr_assets.DockerImageAsset(this, 'ServerImage', {
        directory: path.resolve(__dirname, '..', '..', '..'),
        file: 'deploy/Dockerfile',
        platform: ecr_assets.Platform.LINUX_AMD64,
        // Prevent CDK from copying its own build output (cdk.out) and every
        // node_modules tree in the repo into the staging area. Without this
        // the copy would recurse infinitely under deploy/aws/cdk.out/.
        exclude: [
          'deploy/aws/cdk.out',
          'deploy/aws/node_modules',
          '**/node_modules',
          '**/dist',
          '**/target',
          '**/build',
          '.git',
          'docs',
          'clients',           // client libs — server binary doesn't need them
          'wails-client',
          'deploy/k8s',
        ],
      }),
    );

    const taskDef = new ecs.FargateTaskDefinition(this, 'TaskDef', {
      cpu: props.cpu,
      memoryLimitMiB: props.memoryMiB,
      runtimePlatform: {
        cpuArchitecture: ecs.CpuArchitecture.X86_64,
        operatingSystemFamily: ecs.OperatingSystemFamily.LINUX,
      },
    });

    const container = taskDef.addContainer('server', {
      image,
      containerName: 'server',
      logging: ecs.LogDrivers.awsLogs({ streamPrefix: 'server', logGroup }),
      environment: {
        EW_ADDR:         ':8080',
        EW_STORE:        'redis',
        EW_REDIS_ADDR:   `${redis.attrRedisEndpointAddress}:${redis.attrRedisEndpointPort}`,
        EW_DEFAULT_TTL:  '168h',
        EW_ARCHIVE_INTERVAL: '5m',
      },
      portMappings: [{ containerPort: 8080, protocol: ecs.Protocol.TCP }],
    });

    const service = new ecs.FargateService(this, 'Service', {
      cluster,
      taskDefinition: taskDef,
      // NOTE: >1 requires wiring the Redis Pub/Sub bridge on the Store
      // interface (Notify/Watch exist but nothing calls them). Until then
      // a second task would silently split the subscription graph.
      desiredCount: props.desiredCount,
      minHealthyPercent: 0,
      maxHealthyPercent: 200,
      assignPublicIp: props.publicSubnets,
      vpcSubnets: {
        subnetType: props.publicSubnets ? ec2.SubnetType.PUBLIC : ec2.SubnetType.PRIVATE_WITH_EGRESS,
      },
      circuitBreaker: { rollback: true },
    });

    // Allow tasks to reach Redis on 6379.
    redisSg.addIngressRule(
      service.connections.securityGroups[0],
      ec2.Port.tcp(6379),
      'event_watch tasks -> redis',
    );

    // ------ 4. ALB -------------------------------------------------------
    const alb = new elbv2.ApplicationLoadBalancer(this, 'Alb', {
      vpc, internetFacing: true,
      // 60s is default — bump for WebSocket long connections.
      idleTimeout: cdk.Duration.seconds(3600),
    });

    const targetGroup = new elbv2.ApplicationTargetGroup(this, 'Tg', {
      vpc,
      protocol: elbv2.ApplicationProtocol.HTTP,
      port: 8080,
      targetType: elbv2.TargetType.IP,
      healthCheck: {
        // Cheap, unauthenticated, exercises the metrics registry.
        path: '/admin/metrics.json',
        interval: cdk.Duration.seconds(15),
        timeout: cdk.Duration.seconds(5),
        healthyThresholdCount: 2,
        unhealthyThresholdCount: 3,
      },
      deregistrationDelay: cdk.Duration.seconds(15),
      // Sticky sessions matter for WebSocket only if we ever run >1 task;
      // enable now so it Just Works once desiredCount goes up.
      stickinessCookieDuration: cdk.Duration.hours(1),
    });
    service.attachToApplicationTargetGroup(targetGroup);

    if (props.certificateArn) {
      // HTTPS + HTTP-redirect
      alb.addRedirect({ sourceProtocol: elbv2.ApplicationProtocol.HTTP,
                        sourcePort: 80,
                        targetProtocol: elbv2.ApplicationProtocol.HTTPS,
                        targetPort: 443 });
      alb.addListener('Https', {
        port: 443, protocol: elbv2.ApplicationProtocol.HTTPS,
        certificates: [{ certificateArn: props.certificateArn }],
        defaultTargetGroups: [targetGroup],
      });
    } else {
      alb.addListener('Http', {
        port: 80, protocol: elbv2.ApplicationProtocol.HTTP,
        defaultTargetGroups: [targetGroup],
      });
    }

    // Optional Route 53 alias to the ALB.
    if (props.domainName && props.hostedZoneName) {
      const zone = route53.HostedZone.fromLookup(this, 'Zone', {
        domainName: props.hostedZoneName,
      });
      new route53.ARecord(this, 'AlbAlias', {
        zone, recordName: props.domainName,
        target: route53.RecordTarget.fromAlias(new route53_targets.LoadBalancerTarget(alb)),
      });
    }

    // ------ 5. Outputs ---------------------------------------------------
    new cdk.CfnOutput(this, 'AlbDnsName', {
      value: alb.loadBalancerDnsName,
      description: 'Public DNS of the ALB (use ws://<dns>/ws from clients)',
    });
    new cdk.CfnOutput(this, 'RedisEndpoint', {
      value: `${redis.attrRedisEndpointAddress}:${redis.attrRedisEndpointPort}`,
      description: 'ElastiCache Redis endpoint (VPC-internal)',
    });
    if (props.domainName) {
      new cdk.CfnOutput(this, 'PublicUrl', {
        value: `${props.certificateArn ? 'https' : 'http'}://${props.domainName}/`,
      });
    }

    // Suppress the "certificate needed for HTTPS" concern from cdk-nag if
    // the caller opted out of HTTPS explicitly by not passing a cert ARN.
    // (Comment for humans; cdk-nag isn't wired here.)
  }
}
