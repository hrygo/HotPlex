*Read this in other languages: [English](production-guide.md), [简体中文](production-guide_zh.md).*

# Production Deployment Guide

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────┐
│                     Load Balancer                           │
│                  (nginx / cloud LB)                         │
└─────────────────────────────────────────────────────────────┘
                            │
         ┌──────────────────┼──────────────────┐
         ▼                  ▼                  ▼
   ┌──────────┐       ┌──────────┐       ┌──────────┐
   │ HotPlex  │       │ HotPlex  │       │ HotPlex  │
   │  Node 1  │       │  Node 2  │       │  Node 3  │
   └──────────┘       └──────────┘       └──────────┘
         │                  │                  │
         └──────────────────┴──────────────────┘
                            │
         ┌──────────────────┼──────────────────┐
         ▼                  ▼                  ▼
   ┌──────────┐       ┌──────────┐       ┌──────────┐
   │ Prometheus│       │  Jaeger  │       │  Loki    │
   │ (metrics)│       │ (traces) │       │  (logs)  │
   └──────────┘       └──────────┘       └──────────┘
                            │
                            ▼
         ┌──────────────────┴──────────────────┐
         ▼                                     ▼
   ┌──────────┐                         ┌──────────┐
   │  Slack   │                         │ DingTalk │
   │  Alerts  │                         │  Alerts  │
   └──────────┘                         └──────────┘
```

## Scaling Guidelines

| Concurrent Users | Instances | CPU/Instance | Memory/Instance |
| ---------------- | --------- | ------------ | --------------- |
| 1-100            | 1         | 0.5 core     | 512MB           |
| 100-500          | 2-3       | 1 core       | 1GB             |
| 500-2000         | 5-10      | 2 core       | 2GB             |
| 2000+            | 10+       | 2-4 core     | 2-4GB           |

## Configuration

### ChatApps Platform Configuration

Create configuration files in `configs/chatapps/` directory:

```yaml
# configs/chatapps/slack.yaml
platform: slack
system_prompt: |
  You are an AI coding assistant.
task_instructions: |
  Help users with coding tasks.
engine:
  timeout: 5m
  idle_timeout: 30m
  work_dir: /tmp/hotplex
  allowed_tools:
    - Bash
    - Edit
    - Glob
    - Read
provider:
  type: claude-code
  model: claude-sonnet-4-20250514
security:
  verify_signature: true
  permission:
    dm_policy: allow
    group_policy: allow
    bot_user_id: U1234567890
```

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `HOTPLEX_PORT` | HTTP server port | `8080` |
| `HOTPLEX_LOG_LEVEL` | Log level (debug/info/warn/error) | `info` |
| `HOTPLEX_CONFIG_DIR` | Configuration directory | `./configs` |
| `HOTPLEX_METRICS_PATH` | Metrics endpoint path | `/metrics` |

## Monitoring

### Prometheus Configuration

```yaml
scrape_configs:
  - job_name: 'hotplex'
    static_configs:
      - targets: ['hotplex:8080']
    metrics_path: /metrics
```

### Key Metrics

#### Engine Metrics
- `hotplex_engine_sessions_active`: Number of active sessions
- `hotplex_engine_sessions_total`: Total sessions created
- `hotplex_engine_executions_total`: Total executions
- `hotplex_engine_execution_duration_seconds`: Execution duration

#### ChatApps Metrics
- `hotplex_chatapps_messages_received_total`: Messages received by platform
- `hotplex_chatapps_messages_sent_total`: Messages sent to platform
- `hotplex_chatapps_processing_duration_seconds`: Message processing time
- `hotplex_chatapps_errors_total`: Processing errors

#### Provider Metrics
- `hotplex_provider_tokens_total`: Total tokens used
- `hotplex_provider_cost_usd_total`: Total cost in USD
- `hotplex_provider_tool_invocations_total`: Tool invocation count

### Grafana Dashboard

Key panels:
- Active Sessions
- Request Latency (p50, p95, p99)
- Error Rate by Platform
- Token Usage & Cost
- Tool Invocation Rate

### Alerting Rules

```yaml
groups:
- name: hotplex
  rules:
  - alert: HighErrorRate
    expr: rate(hotplex_engine_errors_total[5m]) > 0.1
    for: 5m
    labels:
      severity: warning
    annotations:
      summary: High error rate detected

  - alert: SessionPoolExhausted
    expr: hotplex_engine_sessions_active > 800
    for: 2m
    labels:
      severity: critical
    annotations:
      summary: Session pool nearly exhausted

  - alert: HighLatency
    expr: histogram_quantile(0.95, rate(hotplex_engine_execution_duration_seconds_bucket[5m])) > 60
    for: 5m
    labels:
      severity: warning
    annotations:
      summary: 95th percentile latency exceeds 60s

  - alert: PlatformRateLimit
    expr: rate(hotplex_chatapps_rate_limits_total[5m]) > 0
    for: 1m
    labels:
      severity: warning
    annotations:
      summary: Platform rate limit triggered
