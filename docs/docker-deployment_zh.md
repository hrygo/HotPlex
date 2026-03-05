*Read this in other languages: [English](docker-deployment.md), [简体中文](docker-deployment_zh.md).*

# Docker 部署指南

## 快速入门

### 1. 构建镜像

```bash
# 构建 All-in-One 镜像（包含 Claude Code CLI）
make docker-build
```

### 2. 运行容器 (推荐)

此方案可以无缝集成您宿主机已有的配置文件与模型：

```bash
# 使用 Makefile 运行
make docker-run
# 或手动运行 (请根据实际端口替换 15721/7897)
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
> **多机器人隔离**: 如果您运行多个机器人，建议为每个机器人指定独立的宿主机目录映射到 `/home/hotplex/projects`，这样可以确保会话记录和临时文件互不干扰。

> [!NOTE]
> **Slack App 兼容性**: 将主机端口改为 `18080` **不会** 影响 Slack 连接。HotPlex 默认使用 **Socket Mode**（WebSocket 模式），这属于主动向外发起连接。主机端口映射仅用于本地健康检查 (`http://localhost:18080/health`) 和内部指标监控。

**目录映射说明**：
| 宿主机路径            | 容器内路径                   | 模式 | 说明                       |
| --------------------- | ---------------------------- | ---- | -------------------------- |
| `$HOME/.claude`       | `/home/hotplex/.claude`      | 读写 | 历史记录、插件、技能与配置 |
| `$HOME/.claude.json`  | `/home/hotplex/.claude.json` | 读写 | 认证信息与 MCP 服务器配置  |
| `$HOME/.hotplex`      | `/home/hotplex/.hotplex`     | 读写 | 会话、标记位与自定义配置   |
| `$HOME/.slack/BOT_ID` | `/home/hotplex/projects`     | 读写 | **机器人隔离工作目录**     |

## 进阶：多机器人与配置优先级

HotPlex 支持在 Docker 中运行多个具有独立身份（Token）的机器人。

### 1. 配置加载策略
程序按以下顺序搜索配置文件：
1. `HOTPLEX_CHATAPPS_CONFIG_DIR` 环境变量 (最高)
2. `~/.hotplex/configs` (用户级同步配置)
3. `./chatapps/configs` (默认路径)

### 2. Docker Compose 最佳实践
在 `docker-compose.yml` 中：
- **主机器人**: 建议使用 `user config` 模式，通过 `make docker-sync` 统一维护。
- **副机器人**: 建议使用 `environment` 显式指定，并挂载特定的配置文件副本。

### 3. 多平台构建 (amd64 + arm64)

```bash
make docker-buildx
```

## Docker Compose 配置 (推荐)

为了简化管理，我们提供了 `docker-compose.yml`。

### 启动容器
```bash
docker compose up -d
```

### 查看日志
```bash
docker compose logs -f
```

### 停止容器
```bash
docker compose down
```

**运行前提:** 在启动前，请确保宿主机上已存在 `.claude/settings.json` 和 `.hotplex` 目录，否则 Docker 可能会以 `root` 权限创建这些目录。

## 网络与代理配置 (针对 macOS/Windows)

在 Docker 容器内访问宿主机代理需要特殊配置。

### 1. 核心概念
- **`host.docker.internal`**: Docker 提供的特殊 DNS 名称，用于在容器内访问宿主机。
- **允许局域网连接 (Allow LAN)**: **必须**在您的代理软件（Clash, V2Ray 等）中开启此选项，否则宿主机会拒绝来自容器虚拟网卡的连接。

### 2. 代理区分
为了获得最佳体验，我们建议在环境或 `docker-compose.yml` 中区分两类代理：
- **LLM 专用代理 (`ANTHROPIC_BASE_URL`)**: 指向您的 AI 接口专用通道（如端口 15721）。
- **通用系统代理 (`HTTP_PROXY`)**: 指向您的常规上网插件（如 Clash 端口 7897）。

### 3. 验证网络
```bash
# 检查容器是否能连通宿主机代理
make docker-check-net
```

## Kubernetes 部署

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

## 配置参数

| 变量                 | 默认值 | 描述                   |
| -------------------- | ------ | ---------------------- |
| HOTPLEX_PORT         | 8080   | 服务端口               |
| HOTPLEX_LOG_LEVEL    | info   | 日志级别               |
| HOTPLEX_IDLE_TIMEOUT | 30m    | 会话空闲超时时间       |
| OTEL_ENDPOINT        | -      | OpenTelemetry 接口地址 |
| MAX_SESSIONS         | 1000   | 最大并发会话数         |
