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
# 或手动运行
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
> **Slack App 兼容性**: 将主机端口改为 `18080` **不会** 影响 Slack 连接。HotPlex 默认使用 **Socket Mode**（WebSocket 模式），这属于主动向外发起连接。主机端口映射仅用于本地健康检查 (`http://localhost:18080/health`) 和内部指标监控。

**目录映射说明**：
| 宿主机路径           | 容器内路径                   | 模式 | 说明                       |
| -------------------- | ---------------------------- | ---- | -------------------------- |
| `$HOME/.claude`      | `/home/hotplex/.claude`      | 读写 | 历史记录、插件、技能与配置 |
| `$HOME/.claude.json` | `/home/hotplex/.claude.json` | 读写 | 认证信息与 MCP 服务器配置  |
| `$HOME/.hotplex`     | `/.hotplex`                  | 读写 | HotPlex 服务配置           |
| `$HOME/projects`     | `/home/hotplex/projects`     | 读写 | 项目工作目录               |

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

**注意:** 在运行之前，请确保宿主机上已存在 `.claude/settings.json` 和 `.hotplex` 目录，否则 Docker 可能会以 `root` 权限创建这些目录。

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
