# ChatApps 接入层：架构协议与集成规范

HotPlex ChatApps 接入层是系统与外部通讯生态集成的核心协议层。它将 HotPlex 引擎的能力抽象为 **ChatApps-as-a-Service**，通过标准化的适配器模式实现不同平台（Slack, Telegram, 钉钉等）的无缝接入。

---

## 1. 架构概览 (System Architecture)

### 1.1 设计哲学
- **抽象解耦**：抽象统一的 `ChatAdapter` 接口，屏蔽各通讯平台 API 的巨大差异。
- **状态隔离**：基于 `Platform-User-Session` 的三维隔离机制，确保多用户环境下 Agent 状态的绝对安全性。
- **流式响应**：深度适配 Agent 的流式输出，支持亚秒级的实时反馈。

### 1.2 系统拓扑
![ChatApps Architecture](../images/chatapps-architecture.svg)

### 1.3 关键组件定义

| 组件                 | 技术定位     | 核心职责                                                             |
| :------------------- | :----------- | :------------------------------------------------------------------- |
| **AdapterManager**   | 控制面中心   | 管理所有活跃适配器的生命周期，分发上行消息至引擎。                   |
| **ChatAdapter**      | 协议转换层   | 负责特定平台的协议封装（如 WebSocket Gateway vs Webhook）。          |
| **EngineHandler**    | 业务逻辑桥接 | 订阅 Engine 事件流，将其转化为平台特定的富文本（Blocks/Cards）。     |
| **Session Registry** | 状态管理器   | 维护外部平台 `UserID` 与内部 `SessionID` 及 `WorkDir` 的持久化映射。 |

---

## 2. 核心协议契约 (Protocol Contracts)

### 2.1 适配器接口规范 `ChatAdapter`
所有集成平台必须实现以下 Golang 物理接口：

```go
type ChatAdapter interface {
    // Identity & Lifecycle
    Platform() string           // 返回平台全局唯一标识
    Start(ctx context.Context) error
    Stop() error
    
    // Message Exchange
    // SendMessage: 异步下行，支持更新现有消息（用于流式 Thinking）
    SendMessage(ctx context.Context, sessionID string, msg *ChatMessage) error
    
    // HandleMessage: 同步上行回调，适配器在此完成 Payload 预处理
    HandleMessage(ctx context.Context, msg *ChatMessage) error
}
```

### 2.2 统一消息模型 `ChatMessage`
```go
type ChatMessage struct {
    Platform    string            // 平台标识
    SessionID   string           // 系统生成的会话唯一 ID
    UserID      string           // 平台侧原生 UserID
    Content     string           // 标准化 Markdown 文本
    MessageID   string           // 平台消息回执 ID (用于 Reply-to 链)
    Metadata    map[string]any   // 平台私有扩展字段
    RichContent *RichContent     // 多模态 UI 定义 (Blocks, Cards, Buttons)
}
```

---

## 3. 消息生命周期流转 (Data Flow)

### 3.1 预处理与分发 (Ingress)
1. **Payload 归一化**：适配器捕获原始 HTTP Webhook 或 Socket 事件。
2. **身份解析**：从 Payload 提取原生 UID，并向 `Session Registry` 请求关联的 `SessionContext`。
3. **指令投影**：将文本消息映射为 `Engine.Execute` 调用。

### 3.2 响应流编排 (Egress)
1. **事件订阅**：`EngineHandler` 监听 `internal/engine` 输出。
2. **差异化渲染**：
    - **Slack**: 映射为 `Section` 和 `Context` Blocks。
    - **Telegram**: 转换为 `MarkdownV2` 并处理转义字符。
    - **钉钉**: 构建 `ActionCard` JSON。
3. **节流更新 (Throttling)**：对于 SSE 流式输出，适配器层执行 500ms-1s 的 UI 节流，避免触碰平台 Rate Limit。

---

