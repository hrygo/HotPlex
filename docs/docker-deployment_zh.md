*Read this in other languages: [English](docker-deployment.md), [简体中文](docker-deployment_zh.md).*

# Docker 部署指南

## 快速入门

### 1. 构建方式

HotPlex 提供两种 Docker 镜像构建方式：

#### 方式 1: 纯净构建 (hotplex-only)
仅包含 hotplexd 二进制文件，镜像体积极小（约 20MB）。

```bash
make docker-build
```

#### 方式 2: All-in-One 构建
包含 hotplexd 和 Claude Code CLI，支持直接映射宿主机的配置文件，开箱即用。

```bash
make docker-build DOCKER_IMAGE=hotplex-ai
```

### 2. 运行容器

#### 纯净构建运行方案

```bash
make docker-run
# 或手动运行
docker run -d \
  --name hotplex \
  -p 8080:8080 \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -e CLAUDE_API_KEY=your-key \
  hotplex:latest
```

#### All-in-One 构建运行方案 (推荐)

此方案可以无缝集成您宿主机已有的配置文件与模型：

```bash
# 使用 Makefile 运行
make docker-run DOCKER_IMAGE=hotplex-ai
# 或手动运行
docker run -d \
  --name hotplex-ai \
  -p 8080:8080 \
  -v $HOME/.hotplex:/.hotplex \
  -v $HOME/.claude/settings.json:/home/hotplex/.claude/settings.json:ro \
  -v $HOME/.claude/projects:/home/hotplex/.claude/projects:rw \
  -v $HOME/projects:/home/hotplex/projects:rw \
  hotplex-ai:latest
```

**目录映射说明**：
| 宿主机路径                    | 容器内路径                            | 模式 | 说明                 |
| ----------------------------- | ------------------------------------- | ---- | -------------------- |
| `$HOME/.claude/settings.json` | `/home/hotplex/.claude/settings.json` | 只读 | Claude Code 配置文件 |
| `$HOME/.claude/projects`      | `/home/hotplex/.claude/projects`      | 读写 | 会话历史记录         |
| `$HOME/.hotplex`              | `/.hotplex`                           | 读写 | HotPlex 服务配置     |
| `$HOME/projects`              | `/home/hotplex/projects`              | 读写 | 项目工作目录         |

### 3. 多平台构建 (amd64 + arm64)

```bash
make docker-buildx
```

## Docker Compose 配置

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

## 配置管理

### 目录结构

```
~/.hotplex/
├── .env                    # 敏感配置 (token、secret)
└── configs/                # 平台行为配置
    ├── slack.yaml
    ├── telegram.yaml
    └── ...
```

### 配置目录优先级

1. **HOTPLEX_CHATAPPS_CONFIG_DIR** - 环境变量指定（最高优先级，向后兼容）
2. **~/.hotplex/configs** - 用户配置目录
3. **./chatapps/configs** - 代码默认配置（Docker 镜像内）

### Docker 部署示例

```bash
# 方式 1: 挂载用户配置目录
docker run -v ~/.hotplex:/root/.hotplex hotplex:latest

# 方式 2: 指定配置目录
docker run -e HOTPLEX_CHATAPPS_CONFIG_DIR=/app/configs \
           -v ./configs:/app/configs \
           hotplex:latest
```

### Docker Compose 示例

```yaml
version: '3.8'
services:
  hotplex:
    image: hotplex:latest
    ports:
      - "8080:8080"
    volumes:
      - ./configs:/app/configs          # 平台配置
      - ./secrets/.env:/root/.env       # 敏感配置
    environment:
      - HOTPLEX_CHATAPPS_CONFIG_DIR=/app/configs
    restart: unless-stopped
```
