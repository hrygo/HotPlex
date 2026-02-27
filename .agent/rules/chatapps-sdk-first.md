# ChatApps 开发规范 (SDK First)

> **核心原则**：优先使用平台 SDK，所有平台交互必须通过官方 SDK，禁止重复造轮子

---

## 1. SDK 使用强制规范

### 1.1 消息构建

| 场景 | 规范 |
|------|------|
| **Block Kit** | 使用 `slack.NewSectionBlock()`, `slack.NewHeaderBlock()` 等 |
| **按钮** | 使用 `slack.NewButtonBlockElement()` |
| **文本** | 使用 `slack.NewTextBlockObject()` |
| **输入** | 使用 `slack.NewPlainInputElement()`, `slack.NewStaticSelectElement()` |

**禁止**：
```go
// ❌ 禁止
block := map[string]any{
    "type": "section",
    "text": map[string]any{"type": "mrkdwn", "text": "hello"},
}

// ✅ 必须使用 SDK
block := slack.NewSectionBlock(
    slack.NewTextBlockObject(slack.MarkdownType, "hello", false, false),
    nil, nil,
)
```

### 1.2 消息发送

| 场景 | 规范 |
|------|------|
| **发送消息** | 使用 `client.PostMessage()` |
| **更新消息** | 使用 `client.UpdateMessage()` |
| **删除消息** | 使用 `client.DeleteMessage()` |
| **添加反应** | 使用 `client.AddReaction()` |
| **发送 Ephemeral** | 使用 `client.PostEphemeral()` |

### 1.3 Socket Mode

| 场景 | 规范 |
|------|------|
| **连接** | 使用 `socketmode.New(client)` |
| **事件处理** | 从 `socketmode.Event` 提取 |
| **ACK** | 使用 `client.Ack()` |

---

## 2. 禁止重复的代码模式

### 2.1 Rate Limiting

| 平台 | SDK 支持 |
|------|----------|
| Slack | `golang.org/x/time/rate` 或 SDK 内置重试 |
| 其他 | 使用通用 `golang.org/x/time/rate` |

**禁止**：为每个平台手写限流器

### 2.2 签名验证

| 平台 | SDK 支持 |
|------|----------|
| Slack | SDK 内置 HMAC 验证 |
| Discord | SDK 内置 Ed25519 验证 |
| DingTalk | SDK 内置验证 |

**禁止**：重复实现签名验证逻辑

### 2.3 消息分块

Slack SDK 限制 4000 字符。实现分块逻辑时：
- 使用 SDK 的 `chat.postMessage` 参数
- 优先使用通用分块算法，不依赖平台 API

---

## 3. 适配器职责边界

```
┌─────────────────────────────────────────────────────┐
│                    Platform SDK                      │
│         (slack-go, telegram-bot-api, etc.)           │
└─────────────────────────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────┐
│                   Adapter Layer                      │
│  • 事件接收与认证                                    │
│  • 平台消息 → ChatMessage 转换                       │
│  • SDK 调用                                          │
└─────────────────────────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────┐
│                   Base Layer                         │
│  • 通用类型 (ChatMessage, Session)                   │
│  • 会话管理                                          │
│  • 消息处理器链                                      │
└─────────────────────────────────────────────────────┘
```

**Adapter 职责**：
- 只做平台特定的转换和 SDK 调用
- 不实现通用逻辑（限流、分块、聚合）
- 所有 SDK 功能通过 wrapper 调用

---

## 4. 代码审查检查点

提交代码前自检：

- [ ] 消息构建是否使用 SDK 类型？
- [ ] 是否有手写的签名验证？（SDK 已有）
- [ ] 是否有重复的 RateLimiter？（通用库即可）
- [ ] 是否有重复的 Block 构建？（SDK 提供）

---

## 5. 已有正面示例

### ✅ Slack Adapter 消息构建

```go
// 使用 SDK 类型
header := slack.NewHeaderBlock(
    slack.NewTextBlockObject(slack.PlainTextType, "Title", false, false),
)
section := slack.NewSectionBlock(
    slack.NewTextBlockObject(slack.MarkdownType, "*Hello*", false, false),
    nil, nil,
)
```

### ✅ Socket Mode 连接

```go
// 使用 SDK 内置
client := socketmode.New(slackClient, socketmode.OptionDebug(true))
go client.RunContext(ctx)
for evt := range client.Events {
    // 处理事件
}
```

---

## 6. 违规示例

| 违规类型 | ❌ 错误做法 | ✅ 正确做法 |
|---------|-----------|------------|
| Block 构建 | `map[string]any{...}` | `slack.NewSectionBlock(...)` |
| 签名验证 | 手写 HMAC 验证 | SDK 内置验证 |
| Socket Mode | 手写 WebSocket | `socketmode.New()` |
| Rate Limit | 每平台重复实现 | `golang.org/x/time/rate` 统一 |
| 按钮构建 | `map[string]any` | `slack.NewButtonBlockElement()` |

---

## 7. 依赖原则

```
项目依赖 ──→ 平台 SDK ──→ 标准库
                │
                ▼
         golang.org/x/*
              (通用库)
```

- **优先**：平台 SDK（slack-go, telegram-bot-api）
- **次优**：golang.org/x/*（限流、日志等通用库）
- **禁止**：手写平台 SDK 已提供的功能
