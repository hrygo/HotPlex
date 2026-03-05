*Read this in other languages: [English](docker-deployment.md), [简体中文](docker-deployment_zh.md).*

# Docker Deployment Guide

## Quick Start

### 1. Build the Image

```bash
# Build the All-in-One image (includes Claude Code CLI)
make docker-build
```

### 2. Run the Container (Recommended)

This method seamlessly integrates with your host machine's configuration:

```bash
# Using Makefile
make docker-run
# or manually
docker run -d \
  --name hotplex \
  -p 18080:8080 \
  -v $HOME/.hotplex:/.hotplex \
  -v $HOME/.claude:/home/hotplex/.claude:rw \
  -v $HOME/.claude.json:/home/hotplex/.claude.json:rw \
  -v $HOME/projects:/home/hotplex/projects:rw \
  hotplex:latest
```

> [!NOTE]
> **Slack App Compatibility**: Changing the host port to `18080` does **not** affect Slack connectivity. HotPlex defaults to **Socket Mode**, which uses outbound WebSocket connections. Host port mapping is only used for local Health Checks (`http://localhost:18080/health`) and internal metrics.

**Volume Mapping Explanation**:
| Host Path            | Container Path               | Mode       | Description                            |
| -------------------- | ---------------------------- | ---------- | -------------------------------------- |
| `$HOME/.claude`      | `/home/hotplex/.claude`      | Read/Write | History, skills, plugins, and settings |
| `$HOME/.claude.json` | `/home/hotplex/.claude.json` | Read/Write | Authentication and MCP servers         |
| `$HOME/.hotplex`     | `/.hotplex`                  | Read/Write | HotPlex config                         |
| `$HOME/projects`     | `/home/hotplex/projects`     | Read/Write | Workspace                              |

### 3. Multi-Platform Build (amd64 + arm64)

```bash
make docker-buildx
```

## Docker Compose (Recommended)

To simplify management, we provide a `docker-compose.yml`.

### Start the Container
```bash
docker compose up -d
```

### View Logs
```bash
docker compose logs -f
```

### Stop Containers
```bash
docker compose down
```

**Note:** Ensure your `.claude/settings.json` and `.hotplex` directories exist on your host before running the composed setup to prevent Docker from creating them as `root` directories.

## Kubernetes Deployment

```yaml
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
        livenessProbe:
          httpGet:
            path: /health/live
            port: 8080
          initialDelaySeconds: 10
          periodSeconds: 10
        readinessProbe:
          httpGet:
            path: /health/ready
            port: 8080
          initialDelaySeconds: 5
          periodSeconds: 5
        resources:
          requests:
            cpu: 100m
            memory: 256Mi
          limits:
            cpu: 500m
            memory: 512Mi
---
apiVersion: v1
kind: Service
metadata:
  name: hotplex
spec:
  selector:
    app: hotplex
  ports:
  - port: 80
    targetPort: 8080
```

## Configuration

| Variable             | Default | Description             |
| -------------------- | ------- | ----------------------- |
| HOTPLEX_PORT         | 8080    | Server port             |
| HOTPLEX_LOG_LEVEL    | info    | Log level               |
| HOTPLEX_IDLE_TIMEOUT | 30m     | Session idle timeout    |
| OTEL_ENDPOINT        | -       | OpenTelemetry endpoint  |
| MAX_SESSIONS         | 1000    | Max concurrent sessions |
