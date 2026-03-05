*Read this in other languages: [English](docker-deployment.md), [简体中文](docker-deployment_zh.md).*

# Docker 部署指南

![Docker Deployment Architecture](../images/docker-deployment-arch.png)

## 快速入门

### 1. 构建镜像

```bash
# 构建 All-in-One 镜像（包含 Claude Code CLI）
make docker-build
```

**💡 提示：自定义您的开发环境**
本项目提供的 `Dockerfile` 是一个基础模板。由于 AI 编码工具在执行具体任务（例如打包前端、运行 Python 脚本或执行特定的 CLI 命令）时依赖相应的系统环境，我们强烈建议您在构建镜像前，**打开 `Dockerfile` 并加入您自己技术栈所需的依赖库或语言环境**（例如 `Node.js`, `Python`, `Rust` 或特定的构建工具）。然后再执行上方的构建命令。

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

## 网络与代理配置 (全平台支持：macOS / Windows / Linux)

由于 Docker 的网络隔离机制，在容器内访问宿主机（您的物理机）的网络代理需要特定配置。

我们在项目提供的 `docker-compose.yml` 中，已经通过加入 `extra_hosts: - "host.docker.internal:host-gateway"` 配置打通了底层通道。这一机制使得原本只有 macOS / Windows (Docker Desktop) 支持的 `host.docker.internal` 魔法域名，在 **Linux 环境下也能完美兼容生效。**

请核对您的网络需求类型，并在您的环境配置或 `docker-compose.yml` 中做出对应设置：

### 第一类用户：标准网络用户（无代理需求）
**您不需要翻墙，只需访问国内公共网络（如百度）或局域网内网接口。**
- **操作**：什么都不用做，直接启动容器即可，默认网桥会自动提供出网能力。
- **排错建议**：如果您在无代理状态下依然遇到网络奇慢或请求国内接口超时失败，大概率是默认 DNS 解析异常。您可以手动为容器指定国内优质 DNS：
  ```yaml
  dns:
    - 223.5.5.5
    - 114.114.114.114
  ```

### 第二类用户：全局代理用户（装有 Clash/V2Ray 等梯子）
**您的宿主机运行了全局代理软件，希望容器也能通过该代理访问类似 Google、GitHub 等外部资源。**
- **关键操作 1**：必须去宿主机的代理软件设置中开启 **“允许局域网连接 (Allow LAN)”**，否则宿主机会因安全策略拒绝容器的访问请求。
- **操作 2**：注入标准系统代理变量，通过魔法域名跳出沙盒：
  ```yaml
  environment:
    - HTTP_PROXY=http://host.docker.internal:<您的代理端口>
    - HTTPS_PROXY=http://host.docker.internal:<您的代理端口>
  ```

### 第三类用户：高阶用户（使用独立 LLM 专用代理通道）
**您的网络环境比较复杂，除了需要全局代理用于上网插件外，还需要为大模型（LLM）API 指派非常专用的定制节点路由或端口。**
- **操作**：HotPlex 支持精细化的代理剥离策略。您可以同时并存两者，互不干扰：
  ```yaml
  environment:
    # 1. LLM 专属定制通道网关 (例如仅加速 Claude 接口)
    - ANTHROPIC_BASE_URL=http://host.docker.internal:15721
    # 2. 覆盖其它网络抓取插件的通用系统代理
    - HTTP_PROXY=http://host.docker.internal:7897
    - HTTPS_PROXY=http://host.docker.internal:7897
    # 3. 拦截本地回环，防止死因循环的报错
    - NO_PROXY=localhost,127.0.0.1,host.docker.internal
  ```

### 验证网络
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
