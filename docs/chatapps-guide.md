# ChatApps 接入层：架构设计与用户指南

HotPlex ChatApps 接入层允许用户通过各种聊天应用与 AI Agent 进行自然语言交互。参考 OpenClaw 的设计理念，将 HotPlex 打造成 **ChatApps-as-a-Service** 平台，支持多平台接入与自托管。

---

## 1. 架构设计 (Architecture)

### 1.1 设计目标
将 HotPlex 引擎的能力通过聊天机器人（Bots）延伸到用户常用的通讯工具中，实现“对话即入口”的 AI 助手体验。

### 1.2 整体架构图
![ChatApps Architecture](./images/chatapps-architecture.svg)

### 1.3 核心组件说明

| 组件                | 职责                                                                                              |
| ------------------- | ------------------------------------------------------------------------------------------------- |
| **ChatAdapter**     | 适配器接口，负责特定平台的协议转换（Webhook/Polling）、消息解析与格式化。                         |
| **AdapterManager**  | 适配器注册与生命周期管理，负责将不同来源的消息分发给 Engine，并将 Engine 的事件广播回特定适配器。 |
| **ChatMessage**     | 内部统一的消息交换格式，包含平台 ID、用户 ID、会话 ID 和内容。                                    |
| **Session Manager** | 处理聊天平台会话与 HotPlex Engine 会话的映射（Mapping）。                                         |

---

## 2. 核心接口定义

### 2.1 ChatAdapter 接口
```go
type ChatAdapter interface {
    // Platform 返回平台标识 (如 "dingtalk", "telegram")
    Platform() string
    
    // Start 启动适配器
    Start(ctx context.Context) error
    
    // Stop 停止适配器
    Stop() error
    
    // SendMessage 发送消息到聊天平台 (供 Engine 调用)
    SendMessage(ctx context.Context, sessionID string, msg *ChatMessage) error
    
    // HandleMessage 处理从聊天平台接收的消息
    HandleMessage(ctx context.Context, msg *ChatMessage) error
}
```

### 2.2 ChatMessage 结构 (Rich Content 支持)
```go
type ChatMessage struct {
    Platform    string            // 平台标识
    SessionID   string           // 内部会话 ID
    UserID      string           // 平台唯一用户 ID
    Content     string           // 消息文本内容
    MessageID   string           // 平台原始消息 ID (用于回复)
    Timestamp   time.Time        // 消息产生时间
    Metadata    map[string]any   // 平台特定扩展数据
    RichContent *RichContent     // 富文本支持 (按钮、卡片、附件)
}

// RichContent 用于高级交互 (Telegram 按钮, Slack Blocks 等)
type RichContent struct {
    ParseMode      ParseMode             // Markdown/HTML
    InlineKeyboard *InlineKeyboardMarkup // Telegram 内置键盘
    Blocks         []SlackBlock          // Slack Block Kit
    Embeds         []DiscordEmbed        // Discord Embeds
    Attachments    []Attachment         // 通用媒体附件
}
```

---

## 3. 消息交互全流程

### 3.1 上行触发 (User -> AI)
1. **用户发送**: 用户在钉钉/Telegram 中向机器人发送消息。
2. **回调接收**: `ChatAdapter` 接收回调 (Webhook) 或通过 Polling 获取原始消息。
3. **格式转换**: 适配器将原始 payload 转换为 `ChatMessage` 对象。
4. **引擎执行**: 调用 `Engine.Execute()`，附带用户 ID 映射的会话。

### 3.2 下行响应 (AI -> User)
1. **事件流产生**: AI CLI (Claude Code/OpenCode) 产生 SSE 事件流 (Thinking, Tool use, Answer)。
2. **事件转换**: 引擎将事件转发给 `AdapterManager`。
3. **适配器下发**: 对应平台的 `ChatAdapter` 调用平台 API (如 `sendMsg`) 将内容回传给用户。由于 AI 响应常为流式且包含 Markdown，适配器需处理**消息分片**或**内容更新**逻辑。

---

