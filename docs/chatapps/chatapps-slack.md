# Slack Adapter: 用户与开发者手册

HotPlex Slack 适配器允许用户通过 Slack 与 AI Agent 进行自然语言交互。本手册涵盖本地开发配置、生产环境部署、以及功能特性说明。

---

## 1. 快速开始 (Quick Start)

### 1.1 本地开发 (MacBook - 无需公网地址)

Slack **Socket Mode** 支持在防火墙内运行，无需 ngrok 或公网地址。

```bash
# 1. 配置环境变量 (.env)
CHATAPPS_ENABLED=true
SLACK_BOT_TOKEN=xoxb-your-bot-token
SLACK_APP_TOKEN=xapp-your-app-token
SLACK_MODE=socket
SLACK_SERVER_ADDR=:8080

# 2. 启动 HotPlex
go run cmd/hotplexd/main.go

# 3. 在 Slack 中 @ 你的机器人开始对话
```

### 1.2 生产环境 (需要公网地址)

```bash
# 1. 配置环境变量 (.env)
CHATAPPS_ENABLED=true
SLACK_BOT_TOKEN=xoxb-your-bot-token
SLACK_SIGNING_SECRET=your-signing-secret
SLACK_MODE=http

# 2. 配置 ngrok (如果需要测试)
ngrok http 8080

# 3. 将 ngrok URL 配置到 Slack App 的 Event Subscriptions
```

---

## 2. Slack App 创建指南

### 2.1 创建 App

