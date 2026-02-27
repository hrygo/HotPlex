# Slack 模块架构图

## 完整架构图（Socket Mode + HTTP Mode）

```
┌──────────────────────────────────────────────────────────────────────────────┐
│                        Slack 模块完整架构                                     │
└──────────────────────────────────────────────────────────────────────────────┘

                    ┌─────────────────┐
                    │   Slack 用户    │
                    │                 │
                    │ • @mention     │
                    │ • /reset, /dc  │
                    │ • 点击按钮      │
                    └────────┬────────┘
                             │
         ┌───────────────────┼───────────────────┐
         │                   │                   │
         ▼                   ▼                   ▼
┌─────────────────┐ ┌─────────────────┐ ┌─────────────────┐
│  Socket Mode    │ │   HTTP Mode     │ │  Interactive   │
│  (WebSocket)   │ │  (Webhook)      │ │  (Callback)    │
└────────┬────────┘ └────────┬────────┘ └────────┬────────┘
         │                   │                   │
         └───────────────────┼───────────────────┘
                             │
                             ▼
┌──────────────────────────────────────────────────────────────────────────────┐
│                        adapter.go                                           │
│  ┌─────────────────────────────────────────────────────────────────────┐    │
│  │  NewAdapter(config)                                                 │    │
│  │    ├─ config.Mode == "socket" → socketmode.New(client)            │    │
│  │    └─ config.Mode == "http"  → 注册 HTTP handlers                  │    │
│  └─────────────────────────────────────────────────────────────────────┘    │
│                                                                              │
│  ┌─────────────────────────────────────────────────────────────────────┐    │
│  │  Start(ctx)                                                        │    │
│  │    ├─ Socket Mode: startSocketMode(ctx) → client.RunContext(ctx)  │    │
│  │    └─ HTTP Mode:    adapter.Start(ctx) → HTTP Server              │    │
│  └─────────────────────────────────────────────────────────────────────┘    │
└──────────────────────────────────────────────────────────────────────────────┘
                             │
         ┌───────────────────┴───────────────────┐
         │                                       │
         ▼                                       ▼
┌─────────────────────────┐         ┌─────────────────────────┐
│  Socket Mode 事件处理   │         │   HTTP 事件处理        │
│  (socketmode.Event)     │         │   (HTTP Request)       │
├─────────────────────────┤         ├─────────────────────────┤
│ • EventsAPI            │         │ • handleEvent()         │
│ • SlashCommand         │         │ • handleSlashCommand()  │
│ • Interactive          │         │ • handleInteractive()   │
└────────────┬────────────┘         └────────────┬────────────┘
             │                                     │
             └───────────────────┬─────────────────┘
                                 │
                                 ▼
┌──────────────────────────────────────────────────────────────────────────────┐
│                        统一处理层                                           │
│  ┌────────────────┐ ┌────────────────┐ ┌────────────────────────┐           │
│  │ handleReset    │ │ handleDisconnect│ │ handlePermission      │           │
│  │ Command()     │ │ Command()      │ │ Callback()           │           │
│  └───────┬────────┘ └───────┬────────┘ └──────────┬───────────┘           │
│          │                   │                     │                       │
│          └───────────────────┼─────────────────────┘                       │
│                              │                                             │
│                              ▼                                             │
│                    ┌─────────────────────┐                                │
│                    │      Engine         │                                │
│                    │  • 执行命令         │                                │
│                    │  • 管理会话         │                                │
│                    └──────────┬──────────┘                                │
└───────────────────────────────┼───────────────────────────────────────────┘
                                │
                                ▼
┌──────────────────────────────────────────────────────────────────────────────┐
│                        消息响应层                                           │
│  ┌────────────────┐ ┌────────────────┐ ┌────────────────────────┐           │
│  │ builder.go     │ │ formatting.go  │ │ chunker.go            │           │
│  │ (构建消息)     │ │ (格式转换)     │ │ (消息分块)            │           │
│  └───────┬────────┘ └───────┬────────┘ └──────────┬───────────┘           │
│          │                   │                     │                       │
│          └───────────────────┼─────────────────────┘                       │
│                              │                                             │
│                              ▼                                             │
│                    ┌─────────────────────┐                                │
│                    │ slack.Client        │                                │
│                    │ • PostMessage()     │                                │
│                    │ • UpdateMessage()   │                                │
│                    └─────────────────────┘                                │
└──────────────────────────────────────────────────────────────────────────────┘
```

---

## 两种运行模式

### Socket Mode (推荐)

- **连接方式**: WebSocket 长连接
- **优点**:
  - 不需要公网地址
  - 不需要验证签名
  - 适合本地调试和企业内网
- **配置**:
  ```yaml
  mode: socket
  app_token: xapp-xxxx-xxxx-xxxx-xxxx
  bot_token: xoxb-xxxx-xxxx-xxxx-xxxx
  ```

### HTTP Mode (传统)

- **连接方式**: HTTP Webhook
- **优点**:
  - 稳定可靠
  - 适合生产环境
- **配置**:
  ```yaml
  mode: http
  signing_secret: xxxxx
  bot_token: xoxb-xxxx-xxxx-xxxx-xxxx
  ```

---

## 文件清单