## 4. 平台选型指南 (Comparison & Selection)

不同的聊天平台在功能特性、连通性以及用户受众上各有千秋。下表帮助开发者根据项目需求选择最合适的接入方式：

|     平台     | 最适场景                 | 连通方式                 | 格式支持              | 核心特色                                                                       |
| :----------: | ------------------------ | ------------------------ | --------------------- | ------------------------------------------------------------------------------ |
| **Telegram** | 个人助手、开源社区       | Webhook / Polling        | MarkdownV2, HTML      | **最强 API**: 自由度极高，支持内联查询、自定义键盘，完全免费且无审批。         |
|   **钉钉**   | 中国企业办公、自动化运维 | Webhook (回调)           | Markdown (有限), 卡片 | **合规性**: 国内企业标配，支持 ActionCard 交互，深度集成 OA 审批流。           |
|  **Slack**   | 国际企业协作、DevOps     | **Socket Mode**, Webhook | mrkdwn, Block Kit     | **免公网**: Socket Mode 允许在防火墙内运行。Block Kit 支持极其复杂的 UI 交互。 |
| **Discord**  | 开发者社区、技术支持     | WebSocket Gateway        | Embeds, Markdown      | **高并发**: 支持分片 (Sharding)，Embeds 展现力极强，适合大规模社区。           |
| **WhatsApp** | 极高普及度的个人通讯     | Business API / Baileys   | 文本, 按钮模板        | **触达率**: 全球最高覆盖，但 API 限制较多且 commercial 账号有费用成本。        |

### 4.1 核心维度深入对比

#### 🛠️ UI 交互能力
*   **高 (Slack/Discord)**: 支持下拉菜单、日期选择器、复杂的卡片嵌入 (Embeds/Blocks)。
*   **中 (Telegram/钉钉)**: 支持内联按钮 (Inline Buttons) 和基础 Markdown。Telegram 的内联模式 (Inline Mode) 非常适合快速指令触发。
*   **低 (WhatsApp)**: 以文字和预设按钮模板 (Buttons/List) 为众，交互路径较长。

#### 🌐 连通性与穿透
*   **内网友好**: **Slack (Socket Mode)** 可以在没有公网 IP 的开发机直接运行，无需 `ngrok`。
*   **公网必需**: **Telegram/钉钉/Discord** 默认需要公网 Webhook URL (Discord 也可使用 WebSocket 长连但通常用于接收事件)。

#### ⏳ 频率限制 (Rate Limits)
*   **Telegram**: 个人 1 msg/sec，群组 20 msgs/min，适合中速交互。
*   **Slack**: 依据 Tier 有级阶限制，通常 1 msg/sec/channel。
*   **Discord**: 全球 50 req/sec，处理突发流量能力最强。
*   **钉钉**: 机器人每分钟限 20 条消息推送。

---

## 5. 平台集成指南 (Deep Dive)

### 5.1 Telegram (推荐)
Telegram 是接入最简单且功能最丰富的平台。支持流式 Markdown、富文本、内联按钮等。

