---
type: spec
tags: [project/HotPlex, messaging/feishu, messaging/slack, webchat, platform-adapter]
date: 2026-05-20
status: implemented
priority: high
estimated_hours: 20
last_updated: 2026-05-21
research_sources:
  - Hermes Agent /home/hotplex/.hermes/hermes-agent (approval.py, feishu.py, slack.py, tui_gateway)
  - HotPlex /home/hotplex/.hotplex/workspace/hotplex (feishu/*, slack/*, gateway/*)
  - Lark SDK v3.5.3 ws/client.go, card/card.go, card/model.go
sdk_upgrade:
  from: v3.5.3
  to: v3.9.3
  notes: "v3.9.3 EventDispatcher natively supports OnP2CardActionTrigger; no SDK patching or go.mod replace required"
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

飞书交互式按钮的回调路径在 **Lark SDK v3.9.3** 中已得到原生支持。SDK 的 `EventDispatcher` 内置 `OnP2CardActionTrigger` 注册入口：

- 卡片按钮点击事件通过 `MessageTypeEvent` 帧传输，event_type 字段为 `card.action.trigger`
- `EventDispatcher.Do()` 通过 `callbackType2CallbackHandler` 自动路由到注册的 `OnP2CardActionTrigger` 处理器
- 无需任何 SDK 补丁、go.mod replace 指令或 vendor 目录覆盖

**关键结论**：升级到 SDK v3.9.3 即可使用 `OnP2CardActionTrigger` 路由卡片回调，无需修改 SDK 源码。

---

## 2. 方案设计

### 2.1 方案：EventDispatcher.OnP2CardActionTrigger

**采用方案**：使用 SDK v3.9.3 内置的 `EventDispatcher.OnP2CardActionTrigger` 路由卡片按钮回调。

- **触发路径**：卡片按钮点击 → 飞书 WS → `MessageTypeEvent` 帧（event_type=`card.action.trigger`）→ `EventDispatcher.Do()` → `callbackType2CallbackHandler` → `OnP2CardActionTrigger` 处理器
- **优势**：无 SDK 源码修改、无 `go.mod` replace、无 vendor 补丁；与官方 SDK 升级路径完全兼容
- **代价**：回调数据由 `callback.CardActionTriggerEvent` 包装（非裸 `CardAction`），返回类型为 `*callback.CardActionTriggerResponse`

### 2.2 架构总览

```
                        ┌─────────────────────────────────────────┐
                        │           HotPlex Gateway               │
                        │                                         │
  Feishu WS ───────────▶│  ws.go ──▶ newEventHandler()            │
  (MessageTypeEvent,    │            │                             │
   event_type=          │            ├── OnP2CardActionTrigger     │
   card.action.trigger) │            │     └─▶ handleCardAction   │
                        │            │          Trigger()         │
                        │            ├── P2MessageReceiveV1        │
                        │            └── P2ChatAccessEvent...      │
                        │                                          │
                        │  handleCardActionTrigger():              │
                        │    ├── 验证操作者身份                    │
                        │    ├── Interactions.Complete(requestID)  │
                        │    ├── pi.SendResponse(metadata)         │
                        │    └── 返回 wrapResolvedCard(...)        │
                        │                                          │
  Slack Socket ─────────▶│  slack/interaction.go (已实现 ✅)        │
  (BlockActions)         │                                          │
                        │                                          │
  WebChat WS ───────────▶│  gateway/conn.go → 浏览器前端 (已实现 ✅) │
  (AEP events)           │                                          │
                        └─────────────────────────────────────────┘
```

---

## 3. 实现细节

### 3.1 Phase 1: 无 SDK 改动

Lark SDK v3.9.3 的 `EventDispatcher` 已原生支持卡片回调（`OnP2CardActionTrigger`），无需任何 SDK 源码补丁、`go.mod` replace 指令或 `vendor/patches/` 目录。代码改动完全在 `internal/messaging/feishu/` 内部。

### 3.2 Phase 2: Feishu Adapter 注册卡片处理器

**文件**: `internal/messaging/feishu/ws.go`

```go
func (a *Adapter) newEventHandler() *dispatcher.EventDispatcher {
    return dispatcher.NewEventDispatcher("", "").
        // Card action handlers are registered first — EventDispatcher.Do()
        // checks callbacks before events, mirroring that priority order.
        OnP2CardActionTrigger(a.handleCardActionTrigger).
        OnP2MessageReceiveV1(func(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
            return a.handleMessage(ctx, event)
        }).
        // ... 其他 P2 事件处理器（Read/Reaction/ChatAccess）...
        OnP2ChatAccessEventBotP2pChatEnteredV1(a.handleChatEntered)
}
```

### 3.3 Phase 3: 卡片回调处理器

**文件**: `internal/messaging/feishu/card_action.go`（新建）

```go
package feishu

import (
	"context"
	"fmt"
	"runtime/debug"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"

	"github.com/hrygo/hotplex/internal/messaging"
)

const (
	cardActionAllow   = "allow"
	cardActionDeny    = "deny"
	cardActionAnswer  = "answer"
	cardActionAccept  = "accept"
	cardActionDecline = "decline"
)

// handleCardActionTrigger 处理飞书卡片按钮回调。
// 注册入口：newEventHandler().OnP2CardActionTrigger
// 返回值：原地更新卡片内容（变色显示审批结果），通过 wrapResolvedCard 包装。
func (a *Adapter) handleCardActionTrigger(_ context.Context, event *callback.CardActionTriggerEvent) (resp *callback.CardActionTriggerResponse, err error) {
	// Panic recovery: WS handler convention (see ws.go). Named returns are
	// required so the defer can set `err` and prevent a higher-level crash.
	defer func() {
		if r := recover(); r != nil {
			a.Log.Error("feishu: panic in card action handler", "panic", r, "stack", string(debug.Stack()))
			err = fmt.Errorf("feishu card action panic: %v", r)
		}
	}()

	if event == nil || event.Event == nil || event.Event.Action == nil || event.Event.Action.Value == nil {
		return nil, nil
	}

	val := event.Event.Action.Value
	requestID, _ := val["request_id"].(string)
	actionType, _ := val["action"].(string)

	openID := ""
	if event.Event.Operator != nil {
		openID = event.Event.Operator.OpenID
	}

	var (
		metadata      map[string]any
		resolvedLabel string
		resolvedColor string
	)

	switch actionType {
	case cardActionAllow:
		metadata = messaging.BuildPermissionResponse(requestID, true, "")
		resolvedLabel = "✅ 已允许"
		resolvedColor = "green"
	case cardActionDeny:
		metadata = messaging.BuildPermissionResponse(requestID, false, "user denied")
		resolvedLabel = "🚫 已拒绝"
		resolvedColor = "red"
	case cardActionAnswer:
		answer, _ := val["answer"].(string)
		if answer == "" {
			answer, _ = val["label"].(string)
		}
		metadata = messaging.BuildQuestionResponse(requestID, answer)
		resolvedLabel = "✅ 已回答"
		resolvedColor = "green"
	case cardActionAccept:
		metadata = messaging.BuildElicitationResponse(requestID, "accept")
		resolvedLabel = "✅ 已接受"
		resolvedColor = "green"
	case cardActionDecline:
		metadata = messaging.BuildElicitationResponse(requestID, "decline")
		resolvedLabel = "🚫 已拒绝"
		resolvedColor = "red"
	default:
		a.Log.Warn("feishu: unknown card action type", "action", actionType, "request_id", requestID)
		return nil, nil
	}

	// Owner check BEFORE Complete — preserves the interaction for non-owner
	// clicks. If we Completed first, the original watchTimeout goroutine
	// (still running) would race the re-Registered entry.
	pending, exists := a.Interactions.Get(requestID)
	if !exists {
		return wrapResolvedCard(buildResolvedCard("deny", "已过期或已响应", "")), nil
	}
	if pending.OwnerID != "" && pending.OwnerID != openID {
		return nil, nil
	}

	pi, ok := a.Interactions.Complete(requestID)
	if !ok {
		return wrapResolvedCard(buildResolvedCard("deny", "已过期或已响应", "")), nil
	}

	if pi.SendResponse != nil {
		pi.SendResponse(metadata)
	}

	a.Log.Info("feishu: interaction resolved via card button",
		"request_id", requestID, "action", actionType, "operator", openID)

	return wrapResolvedCard(buildResolvedCard(actionType, resolvedLabel, resolvedColor)), nil
}

func wrapResolvedCard(card map[string]any) *callback.CardActionTriggerResponse {
	return &callback.CardActionTriggerResponse{
		Card: &callback.Card{
			Type: "card_json",
			Data: card,
		},
	}
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
回调: MessageTypeEvent (event_type=card.action.trigger) → OnP2CardActionTrigger → handleCardActionTrigger()
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
                                   handleCardActionTrigger  handleInteraction  sendResponse
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
| EventDispatcher 路由 | WS 收到 MessageTypeEvent 帧（event_type=card.action.trigger）| 自动路由到 OnP2CardActionTrigger → handleCardActionTrigger |

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
| `internal/messaging/feishu/card_action.go` | **新建** | ~135行 | handleCardActionTrigger + wrapResolvedCard + panic recovery |
| `internal/messaging/feishu/card_template.go` | 修改 | ~150行 | 新增 4 个卡片构建函数 |
| `internal/messaging/feishu/interaction.go` | 修改 | ~30行 | 切换到带按钮卡片、删除过时注释 |
| `internal/messaging/feishu/ws.go` | 修改 | ~5行 | 在 newEventHandler() 链中添加 OnP2CardActionTrigger |
| `internal/messaging/feishu/AGENTS.md` | 修改 | ~5行 | 更新文档 |

**总改动量**：~325 行（含测试），其中 SDK 部分 0 改动

---

## 7. 风险与缓解

| 风险 | 影响 | 概率 | 缓解 |
|------|------|------|------|
| EventDispatcher 行为变更 | 低 | 低 | 使用公开 API `OnP2CardActionTrigger`，向后兼容保证高；SDK v3.9.3 已稳定 |
| 卡片回调 3 秒超时 | 中 | 低 | handleCardActionTrigger 内部逻辑简单（map lookup + channel send），远低于 3 秒 |
| 按钮卡在飞书旧版客户端不显示 | 低 | 中 | Fallback 到纯文字通道已保留 |

---

## 8. 飞书开放平台配置（2026-06 更新）

### 8.1 控制台配置路径

```
open.feishu.cn → 开发者后台 → 选择应用
  → 开发配置 → 事件与回调 → 回调配置（非"事件配置"标签页）
    ├─ 订阅方式：编辑 → 使用长连接接收回调 → 保存
    │   ⚠️ 保存前必须确保本地 WS 客户端处于已连接状态
    └─ 已订阅的回调 → 添加回调 → 卡片回传交互 (card.action.trigger)
```

### 8.2 权限要求

1. 确认已拥有 `im:message` + `im:message:send_as_bot` 权限
2. `card.action.trigger` 回调本身无权限要求

### 8.3 技术实现说明

SDK v3.9.3 的 `EventDispatcher` 已内置 `OnP2CardActionTrigger` 支持：
- 卡片回调通过 `MessageTypeEvent` 帧（event_type=`card.action.trigger`）传输
- `EventDispatcher.Do()` 的 `callbackType2CallbackHandler` 自动路由到 `OnP2CardActionTrigger`
- **无需 SDK 补丁**，直接使用 `EventDispatcher.OnP2CardActionTrigger` 注册回调处理器
