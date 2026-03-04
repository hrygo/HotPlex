*Read this in other languages: [English](docker-deployment.md), [简体中文](docker-deployment_zh.md).*

# Docker 部署指南

## 快速入门

### 1. 构建镜像

```bash
docker build -t hotplex:latest .
```

### 2. 运行容器

```bash
docker run -d \
  --name hotplex \
  -p 8080:8080 \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -e CLAUDE_API_KEY=your-key \
  hotplex:latest
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
      - PORT=8080
      - LOG_LEVEL=info
      - IDLE_TIMEOUT=30m
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

| 变量          | 默认值 | 描述                   |
| ------------- | ------ | ---------------------- |
| PORT          | 8080   | 服务端口               |
| LOG_LEVEL     | info   | 日志级别               |
| IDLE_TIMEOUT  | 30m    | 会话空闲超时时间       |
| OTEL_ENDPOINT | -      | OpenTelemetry 接口地址 |
| MAX_SESSIONS  | 1000   | 最大并发会话数         |

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

1. **CHATAPPS_CONFIG_DIR** - 环境变量指定（最高优先级，向后兼容）
2. **~/.hotplex/configs** - 用户配置目录
3. **./chatapps/configs** - 代码默认配置（Docker 镜像内）

### Docker 部署示例

```bash
# 方式 1: 挂载用户配置目录
docker run -v ~/.hotplex:/root/.hotplex hotplex:latest

# 方式 2: 指定配置目录
docker run -e CHATAPPS_CONFIG_DIR=/app/configs \
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
      - CHATAPPS_CONFIG_DIR=/app/configs
    restart: unless-stopped
```