1. 访问 [https://api.slack.com/apps](https://api.slack.com/apps)
2. 点击 **Create New App** → **From scratch**
3. 输入 App 名称，选择目标 Workspace
4. 点击 **Create App**

### 2.2 Socket Mode 配置 (本地开发)

1. 进入 App 左侧菜单 → **Socket Mode**
2. 点击 **Enable Socket Mode**
3. 生成并保存 **App-Level Token** (`xapp-...`)
4. 在 **Event Subscriptions** 中订阅:
   - `app_mention` - @机器人时触发
   - `message.channels` - 频道消息
   - `message.groups` - 私群消息

### 2.3 HTTP Mode 配置 (生产环境)

1. 进入 App 左侧菜单 → **Event Subscriptions**
2. 点击 **Enable Events**
3. 输入公网可访问的 Request URL (如 `https://your-domain.com/webhook/slack/events`)
4. 订阅相同的事件
5. 进入 **Install App** → **Install to Workspace**
6. 保存 **Bot User OAuth Token** (`xoxb-...`)
7. 在 **Basic Information** → **App Credentials** 中获取 **Signing Secret**

---

## 3. 配置详解

### 3.1 环境变量 (.env)

| 变量名                 | 必填 | 说明                          | 获取位置                                |
| ---------------------- | ---- | ----------------------------- | --------------------------------------- |
| `CHATAPPS_ENABLED`     | ✅    | 启用 ChatApps                 | -                                       |
| `SLACK_MODE`           | ✅    | 连接模式 (`socket` 或 `http`) | -                                       |
| `SLACK_BOT_TOKEN`      | ✅    | Bot User OAuth Token          | **OAuth & Permissions** (xoxb-)         |
| `SLACK_APP_TOKEN`      | ⚠️    | App-Level Token (Socket Mode) | **Socket Mode** (xapp-)                 |
| `SLACK_SIGNING_SECRET` | ⚠️    | 签名密钥 (HTTP Mode)          | **Basic Information** → App Credentials |
| `SLACK_SERVER_ADDR`    | -    | 服务地址 (默认 :8080)         | -                                       |

### 3.2 YAML 配置 (chatapps/configs/slack.yaml)

```yaml
platform: slack
provider:
  type: claude-code
  default_model: sonnet
  default_permission_mode: bypass-permissions

mode: ${SLACK_MODE:-http}
server_addr: ${SLACK_SERVER_ADDR:-:8080}

system_prompt: |
  你是一个 AI 助手，运行在 Slack 中。
  始终在线程中回复用户。

features:
  chunking:
    enabled: true      # 自动分片 >4000 字符
    max_chars: 4000
  threading:
    enabled: true      # 线程支持
  rate_limit:
    enabled: true      # 指数退避重试
    max_attempts: 3
  markdown:
    enabled: true      # Markdown 转 mrkdwn
```

---

## 4. 功能特性

### 4.1 Socket Mode (WebSocket)

- **低延迟**: 实时 WebSocket 连接，无需轮询。
- **无需公网**: 防火墙内可直接运行。
- **自动重连**: 断线自动重连（指数退避）。

### 4.2 消息分片

Slack 单条消息限制 **4000 字符**。启用分片后:
- 自动分割超长消息。
- 添加 `[1/N]` 编号前缀。
- 保持同一线程上下文。

### 4.3 线程支持

- 自动解析 `thread_ts`。
- 始终在线程中回复用户，保持频道整洁。
- 支持 `reply_broadcast`（视配置而定）。

### 4.4 Rate Limit 处理

- 遇到 429 错误自动重试。
- 指数退避: 500ms → 1s → 2s → 4s。
- 最大 3 次重试保障。

### 4.5 Markdown 转换

自动转换 Markdown 到 Slack mrkdwn 格式:

| Markdown      | mrkdwn       |
| ------------- | ------------ |
| `**bold**`    | `*bold*`     |
| `*italic*`    | `_italic_`   |
| `[link](url)` | `<url        | text>` |
| `` `code` ``  | `` `code` `` |

---

## 5. 架构说明

### 5.1 消息流程

```
用户 @机器人
    ↓
Slack Events API / WebSocket
    ↓
handleEvent / handleSocketModeEvent
    ↓
解析 thread_ts, channel_id
    ↓
GetOrCreateSession(channel:user)
    ↓
Webhook → Engine.Execute()
    ↓
AI Response → SendToChannel()
    ↓
Slack API / WebSocket
    ↓
用户收到回复
```

### 5.2 文件结构

```
chatapps/slack/
├── adapter.go      # 核心适配器
├── config.go       # 配置定义
├── socket_mode.go  # WebSocket 连接与 ACK 逻辑
├── chunker.go      # 消息分片逻辑
├── retry.go        # 重试策略
├── sender.go       # 发送管道与格式转换
└── config.yaml     # 默认参数配置
```

### 5.3 Session 管理

- **Session Key**: `{channelId}:{userId}`。
- 隔离性：每个用户在每个频道有独立的 AI 会话。
- 生命周期：30 分钟无活动自动清理。

### 5.4 API 端点

| 类别          | 详情                         | 备注                   |
| ------------- | ---------------------------- | ---------------------- |
| 发送          | `POST /api/chat.postMessage` | 使用 Bot Token (xoxb-) |
| 接收 (HTTP)   | `/webhook/slack/events`      | 需 SigningSecret 验证  |
| 接收 (Socket) | `wss://wss.slack.com/ws`     | 需 App Token (xapp-)   |

---

## 6. 安全机制

### 6.1 签名验证 (HTTP Mode)

基于 Slack 官方推荐的签名算法验证请求来源：
- 使用 HMAC-SHA256 算法。
- 验证 `X-Slack-Request-Timestamp`（过期时间 5 分钟）。
- 防止重放攻击和请求篡改。

### 6.2 路径安全检查 (Path Protection)

HotPlex 强化了文件操作的安全性：
- **路径拦截**：禁止访问 `/etc`, `/var`, `/usr`, `/root`, `/proc`, `/sys` 等敏感目录。
- **路径清洗**：通过 `filepath.Clean` 处理 `..` 遍历攻击。
- **主目录扩展**：支持 `~` 自动扩展为当前运行用户的主目录。

### 6.3 Socket Mode ACK 可靠性

Socket Mode 要求必须在 3 秒内确认收到消息：
- **重试机制**：如果 ACK 发送失败，自动进行指数退避重试（1s, 2s, 4s）。
- **Envelope ID**：精确匹配每一个消息信封。

### 6.4 会话安全

- **逻辑隔离**：通过 `{channelId}:{userId}` 确保不同用户间的上下文不泄露。
- **自动清理**：空闲会话自动扫描并销毁，释放系统资源。

---

## 7. 系统提示词配置

系统提示词定义在 `chatapps/configs/slack.yaml` 中，决定了 AI 的人格与行为边界。

### 7.1 默认配置说明

```yaml
system_prompt: |
  ## Identity
  You are an AI assistant powered by **HotPlex**, operating within the Slack workspace.
  
  ## Platform Environment
  - **Platform**: Slack (Enterprise Workflow)
  - **Etiquette**: ALWAYS respond in threads.
  
  ## Slack-Specific Rules (mrkdwn)
  - **Formatting**: Use `*bold*` for bold, `_italic_` for italic.
  - **Code**: Use ` ``` ` (triple backticks) for code blocks.
  
  ## Guidelines
  - Be efficient and use bulleted lists.
  - Use emoji reactions (e.g., :thinking_face:) for processing status.
```

### 7.2 注入流程

```
配置文件 (slack.yaml) → ConfigLoader → EngineMessageHandler → Engine.Execute() → AI Provider (Claude/OpenCode)
```

---

## 8. 常见问题排查 (Troubleshooting)

### Q1: 收不到消息回调
- **Socket Mode**: 检查 `SLACK_APP_TOKEN` 是否正确（必须以 `xapp-` 开头）。
- **HTTP Mode**: 检查公网 URL（如 ngrok）是否已填入 Slack 的 Event Subscriptions 且状态为 "Verified"。
- **基本项**: 确认已订阅 `app_mention` 和 `message.channels` 事件，且应用已安装到 Workspace。

### Q2: 机器人显示“无法在此发送消息”
- **解决方案**: 在 Slack App 管理后台的 **App Home** 中，开启 **Messages Tab**，并勾选 `Allow users to send Slash commands and messages from the messages tab`。

### Q3: 端口冲突 (Address already in use)
- **现象**: 启动时报错 `listen tcp :8080: bind: address already in use`。
- **解决**: 使用 `lsof -i :8080` 找到对应的 PID，然后执行 `kill -9 <PID>`。

### Q4: WebSocket Bad Handshake (403)
- **解决**: 确认 `SLACK_APP_TOKEN` 是否拥有 `connections:write` 权限，并确认该 Token 确实属于当前配置的应用。

---

## 9. 开发参考

### 9.1 调试日志

```bash
# 查看详细调试信息
LOG_LEVEL=debug make run
```

### 9.2 发送测试消息

```go
// 在 Go 代码中直接调用
adapter.SendToChannel(ctx, "CHANNEL_ID", "Test Message", "")
```

---

## 10. 相关资源

- [Slack API 官方文档](https://api.slack.com/)
- [Socket Mode 规格说明](https://api.slack.com/apis/socket-mode)
- [HotPlex 架构概览](./chatapps-guide.md)

---

## 更新日志

### v0.10.0 (2026-02-25)
- ✅ 新增：路径安全检查 (遍历攻击防护)。
- ✅ 新增：Socket Mode ACK 超时重试机制。
- ✅ 新增：系统提示词配置详细说明。
- ✅ 改进：修复了 Socket Mode 连接时可能产生的 403 Handshake 错误（支持动态发现 WebSocket URL）。
- ✅ 格式：优化了文档章节号顺序。

### v0.9.0 (2026-02-24)
- ✅ 新增：work_dir 配置支持。
- ✅ 新增：~ 路径自动扩展。

### v0.8.0 (2026-02-23)
- ✅ 初始版本发布，支持 Socket & HTTP 模式。
