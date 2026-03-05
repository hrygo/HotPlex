*Read this in other languages: [English](docker-deployment.md), [简体中文](docker-deployment_zh.md).*

# Docker Deployment Guide

## Quick Start

### 1. Build the Image

HotPlex provides two ways to build Docker images:

#### Option A: Pure Build (hotplex-only)
Minimal image containing only the hotplexd binary (~20MB).

```bash
make docker-build
```

#### Option B: All-in-One Build
Includes hotplexd and Claude Code CLI, ready to use out of the box with host volume mappings.

```bash
make docker-build DOCKER_IMAGE=hotplex-ai
```

### 2. Run the Container

#### Pure Build Usage

```bash
make docker-run
# or
docker run -d \
  --name hotplex \
  -p 8080:8080 \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -e CLAUDE_API_KEY=your-key \
  hotplex:latest
```

#### All-in-One Build Usage (Recommended)

This method seamlessly integrates with your host machine's configuration:

```bash
# Using Makefile
make docker-run DOCKER_IMAGE=hotplex-ai
# or manually
docker run -d \
  --name hotplex-ai \
  -p 8080:8080 \
  -v $HOME/.hotplex:/.hotplex \
  -v $HOME/.claude/settings.json:/home/hotplex/.claude/settings.json:ro \
  -v $HOME/.claude/projects:/home/hotplex/.claude/projects:rw \
  -v $HOME/projects:/home/hotplex/projects:rw \
  hotplex-ai:latest
```

**Volume Mapping Explanation**:
| Host Path                     | Container Path                        | Mode       | Description          |
| ----------------------------- | ------------------------------------- | ---------- | -------------------- |
| `$HOME/.claude/settings.json` | `/home/hotplex/.claude/settings.json` | Read-only  | Claude Code settings |
| `$HOME/.claude/projects`      | `/home/hotplex/.claude/projects`      | Read/Write | Chat histories       |
| `$HOME/.hotplex`              | `/.hotplex`                           | Read/Write | HotPlex config       |
| `$HOME/projects`              | `/home/hotplex/projects`              | Read/Write | Workspace            |

### 3. Multi-Platform Build (amd64 + arm64)

```bash
make docker-buildx
```



## Docker Compose

```yaml
version: '3.8'
services:
  hotplex:
    image: hotplex:latest
    ports:
      - "8080:8080"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - hotplex-data:/data
    environment:
      - HOTPLEX_PORT=8080
      - HOTPLEX_LOG_LEVEL=info
      - HOTPLEX_IDLE_TIMEOUT=30m
    healthcheck:
      test: ["CMD", "wget", "-q", "--spider", "http://localhost:8080/health"]
      interval: 30s
      timeout: 10s
      retries: 3
    restart: unless-stopped

volumes:
  hotplex-data:
```

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