```

## Security Checklist

- [x] Enable TLS termination at LB
- [x] Configure network policies
- [x] Enable rate limiting per platform
- [x] Enable signature verification (Slack/DingTalk/Feishu)
- [x] Set resource limits
- [x] Enable audit logging
- [x] Configure WAF patterns
- [x] Set AllowedTools whitelist

### Platform-Specific Security

#### Slack
- Verify request signature (`X-Slack-Signature`)
- Use Socket Mode for real-time events
- Configure OAuth scopes properly

#### DingTalk
- Verify callback signature
- Validate timestamp to prevent replay attacks

#### Feishu
- Verify signature with SHA256
- Validate request timestamp

## Health Checks

### Endpoint
```
GET /health
```

Response:
```json
{
  "status": "healthy",
  "version": "v0.17.0",
  "uptime": "24h30m",
  "active_sessions": 42
}
```

### Readiness Probe
```
GET /ready
```

Returns 200 when ready to accept traffic.

## Backup & Recovery

### Session State

Sessions are ephemeral (Hot-Multiplexing). No persistent state to backup.
For critical sessions, use session persistence markers in `internal/persistence/`.

### Configuration

```bash
# Backup configs
kubectl get configmap hotplex-config -o yaml > hotplex-config-backup.yaml

# Restore configs
kubectl apply -f hotplex-config-backup.yaml
```

## Troubleshooting

### High Memory Usage

```bash
# Check heap profile
kubectl exec -it hotplex-xxx -- curl localhost:8080/debug/pprof/heap

# Check active sessions
curl http://hotplex:8080/metrics | grep hotplex_engine_sessions_active
```

### Slow Requests

Check traces in Jaeger for bottleneck spans.

### Session Leaks

```bash
# Monitor active sessions
curl http://hotplex:8080/metrics | grep hotplex_engine_sessions_active

# Check for zombie processes
ps aux | grep -E "(claude-code|opencode)" | grep defunct
```

### Platform Integration Issues

#### Slack
- Verify App Token and Bot Token validity
- Check Socket Mode connection status
- Review Slack API rate limits

#### DingTalk
- Validate AppID and AppSecret
- Check callback URL accessibility
- Verify callback token configuration

## Docker Deployment

```yaml
# docker-compose.yaml
version: '3.8'
services:
  hotplex:
    image: hotplex:latest
    ports:
      - "8080:8080"
    volumes:
      - ./configs:/app/configs
      - /tmp/hotplex:/tmp/hotplex
    environment:
      - HOTPLEX_LOG_LEVEL=info
      - HOTPLEX_CONFIG_DIR=/app/configs
    restart: unless-stopped
    resources:
      limits:
        cpu: "2"
        memory: 2Gi
```

## Kubernetes Deployment

```yaml
# deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: hotplex
spec:
  replicas: 3
  selector:
    matchLabels:
      app: hotplex
  template:
    metadata:
      labels:
        app: hotplex
    spec:
      containers:
      - name: hotplex
        image: hotplex:latest
        ports:
        - containerPort: 8080
        resources:
          requests:
            cpu: "1"
            memory: 1Gi
          limits:
            cpu: "2"
            memory: 2Gi
        env:
        - name: HOTPLEX_LOG_LEVEL
          value: "info"
        volumeMounts:
        - name: config
          mountPath: /app/configs
      volumes:
      - name: config
        configMap:
          name: hotplex-config
```

---

*For more details, see [Architecture Documentation](architecture.md) and [SDK Guide](sdk-guide.md).*
