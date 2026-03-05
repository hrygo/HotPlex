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
# or manually (replace 15721/7897 with your actual ports)
docker run -d \
  --name hotplex \
  -p 18080:8080 \
  --env-file .env \
  -e ANTHROPIC_BASE_URL=http://host.docker.internal:15721 \
  -e HTTP_PROXY=http://host.docker.internal:7897 \
  -e HTTPS_PROXY=http://host.docker.internal:7897 \
  --add-host=host.docker.internal:host-gateway \
  -v $HOME/.hotplex:/home/hotplex/.hotplex \
  -v $HOME/.claude:/home/hotplex/.claude:rw \
  -v $HOME/.claude.json:/home/hotplex/.claude.json:rw \
  -v $HOME/.slack/BOT_U0AHRCL1KCM:/home/hotplex/projects:rw \
  hotplex:latest
```

> [!TIP]
> **Multi-Bot Isolation**: If you run multiple bots, it is recommended to map a dedicated host directory for each bot to `/home/hotplex/projects`. This ensures that session logs and temporary files do not interfere with each other.

> [!NOTE]
> **Slack App Compatibility**: Changing the host port to `18080` does **not** affect Slack connectivity. HotPlex defaults to **Socket Mode**, which uses outbound WebSocket connections. Host port mapping is only used for local Health Checks (`http://localhost:18080/health`) and internal metrics.

**Volume Mapping Explanation**:
| Host Path             | Container Path               | Mode       | Description                            |
| --------------------- | ---------------------------- | ---------- | -------------------------------------- |
| `$HOME/.claude`       | `/home/hotplex/.claude`      | Read/Write | History, skills, plugins, and settings |
| `$HOME/.claude.json`  | `/home/hotplex/.claude.json` | Read/Write | Authentication and MCP servers         |
| `$HOME/.hotplex`      | `/home/hotplex/.hotplex`     | Read/Write | Sessions, markers, and custom configs  |
| `$HOME/.slack/BOT_ID` | `/home/hotplex/projects`     | Read/Write | **Isolated Bot Work Directory**        |

## Advanced: Multi-Bot & Config Precedence

HotPlex supports running multiple bots with independent identities (tokens) within Docker.

### 1. Configuration Loading Strategy
The engine searches for configuration files in the following priority order:
1. `HOTPLEX_CHATAPPS_CONFIG_DIR` environment variable (Highest)
2. `~/.hotplex/configs` (User-level synced configs)
3. `./chatapps/configs` (Default path)

### 2. Docker Compose Recommendation
In your `docker-compose.yml`:
- **Primary Bot**: Recommended to use the `user config` mode, managed globally via `make docker-sync`.
- **Secondary Bot**: Recommended to use explicit `environment` overrides with dedicated volume mounts for isolated configuration.


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

**Prerequisite:** Ensure your `.claude/settings.json` and `.hotplex` directories exist on your host before running the setup to prevent Docker from creating them as `root` directories.

## Networking and Proxy Configuration (macOS/Windows)

Accessing host proxies from within a Docker container requires specific configuration.

### 1. Core Concepts
- **`host.docker.internal`**: A special DNS name provided by Docker to access the host from within a container.
- **Allow LAN**: You **must** enable this option in your proxy software (Clash, V2Ray, etc.), otherwise the host will reject connections from the container's virtual network.

### 2. Proxy Separation
For the best experience, we recommend separating two types of proxies in your environment or `docker-compose.yml`:
- **Dedicated LLM Proxy (`ANTHROPIC_BASE_URL`)**: Points to your AI API gateway (e.g., port 15721).
- **General System Proxy (`HTTP_PROXY`)**: Points to your regular VPN/Proxy (e.g., Clash port 7897).

### 3. Verify Connectivity
```bash
# Verify if the container can reach host proxies
make docker-check-net
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