| 文件 | 状态 | 职责 |
|------|------|------|
| `adapter.go` | ✅ 保留 | 核心适配器，支持 Socket Mode 和 HTTP Mode |
| `builder.go` | ✅ 保留 | MessageBuilder，消息构建 |
| `config.go` | ✅ 保留 | 配置管理 |
| `formatting.go` | ✅ 保留 | MrkdwnFormatter，格式转换 |
| `security.go` | ✅ 保留 | 安全验证 |
| `validator.go` | ✅ 保留 | Block Kit 验证 |
| `chunker.go` | ✅ 保留 | 消息分块 |
| `blocks.go` | ✅ 保留 | Block 工具函数 |
| `block_builder.go` | ✅ 精简 | 状态消息构建 |
| `rate_limiter.go` | ✅ 保留 | 限流器 |

---

## 交互流程

### 1. Slash Command 流程

```
Slack 用户 → /reset 或 /dc → adapter.handleSocketModeSlashCommand()
                                        ↓
                              识别命令类型
                                        ↓
                    ┌───────────────────┴───────────────────┐
                    ▼                                       ▼
         handleResetCommand()                    handleDisconnectCommand()
                    │                                       │
                    └───────────────────┬───────────────────┘
                                        ▼
                              Engine 执行命令
                    (删除会话/终止进程)
                                        │
                                        ▼
                              builder.go 构建响应
                                        │
                                        ▼
                              slack.Client 发送响应
```

### 2. Event 流程 (App Mention)

```
Slack 用户 → @mention → adapter.handleAppMentionEvent()
                                    ↓
                          生成 SessionID
                                    ↓
                          构建 ChatMessage
                                    ↓
                          adapter.SendMessage()
                                    │
                                    ▼
                              Engine 处理
                                    │
                                    ▼
                          builder.go 构建响应
                                    │
                                    ▼
                          slack.Client 更新消息
```

### 3. Interactive 流程 (按钮点击)

```
用户点击按钮 → adapter.handleSocketModeInteractive()
                                    ↓
                          解析 callback_id
                                    ↓
              ┌───────────────────┴───────────────────┐
              ▼                                       ▼
    handlePermissionCallback()              handlePlanModeCallback()
    (perm_allow/perm_deny)                  (plan_approve/modify/cancel)
              │                                       │
              └───────────────────┬───────────────────┘
                                  ▼
                          Engine 处理请求
                                  │
                                  ▼
                          builder.go 构建响应
                                  │
                                  ▼
                          slack.Client 更新消息
```

---

## 4. Engine Event → Slack Block 映射

### 4.1 事件处理链

所有 Engine 事件通过 `StreamCallback.Handle()` 分发到对应的处理方法：

| 事件类型 | 处理方法 | MessageBuilder 方法 |
|---------|---------|-------------------|
| `thinking` | `handleThinking()` | `BuildThinkingMessage()` |
| `tool_use` | `handleToolUse()` | `BuildToolUseMessage()` |
| `tool_result` | `handleToolResult()` | `BuildToolResultMessage()` |
| `answer` | `handleAnswer()` | `BuildAnswerMessage()` |
| `error` | `handleError()` | `BuildErrorMessage()` |
| `plan_mode` | `handlePlanMode()` | `BuildPlanModeMessage()` |
| `exit_plan_mode` | `handleExitPlanMode()` | `BuildExitPlanModeMessage()` |
| `ask_user_question` | `handleAskUserQuestion()` | `BuildAskUserQuestionMessage()` |
| `permission_request` | `handlePermissionRequest()` | `BuildPermissionRequestMessageFromChat()` |
| `command_progress` | `handleCommandProgress()` | `BuildCommandProgressMessage()` |
| `command_complete` | `handleCommandComplete()` | `BuildCommandCompleteMessage()` |
| `session_start` | `handleSessionStart()` | `BuildSessionStartMessage()` |
| `engine_starting` | `handleEngineStarting()` | `BuildEngineStartingMessage()` |
| `user_message_received` | `handleUserMessageReceived()` | `BuildUserMessageReceivedMessage()` |
| `system` | `handleSystem()` | `BuildSystemMessage()` |
| `user` | `handleUser()` | `BuildUserMessage()` |
| `step_start` | `handleStepStart()` | `BuildStepStartMessage()` |
| `step_finish` | `handleStepFinish()` | `BuildStepFinishMessage()` |
| `raw` | `handleRaw()` | `BuildRawMessage()` |
| `result` | `handleSessionStats()` | `BuildSessionStatsMessage()` |
| `danger_block` | `handleDangerBlock()` | `BuildDangerBlockMessage()` |

### 4.2 SDK First 规范

所有 Block Kit 构建使用 `slack-go` SDK 类型：

```go
// ✅ 正确：使用 SDK 类型
section := slack.NewSectionBlock(
    slack.NewTextBlockObject(slack.MarkdownType, content, false, false),
    nil, nil,
)
header := slack.NewHeaderBlock(
    slack.NewTextBlockObject(slack.PlainTextType, "Title", false, false),
)
btn := slack.NewButtonBlockElement("action_id", "value",
    slack.NewTextBlockObject(slack.PlainTextType, "Button", false, false))

// ❌ 禁止：使用 map[string]any
block := map[string]any{
    "type": "section",
    "text": map[string]any{"type": "mrkdwn", "text": "hello"},
}
```

---

## 5. 相关文件

| 文件 | 职责 |
|------|------|
| `chatapps/slack/adapter.go` | Slack 适配器，处理双向通信 |
| `chatapps/slack/builder.go` | MessageBuilder，事件 → Block Kit 转换 |
| `chatapps/engine_handler.go` | StreamCallback，Engine 事件处理 |
| `chatapps/base/types.go` | MessageType 枚举定义 |
| `provider/event.go` | ProviderEventType 枚举定义 |
