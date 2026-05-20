---
type: spec
tags: [project/HotPlex, messaging/feishu, messaging/slack, webchat, platform-adapter]
date: 2026-05-20
status: draft
priority: high
estimated_hours: 20
last_updated: 2026-05-21
research_sources:
  - Hermes Agent /home/hotplex/.hermes/hermes-agent (approval.py, feishu.py, slack.py, tui_gateway)
  - HotPlex /home/hotplex/.hotplex/workspace/hotplex (feishu/*, slack/*, gateway/*)
  - Lark SDK v3.5.3 ws/client.go, card/card.go, card/model.go
sdk_upgrade:
  from: v3.5.3
  to: v3.9.1
  breaking_changes: "ReceiveIdTypeChatId constant removed → use string \"chat_id\" directly"
  test_result: ALL PASS (0 failures)
---

# Feishu Interactive Card Buttons Spec

## 1. 概述

### 1.1 目标

为飞书平台添加交互式卡片按钮，让用户通过点击按钮响应 Permission / Question / Elicitation 请求，替代当前的纯文本回复模式。同时确认 Slack 和 WebChat 已有的按钮支持无需改动。

### 1.2 现状分析

| 平台 | 当前状态 | 按钮 UI | 回调机制 | 改动量 |
|------|---------|---------|---------|-------|
| **Slack** | ✅ 已实现 | Block Kit buttons | Socket Mode `InteractionCallback` | **无** |
| **WebChat** | ✅ 已实现 | 浏览器前端组件 | AEP WebSocket 双向通信 | **无** |
| **Feishu** | ❌ 展示型 | 无按钮 | 无回调 | **核心改动** |

### 1.3 根因

飞书交互式按钮不可用的根本原因是 **Lark SDK（v3.5.3 ~ v3.9.1 所有版本）的 WebSocket 客户端未实现卡片回调处理**：

```go
// ws/client.go handleDataFrame() — v3.5.3 和 v3.9.1 代码完全相同
switch MessageType(type_) {
case MessageTypeEvent:
    rsp, err = c.eventHandler.Do(ctx, pl)
case MessageTypeCard:
    return  // ← 卡片回调消息被静默丢弃！所有版本均未修复
}
```

SDK 的 `cardHandler` 字段和 `WithCardHandler` 选项已定义但被注释掉，且 **v3.5.3 → v3.9.1 的 4 个大版本升级均未解除注释**。

**关键结论**：飞书 WS 协议**支持**传输卡片回调消息（`MessageTypeCard`），SDK 已定义处理框架但从未完成实现。升级 SDK 无法解决此问题，必须自行补丁。

---

## 2. 方案设计

### 2.1 方案选择：SDK 补丁 + Handler 注册

| 方案 | 描述 | 优点 | 缺点 |
|------|------|------|------|
| **A. SDK 补丁（推荐）** | go.mod replace 指向本地补丁版 SDK | 改动最小（~10行），复用现有 WS 连接 | 维护本地 SDK fork |
| B. Webhook 混合模式 | WS 收消息 + HTTP 收卡片回调 | 不改 SDK | 需额外 HTTP 端口和公网可达 |
| C. 自定义 WS 拦截器 | 在 SDK 处理前拦截原始帧 | 不改 SDK | 绕过 SDK 抽象层，脆弱 |
| D. 等待官方 SDK 更新 | 无改动 | 最安全 | 时间不可控 |

**选定方案 A**：SDK 补丁。

理由：
1. 补丁量极小（启用 `WithCardHandler` + 路由 `MessageTypeCard`），影响面可控
2. 复用现有 WS 长连接，无需新增 HTTP 端口或公网域名
3. 可向上游提交 PR，未来官方合并后移除 replace 指令

### 2.2 架构总览

```
                        ┌─────────────────────────────────────────┐
                        │           HotPlex Gateway               │
                        │                                         │
  Feishu WS ───────────▶│  ws.go ──▶ newEventHandler()            │
  (MessageTypeCard)     │            │                             │
                        │            ├── P2MessageReceiveV1        │
                        │            ├── P2ChatAccessEvent...      │
                        │            └── [新增] cardHandler         │
                        │                 │                        │
                        │                 ▼                        │
                        │          handleCardAction()              │
                        │            │                             │
                        │            ├── 验证操作者身份              │
                        │            ├── Complete(requestID)       │
                        │            ├── SendResponse(metadata)    │
                        │            └── 返回更新卡片（原地变色）     │
                        │                 │                        │
  Slack Socket ─────────▶│  slack/interaction.go (已实现 ✅)        │
  (BlockActions)         │                                         │
                        │                                         │
  WebChat WS ───────────▶│  gateway/conn.go → 浏览器前端 (已实现 ✅) │
  (AEP events)           │                                         │
                        └─────────────────────────────────────────┘
```

---

## 3. 实现细节

### 3.1 Phase 1: Lark SDK 补丁

**文件**: `go.mod` + vendor/replace

补丁内容（基于 `github.com/larksuite/oapi-sdk-go/v3@v3.5.3/ws/client.go`）：

```go
// 1. 取消注释 WithCardHandler（原第 54-58 行）
func WithCardHandler(handler *larkcard.CardActionHandler) ClientOption {
    return func(cli *Client) {
        cli.cardHandler = handler
    }
}

// 2. 修改 handleMessage 中 MessageTypeCard 分支（原第 424-425 行）
case MessageTypeCard:
    if c.cardHandler != nil {
        rsp, err = c.cardHandler.DoHandle(ctx, &larkevent.EventReq{
            Header: frame.Headers.Map(),
            Body:   pl,
        })
    } else {
        return
    }
```

**go.mod 改动**:

```
replace github.com/larksuite/oapi-sdk-go/v3 => ./vendor/patches/oapi-sdk-go-v3
```

将补丁版 SDK 放入 `vendor/patches/oapi-sdk-go-v3/`，仅修改 `ws/client.go` 一个文件。

### 3.2 Phase 2: Feishu Adapter 注册卡片处理器

**文件**: `internal/messaging/feishu/ws.go`

```go
func (a *Adapter) newEventHandler() *dispatcher.EventDispatcher {
    return dispatcher.NewEventDispatcher("", "").
        OnP2MessageReceiveV1(a.handleMessage).
        OnP2MessageReadV1(func(_ context.Context, _ *larkim.P2MessageReadV1) error { return nil }).
        OnP2MessageReactionCreatedV1(func(_ context.Context, _ *larkim.P2MessageReactionCreatedV1) error { return nil }).
        OnP2MessageReactionDeletedV1(func(_ context.Context, _ *larkim.P2MessageReactionDeletedV1) error { return nil }).
        OnP2ChatAccessEventBotP2pChatEnteredV1(a.handleChatEntered)
}

// 新增：构建卡片回调处理器
func (a *Adapter) newCardHandler() *larkcard.CardActionHandler {
    return larkcard.NewCardActionHandler("", "", a.handleCardAction)
}

func (a *Adapter) runWebSocket(ctx context.Context) {
    // ... existing reconnect loop ...
    client := ws.NewClient(a.appID, a.appSecret,
        ws.WithEventHandler(a.newEventHandler()),
        ws.WithCardHandler(a.newCardHandler()),  // ← 新增
        ws.WithAutoReconnect(true),
        ws.WithLogger(SlogLogger{Logger: a.Log}),
    )
    // ...
}
```

### 3.3 Phase 3: 卡片回调处理器

**文件**: `internal/messaging/feishu/card_action.go`（新建）

```go
package feishu

import (
    "context"
    "encoding/json"
    "fmt"

    larkcard "github.com/larksuite/oapi-sdk-go/v3/card"
    "github.com/hrygo/hotplex/internal/messaging"
    "github.com/hrygo/hotplex/pkg/events"
)

// 交互动作前缀，与 Slack 的 hp_interact/ 前缀保持语义一致
const actionPrefix = "hp_interact"

// 交互动作类型
const (
    actionAllow   = "allow"
    actionDeny    = "deny"
    actionAnswer  = "answer"
    actionAccept  = "accept"
    actionDecline = "decline"
)

// handleCardAction 处理飞书卡片按钮回调。
// 返回值用于原地更新卡片内容（变色显示审批结果）。
func (a *Adapter) handleCardAction(ctx context.Context, cardAction *larkcard.CardAction) (interface{}, error) {
    if cardAction.Action == nil || cardAction.Action.Value == nil {
        return nil, nil
    }

    // 提取按钮 value 中的 action 和 request_id
    val := cardAction.Action.Value
    actionType, _ := val["action"].(string)
    requestID, _ := val["request_id"].(string)

    if actionType == "" || requestID == "" {
        return nil, nil
    }

    // 完整 action key: "hp_interact/allow/req_xxx"
    actionKey := actionPrefix + "/" + actionType + "/" + requestID

    // 验证操作者身份
    operatorID := cardAction.OpenID

    // 从 InteractionManager 中完成交互
    pi, ok := a.Interactions.Complete(requestID)
    if !ok {
        // 已被响应或超时
        return buildResolvedCard(actionDeny, "已过期或已响应", ""), nil
    }

    // 验证所有者
    if pi.OwnerID != "" && pi.OwnerID != operatorID {
        // 非所有者操作，重新注册（不消耗）
        a.Interactions.Register(pi)
        return nil, nil
    }

    // 构建响应 metadata 并发送
    var metadata map[string]any
    var resultLabel string
    var resultColor string

    switch {
    case actionType == actionAllow:
        metadata = messaging.BuildPermissionResponse(requestID, true, "")
        resultLabel = "✅ 已允许"
        resultColor = larkcard.TemplateGreen
    case actionType == actionDeny:
        metadata = messaging.BuildPermissionResponse(requestID, false, "user denied")
        resultLabel = "🚫 已拒绝"
        resultColor = larkcard.TemplateRed
    case actionType == actionAnswer:
        answer, _ := val["answer"].(string)
        if answer == "" {
            answer, _ = val["label"].(string)
        }
        metadata = messaging.BuildQuestionResponse(requestID, answer)
        resultLabel = "✅ 已回答"
        resultColor = larkcard.TemplateGreen
    case actionType == actionAccept:
        metadata = messaging.BuildElicitationResponse(requestID, "accept")
        resultLabel = "✅ 已接受"
        resultColor = larkcard.TemplateGreen
    case actionType == actionDecline:
        metadata = messaging.BuildElicitationResponse(requestID, "decline")
        resultLabel = "🚫 已拒绝"
        resultColor = larkcard.TemplateRed
    default:
        return nil, nil
    }

    // 发送响应到 Worker
    pi.SendResponse(metadata)

    a.Log.Info("feishu: interaction resolved via card button",
        "request_id", requestID,
        "action", actionType,
        "operator", operatorID)

    // 返回更新后的卡片（原地变色）
    return buildResolvedCard(actionType, resultLabel, resultColor), nil
}
```

### 3.4 Phase 4: 交互式卡片模板

**文件**: `internal/messaging/feishu/card_template.go`（扩展）

#### 3.4.1 权限请求卡片（带按钮）

```go
// buildPermissionCardWithButtons 构建带交互按钮的权限请求卡片
func buildPermissionCardWithButtons(data *events.PermissionRequestData) string {
    body := fmt.Sprintf("**⚠️ 工具执行授权**\nClaude Code 请求：\n📝 **%s**", data.ToolName)
    if data.Description != "" && data.Description != data.ToolName {
        body += fmt.Sprintf("\n> %s", data.Description)
    }
    if len(data.Args) > 0 && data.Args[0] != "{}" {
        preview := data.Args[0]
        if len(preview) > 500 {
            preview = preview[:500] + "..."
        }
        preview = strings.ReplaceAll(preview, "```", "")
        body += fmt.Sprintf("\n```\n%s\n```", preview)
    }

    // 按钮行
    actions := map[string]any{
        "tag": "action",
        "actions": []map[string]any{
            {
                "tag": "button",
                "text": map[string]any{"tag": "plain_text", "content": "✅ 允许"},
                "type": "primary",
                "value": map[string]any{
                    "action":     actionAllow,
                    "request_id": data.ID,
                },
            },
            {
                "tag": "button",
                "text": map[string]any{"tag": "plain_text", "content": "❌ 拒绝"},
                "type": "danger",
                "value": map[string]any{
                    "action":     actionDeny,
                    "request_id": data.ID,
                },
            },
        },
    }

    elements := []map[string]any{
        {"tag": "markdown", "content": body},
        actions,
        {"tag": "hr"},
        {"tag": "markdown", "content": fmt.Sprintf("📋 请求ID: `%s`", data.ID)},
    }

    return buildCard(cardHeader{
        Title:    "工具执行授权",
        Subtitle: data.ToolName,
        Template: headerOrange,
        Tags:     []cardTag{{Text: "pending", Color: "orange"}},
    }, map[string]any{"wide_screen_mode": true}, elements)
}
```

#### 3.4.2 问题请求卡片（带选项按钮）

```go
func buildQuestionCardWithButtons(data *events.QuestionRequestData) string {
    var elements []map[string]any

    for _, q := range data.Questions {
        headerLabel := q.Header
        if headerLabel == "" {
            headerLabel = "Question"
        }
        elements = append(elements, map[string]any{
            "tag":     "markdown",
            "content": fmt.Sprintf("**%s**\n%s", headerLabel, q.Question),
        })

        if len(q.Options) > 0 {
            var buttons []map[string]any
            for _, opt := range q.Options {
                label := opt.Label
                if len(label) > 75 {
                    label = label[:75] + "..."
                }
                buttons = append(buttons, map[string]any{
                    "tag": "button",
                    "text": map[string]any{"tag": "plain_text", "content": label},
                    "type": "default",
                    "value": map[string]any{
                        "action":     actionAnswer,
                        "request_id": data.ID,
                        "answer":     opt.Label,
                        "label":      label,
                    },
                })
            }
            elements = append(elements, map[string]any{
                "tag":     "action",
                "actions": buttons,
            })
        }
        elements = append(elements, map[string]any{"tag": "hr"})
    }

    // 自定义答案提示
    elements = append(elements, map[string]any{
        "tag":     "markdown",
        "content": fmt.Sprintf("📋 请求ID: `%s`\n💬 也可直接回复自定义答案", data.ID),
    })

    return buildCard(cardHeader{
        Title:    "用户输入请求",
        Template: headerYellow,
    }, map[string]any{"wide_screen_mode": true}, elements)
}
```

#### 3.4.3 MCP Elicitation 卡片（带按钮）

```go
func buildElicitationCardWithButtons(data *events.ElicitationRequestData) string {
    body := fmt.Sprintf("**🔗 MCP Server Request**\n`%s` 请求输入：\n%s", data.MCPServerName, data.Message)

    elements := []map[string]any{
        {"tag": "markdown", "content": body},
        {
            "tag": "action",
            "actions": []map[string]any{
                {
                    "tag": "button",
                    "text": map[string]any{"tag": "plain_text", "content": "✅ 接受"},
                    "type": "primary",
                    "value": map[string]any{
                        "action":     actionAccept,
                        "request_id": data.ID,
                    },
                },
                {
                    "tag": "button",
                    "text": map[string]any{"tag": "plain_text", "content": "❌ 拒绝"},
                    "type": "danger",
                    "value": map[string]any{
                        "action":     actionDecline,
                        "request_id": data.ID,
                    },
                },
            },
        },
    }

    if data.URL != "" {
        elements = append(elements, map[string]any{"tag": "hr"})
        elements = append(elements, map[string]any{
            "tag":     "markdown",
            "content": fmt.Sprintf("📎 [外部表单](%s)", data.URL),
        })
    }

    return buildCard(cardHeader{
        Title:    "MCP Server 请求",
        Subtitle: data.MCPServerName,
        Template: headerViolet,
    }, map[string]any{"wide_screen_mode": true}, elements)
}
```

#### 3.4.4 结果卡片（按钮点击后原地替换）

```go
// buildResolvedCard 构建审批结果卡片，用于 card action 回调响应（原地替换）
func buildResolvedCard(action, label, color string) map[string]any {
    if color == "" {
        if action == actionDeny || action == actionDecline {
            color = "red"
        } else {
            color = "green"
        }
    }

    return map[string]any{
        "config": map[string]any{"wide_screen_mode": true},
        "header": map[string]any{
            "title":    map[string]any{"tag": "plain_text", "content": label},
            "template": color,
        },
    }
}
```

### 3.5 Phase 5: 改造 interaction.go

**文件**: `internal/messaging/feishu/interaction.go`

将 `sendPermissionRequest` / `sendQuestionRequest` / `sendElicitationRequest` 中的卡片构建从 `buildInteractionCard()` 切换到新的带按钮版本：

```go
func (c *FeishuConn) sendPermissionRequest(ctx context.Context, env *events.Envelope) error {
    data, err := messaging.ExtractPermissionData(env)
    if err != nil {
        return fmt.Errorf("feishu: extract permission data: %w", err)
    }

    // 尝试发送带按钮的交互卡
    cardJSON := buildPermissionCardWithButtons(data)
    if err := c.adapter.sendCardMessage(ctx, c.chatID, cardJSON); err != nil {
        // Fallback 到纯文本
        c.adapter.Log.Warn("feishu: interactive card failed, trying text fallback", "err", err)
        fallback := buildPermissionFallbackText(data)
        if fbErr := c.adapter.sendTextMessage(ctx, c.chatID, fallback); fbErr != nil {
            return fmt.Errorf("feishu: send permission request: card=%w, fallback=%s", err, fbErr.Error())
        }
    }

    c.adapter.registerInteraction(data.ID, env.SessionID, env.OwnerID, events.PermissionRequest, c)
    return nil
}
```

**关键改动**：
- 卡片发送优先使用带按钮版本
- 发送失败自动 fallback 到纯文本（保留文字回复通道）
- `checkPendingInteraction` **保留不变**——文字回复仍作为 fallback 通道
- 按钮和文字**先到先得**：`InteractionManager.Complete()` 是原子操作，第一个响应获胜

### 3.6 Phase 6: 移除过时注释

**文件**: 多处注释清理

| 文件 | 行 | 内容 | 操作 |
|------|---|------|------|
| `feishu/interaction.go:15-16` | "Since the Feishu WS client does not forward card.action.trigger events..." | 删除 |
| `feishu/AGENTS.md:60` | "Permission request: display-only card..." | 更新 |
| `feishu/AGENTS.md:86` | "Assume card.action.trigger works — WS client doesn't forward them" | 删除 |
| `docs/archive/legacy-docs/Product-Whitepaper.md:578` | 历史白皮书记录 | 保留（归档不改动） |

---

## 4. 跨平台验证

### 4.1 Slack（已实现，无需改动）

```
交互卡: Block Kit ActionBlock + ButtonBlockElement
动作ID: hp_interact/<type>/<requestID> (见 slack/interaction.go)
回调: socketmode.EventTypeInteractive → slack.InteractionTypeBlockActions
处理: handleInteractionEvent() → Interactions.Complete() → pi.SendResponse()
Fallback: 文字 "allow <id>" / "deny <id>"
```

### 4.2 WebChat（已实现，无需改动）

```
交互UI: 浏览器 React 组件（permission/question/elicitation）
协议: AEP WebSocket 双向通信
发送: sendPermissionResponse(id, allowed, reason)
超时: 浏览器端自行管理
```

### 4.3 Feishu（本次实现）

```
交互卡: CardKit v2 action 元素 + button 元素
动作值: {"action": "<type>", "request_id": "<id>"}
回调: MessageTypeCard → CardActionHandler → handleCardAction()
Fallback: 文字 "允许/拒绝"（保留 checkPendingInteraction）
```

### 4.4 统一交互流程

所有平台共享同一后端：

```
Worker → AEP PermissionRequest → Gateway Handler → PlatformConn.WriteCtx
                                                         │
                                          ┌──────────────┼──────────────┐
                                          ▼              ▼              ▼
                                     Feishu          Slack         WebChat
                                   发送按钮卡     发送 Block Kit    前端渲染
                                          │              │              │
                                          ▼              ▼              ▼
                                   用户点击按钮   用户点击按钮    用户点击按钮
                                          │              │              │
                                          ▼              ▼              ▼
                                  handleCardAction  handleInteraction  sendResponse
                                          │              │              │
                                          └──────┬───────┘──────────────┘
                                                 ▼
                                    InteractionManager.Complete()
                                                 │
                                                 ▼
                                        SendResponse(metadata)
                                                 │
                                                 ▼
                                    AEP Input → Worker → 命令执行/拒绝
```

---

## 5. 测试计划

### 5.1 单元测试

| 测试 | 文件 | 覆盖 |
|------|------|------|
| `TestBuildPermissionCardWithButtons` | `card_template_test.go` | 卡片 JSON 结构正确、按钮 value 包含正确 action/request_id |
| `TestBuildQuestionCardWithButtons` | `card_template_test.go` | 多选项按钮生成、长标签截断 |
| `TestBuildElicitationCardWithButtons` | `card_template_test.go` | 接受/拒绝按钮、URL 附件 |
| `TestHandleCardAction` | `card_action_test.go` | 各 action 类型路由、owner 验证、已过期处理 |
| `TestBuildResolvedCard` | `card_template_test.go` | 绿色/红色模板、原地替换 JSON |

### 5.2 集成测试

| 场景 | 步骤 | 预期 |
|------|------|------|
| 权限允许 | 1. Worker 发 PermissionRequest<br>2. 用户点击"允许"按钮<br>3. 验证 Worker 收到 permission_response | allowed=true, 卡片变绿 |
| 权限拒绝 | 同上，点击"拒绝" | allowed=false, 卡片变红 |
| 文字 fallback | 发送按钮卡后，用户不打字直接发"允许" | 文字通道仍可用，按钮卡显示"已过期" |
| 超时 | 发送按钮卡后等待 5min | 自动拒绝，卡片不变色 |
| 非所有者 | 其他用户点击按钮 | 操作被忽略，卡片不变 |
| SDK 补丁 | WS 收到 MessageTypeCard 帧 | 正确路由到 cardHandler，不再丢弃 |

### 5.3 验收标准

1. ✅ 飞书权限请求卡片显示"允许"和"拒绝"两个可点击按钮
2. ✅ 点击"允许"后卡片原地变绿，Worker 收到授权
3. ✅ 点击"拒绝"后卡片原地变红，Worker 收到拒绝
4. ✅ 5 分钟无响应自动拒绝
5. ✅ 文字回复通道仍可正常使用
6. ✅ 按钮和文字先到先得，不重复响应
7. ✅ Slack 和 WebChat 交互行为不受影响

---

## 6. 修改文件清单

| 文件 | 操作 | 改动量 | 说明 |
|------|------|-------|------|
| `vendor/patches/oapi-sdk-go-v3/ws/client.go` | 修改 | ~10行 | 启用 WithCardHandler + 路由 MessageTypeCard |
| `go.mod` | 修改 | 1行 | 添加 replace 指令 |
| `internal/messaging/feishu/card_action.go` | **新建** | ~120行 | 卡片回调处理器 |
| `internal/messaging/feishu/card_template.go` | 修改 | ~150行 | 新增 4 个卡片构建函数 |
| `internal/messaging/feishu/interaction.go` | 修改 | ~30行 | 切换到带按钮卡片、删除过时注释 |
| `internal/messaging/feishu/ws.go` | 修改 | ~10行 | 注册 newCardHandler |
| `internal/messaging/feishu/AGENTS.md` | 修改 | ~5行 | 更新文档 |

**总改动量**：~325 行（含测试）

---

## 7. 风险与缓解

| 风险 | 影响 | 概率 | 缓解 |
|------|------|------|------|
| SDK 补丁与未来官方更新冲突 | 低 | 中 | 补丁范围极小（2处），可轻松 rebase；向上游提 PR |
| 飞书 WS 不实际投递 MessageTypeCard | 高 | 低 | Hermes Agent 已在 Python SDK 验证此路径可行；Go SDK 帧解析已就绪 |
| 卡片回调 3 秒超时 | 中 | 低 | handleCardAction 内部逻辑简单（map lookup + channel send），远低于 3 秒 |
| 按钮卡在飞书旧版客户端不显示 | 低 | 中 | Fallback 到纯文字通道已保留 |

---

## 8. 飞书开放平台配置

**前置条件**（需在飞书开放平台后台确认）：

1. 应用功能 → 机器人 → 开启「卡片事件回调」
2. 应用功能 → 机器人 → 确认已拥有 `im:message` + `im:message:send_as_bot` 权限
3. 无需配置 Webhook URL（使用 WS 模式接收回调）
