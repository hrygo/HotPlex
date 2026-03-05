*Read this in other languages: [English](docker-deployment.md), [简体中文](docker-deployment_zh.md).*

# Docker Deployment Guide

## Quick Start

### 1. Build the Image

```bash
# Build the All-in-One image (includes Claude Code CLI)
make docker-build
```

**💡 Tip: Customize Your Development Environment**
The provided `Dockerfile` serves as a foundation. Since AI coding tools (like Claude Code) rely on the container's OS environment to execute specific tasks—such as building frontends, running Python scripts, or using specific CLI tools—we highly recommend that you **open the `Dockerfile` and install the specific dependencies or languages needed for your tech stack** (e.g., `Node.js`, `Python`, `Rust`) before running the build command above.

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

## Networking and Proxy Configuration (macOS/Windows/Linux)

Due to Docker's network isolation, accessing your host machine's proxy from within the container requires specific configuration.

In the provided `docker-compose.yml`, we have configured `extra_hosts: - "host.docker.internal:host-gateway"`. This ensures that the `host.docker.internal` DNS name—which is natively available on macOS and Windows via Docker Desktop—is **fully compatible and functional on Linux systems as well.**

Please identify your user profile and apply the corresponding proxy variables in your `.env` or `docker-compose.yml`:

### Profile 1: Standard Network User (No Proxy Needed)
**You only need to access domestic public networks (e.g., Baidu) or internal APIs.**
- **Action**: Do nothing. Container networks are NAT-bridged by default. 
- **Troubleshooting**: If you experience extreme slowness or timeout, it may be a local DNS issue. You can manually inject public DNS servers into your `docker-compose.yml`:
  ```yaml
  dns:
    - 223.5.5.5
    - 114.114.114.114
  ```

### Profile 2: Global Proxy User (Standard VPN/Clash)
**You run a proxy tool (e.g., Clash, V2Ray) on your host machine to access restricted overseas networks (e.g., Google).**
- **Action 1 (Crucial)**: Open your proxy client's settings on your host machine and enable **"Allow LAN (允许局域网连接)"**.
- **Action 2**: Inject the general proxy variable. HotPlex will map `host.docker.internal` dynamically.
  ```yaml
  environment:
    - HTTP_PROXY=http://host.docker.internal:<YOUR_PROXY_PORT>
    - HTTPS_PROXY=http://host.docker.internal:<YOUR_PROXY_PORT>
  ```

### Profile 3: Advanced User (Dedicated LLM Proxy)
**You have complex network environments locally, splitting general proxy for internet browsing and a dedicated proxy strictly for LLM (Large Language Model) API requests.**
- **Action**: HotPlex provides granular proxy control via `ANTHROPIC_BASE_URL` independent of standard HTTP proxies. Set them up concurrently without conflict:
  ```yaml
  environment:
    # 1. Dedicated tunnel strictly for the LLM API gateway
    - ANTHROPIC_BASE_URL=http://host.docker.internal:15721
    # 2. General proxy for standard web plugins or crawling
    - HTTP_PROXY=http://host.docker.internal:7897
    - HTTPS_PROXY=http://host.docker.internal:7897
    # 3. Prevent proxy loopback issues
    - NO_PROXY=localhost,127.0.0.1,host.docker.internal
  ```

### Verify Connectivity
```bash
# Verify if the container can reach your host proxy
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
