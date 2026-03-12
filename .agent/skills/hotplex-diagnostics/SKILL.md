---
name: HotPlex Diagnostics
description: This skill should be used when the user asks to "diagnose hotplex", "check health", "view logs", "debug session", "check status", "get stats". Provides monitoring and diagnostic capabilities for hotplex services.
version: 0.1.0
---

# HotPlex Diagnostics

Monitor and diagnose hotplex service health, logs, and session statistics.

## Overview

This skill provides diagnostic capabilities for running hotplex containers. It includes log analysis, health checks, API status queries, and session debugging.

## Prerequisites

- HotPlex containers running via docker-compose
- Access to container ports (18080, 18081 by default)
- curl or wget for HTTP API calls

## Health Checks

### HTTP Health Endpoint

Check if a hotplex service is responding:

```bash
curl -s http://localhost:18080/health
curl -s http://localhost:18081/health
```

### Container Health Status

Check Docker container health:

```bash
docker inspect hotplex --format='{{.State.Health.Status}}'
```

## Log Analysis

### View Recent Logs

Get recent log entries:

```bash
docker compose logs --tail=200 hotplex
```

### Filter Logs by Level

Filter for specific log levels (requires JSON logging):

```bash
docker compose logs --filter "level=error" hotplex
```

### Follow Logs in Real-time

Stream logs continuously:

```bash
docker compose logs -f hotplex
```

## Session Statistics

### Get Session Stats via WebSocket

Query session statistics using the stats API:

```bash
# Via docker exec
docker exec hotplex sh -c 'echo {"type":"stats","session_id":"test"}' | wscat -c ws://localhost:8080/ws
```

### List Active Sessions

List running CLI processes inside container:

```bash
docker exec hotplex ps aux | grep -E "(claude|opencode)"
```

## API Diagnostics

### Check WebSocket Endpoint

Verify WebSocket connectivity:

```bash
curl -i -N \
  -H "Connection: Upgrade" \
  -H "Upgrade: websocket" \
  http://localhost:18080/ws
```

### Test Execute Endpoint

Test basic execution capability:

```bash
curl -X POST http://localhost:18080/api/execute \
  -H "Content-Type: application/json" \
  -d '{"prompt":"hello","session_id":"test"}'
```

## Performance Monitoring

### Container Resources

Monitor resource usage:

```bash
docker stats --no-stream --format "table {{.Name}}\t{{.CPUPerc}}\t{{.MemUsage}}" hotplex hotplex-secondary
```

### Disk Usage

Check container disk usage:

```bash
docker exec hotplex du -sh /home/hotplex/.hotplex
docker exec hotplex du -sh /home/hotplex/.claude
```

## Debugging Sessions

### Enter Container Shell

Interactive debugging:

```bash
docker exec -it hotplex /bin/sh
```

### Check Running Processes

View all processes inside container:

```bash
docker exec hotplex ps aux
```

### Network Diagnostics

Check network connectivity:

```bash
docker exec hotplex wget -qO- http://localhost:8080/health
docker exec hotplex nslookup host.docker.internal
```

## Configuration

Default ports:
- Primary bot: 18080
- Secondary bot: 18081

Health endpoint: `/health`
WebSocket endpoint: `/ws`

## Additional Resources

### Reference Files

- **`internal/server/hotplex_ws.go`** - WebSocket API implementation
- **`references/api-endpoints.md`** - Complete API documentation

### Related Skills

- **`docker-container-ops`** - For container lifecycle management
- **`hotplex-data-mgmt`** - For data and session management