#### 1. 配置步骤
1.  **获取 Token**: 在 Telegram 中联系 [@BotFather](https://t.me/botfather) 创建机器人并获取 `Bot Token`。
2.  **设置 Webhook**:
    *   需要一个公网 HTTPS 地址（如 `https://api.yourdomain.com/webhook`）。
    *   **安全建议**: 配置 `SecretToken` 并在适配层验证 `X-Telegram-Bot-Api-Secret-Token` 请求头，以防止伪造请求 (参考 CVE-2026-25474 缓解方案)。
3.  **Parse Mode**: 推荐使用 `MarkdownV2` 以获得最佳的代码块高亮效果。

#### 2. 特效支持 (Rich Interaction)
```go
msg := &chatapps.ChatMessage{
    RichContent: &chatapps.RichContent{
        ParseMode: chatapps.ParseModeMarkdown,
        InlineKeyboard: &chatapps.InlineKeyboardMarkup{
            InlineKeyboard: [][]chatapps.InlineKeyboardButton{
                {{Text: "查看报告", URL: "https://hotplex.io/report"}},
            },
        },
    },
}
```

### 5.2 钉钉 (DingTalk)
#### 1. 接入模式
钉钉支持两种主要的双向通信模式：
- **Webhook 模式**: 仅用于单向通知 (HotPlex 已在 Hooks 层实现)。
- **回调模式 (机器人应用)**: 用于接收用户在对话框的 @ 消息或私聊。

#### 2. 注意事项
*   **消息长度限制**: 钉钉单条消息上限约 5000 字符。`DingTalkAdapter` 内部实现了分片机制以处理超长响应。
*   **私密性**: 企业内部应用回调需要验证 `appKey` 或进行签名校验。

---

## 6. 快速开始与本地开发

### 6.1 运行示例组件
```bash
# 进入 HotPlex 目录
go run _examples/chatapps_dingtalk/main.go
```

### 6.2 本地测试 (内网穿透)
由于聊天平台通常需要一个可验证的公网 Webhook URL，建议开发阶段使用工具：
```bash
# 1. 启动本地 HotPlex
go run main.go

# 2. 启动 ngrok / cloudflared
ngrok http 8080

# 3. 将生成的 URL 填回钉钉后台
```

---

## 7. 安全与用户隔离 (Security & Isolation)

### 7.1 会话映射逻辑
HotPlex 采用 **Platform-User-Chat** 的多维映射：
- `SessionID` 生成规则: `{Platform}-{UserID}-{ChatID}`。
- **作用**: 确保同一个机器人在不同群组、不同用户之间具备完全独立的上下文空间。

### 7.2 PGID 与 Sandbox 隔离
每一个 ChatApp 用户的交互都会触发一个独立的运行环境：
1. **隔离机制**: 利用 Linux `PGID` (Process Group ID) 限制 AI Agent 的进程级别操作。
2. **文件系统隔离**: 每个 `UserID` 绑定一个独立的 `work_dir` 目录，AI Agent 无法跨越目录读取其他用户的文件。
3. **资源限制**: 在 `AdapterManager` 中可配置单个用户的 CPU/内存配额，防止恶意任务耗尽服务器资源。

---

## 8. 环境配置与参考 (Environment Variables)

| 变量名                            | 说明               | 示例            |
| --------------------------------- | ------------------ | --------------- |
| `HOTPLEX_CHATAPPS_ADDR`           | 适配器监听端口     | `:8080`         |
| `HOTPLEX_TELEGRAM_TOKEN`          | Telegram Bot Token | `12345:ABCDE`   |
| `HOTPLEX_TELEGRAM_SECRET`         | Webhook 安全校验码 | `secure_secret` |
| `HOTPLEX_DINGTALK_APP_ID`         | 钉钉 App Key       | `dingXXXX`      |
| `HOTPLEX_DINGTALK_APP_SECRET`     | 钉钉 App Secret    | `secretXXXX`    |
| `HOTPLEX_DINGTALK_CALLBACK_TOKEN` | 回调验证 Token     | `tokenXXXX`     |

---

## 9. 常见问题与风险 (FAQ)

**Q: 如何处理 AI 的长响应？**
A: 绝大多数聊天平台对单条消息有字数限制 (如钉钉为 5000 字)。`ChatAdapter` 内部会负责对 Markdown 内容进行智能分片，或使用更新消息的操作来显示流式输出。

**Q: 是否支持多用户独立上下文？**
A: 是的。HotPlex 利用 `UserID` 作为会话隔离的 Key。每个用户在不同的聊天平台上会有独立的 `work_dir` 文件夹。

**Q: 为什么我收不到回调？**
A: 请检查：
- 端口是否已打通。
- 钉钉后台配置的域名/IP 是否可访问。
- 若启用了签名校验，确认密钥配置是否匹配。

---

## 相关资源
- [Hooks 事件系统](./hooks-architecture.md)
- [服务器 API 指南](./server/api.md)
- [SDK 快速集成](./sdk-guide.md)