## 4. 平台集成矩阵 (Integration Matrix)

| 平台         | 联通协议                  | 交互分级             | 架构优势                                             |
| :----------- | :------------------------ | :------------------- | :--------------------------------------------------- |
| **Slack**    | **Socket Mode**           | L3 (Block Kit)       | **首选方案**：免外网穿透，支持复杂交互与线程自动化。 |
| **Telegram** | Bot API (Polling/Webhook) | L2 (Inline Keyboard) | **响应最快**：API 限制最少，适合极简个人助手。       |
| **钉钉**     | Webhook / Callback        | L2 (ActionCard)      | **合规首选**：企业内隔离，支持长文本智能分片逻辑。   |
| **Discord**  | WebSocket Gateway         | L3 (Embeds)          | **社区化**：支持高性能多节点 Sharding 分片方案。     |

---

## 5. 安全架构与多租户隔离 (Security & Isolation)

### 5.1 会话亲和性
系统通过以下公式生成全局会话 ID，确保跨平台唯一：
`SHA256(Platform + AccountID + ChannelID)`

### 5.2 进程级沙箱
当 `AdapterManager` 触发任务时：
1. **PGID 绑定**：为该 User 创建独立的进程组 ID，强制资源限制（Cgroups）。
2. **FS Chroot**：文件系统访问限制在由 `Session Registry` 分配的专用 `work_dir`。
3. **流量审计**：所有下行至适配器的消息均经过 `SafetyManager` 的敏感词与注入检测。

---

## 6. 事件类型映射 (Event Types)

### 6.1 支持的事件类型

HotPlex Engine 定义了 21 种事件类型，全部已在 Slack 平台实现：

| 事件类型 | 说明 | Block 类型 | 状态 |
|---------|------|-----------|------|
| `thinking` | AI 推理中 | context | ✅ |
| `answer` | AI 文本输出 | section | ✅ |
| `tool_use` | 工具调用开始 | section | ✅ |
| `tool_result` | 工具执行结果 | section | ✅ |
| `error` | 错误发生 | section | ✅ |
| `result` | Turn 完成 | section+context | ✅ |
| `plan_mode` | 计划生成中 | context | ✅ |
| `exit_plan_mode` | 请求批准计划 | header+actions | ✅ |
| `ask_user_question` | 询问用户问题 | section+actions | ✅ |
| `permission_request` | 权限请求 | header+actions | ✅ |
| `danger_block` | 危险操作拦截 | section+actions | ✅ |
| `command_progress` | 命令执行进度 | section+context+actions | ✅ |
| `command_complete` | 命令执行完成 | section+context | ✅ |
| `session_start` | 会话启动 | section+context | ✅ |
| `engine_starting` | 引擎启动中 | context | ✅ |
| `user_message_received` | 消息已收到 | context | ✅ |
| `system` | 系统级消息 | context | ✅ |
| `user` | 用户消息反射 | section+context | ✅ |
| `step_start` | 步骤开始 | section+context | ✅ |
| `step_finish` | 步骤完成 | section+context | ✅ |
| `raw` | 原始输出 | section | ✅ |

### 6.2 数据流架构

```
Engine Event (provider/event.go)
    │
    ▼
StreamCallback.Handle() (engine_handler.go)
    │
    ▼
handleXxx() → base.ChatMessage{Type: MessageTypeXxx}
    │
    ▼
AdapterManager.SendMessage()
    │
    ▼
Adapter.defaultSender() (adapter.go)
    │
    ▼
MessageBuilder.Build(msg) (builder.go)
    │
    ▼
BuildXxxMessage() → []slack.Block
    │
    ▼
slack.Client.PostMessageContext()
    │
    ▼
Slack Channel
```

---

## 7. 相关文档 (Reference)
- [Slack 架构深度解析](./chatapps-slack.md)
- [Slack UX 规范](./engine-events-slack-ux-spec.md)
