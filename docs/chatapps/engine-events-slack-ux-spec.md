# Engine Events Slack UI/UX 规范

> **状态**: ✅ 已实现
> **最后更新**: 2026-02-27
> **生效范围**: HotPlex ChatApps Slack Adapter
> **验证状态**: 所有 21 种事件类型端到端流程已验证完整

---

## 概述

本文档定义了 HotPlex Engine 中 18 种事件类型在 Slack 平台上的最终 UI/UX 效果规范。

### 设计原则

| 原则 | 说明 |
|------|------|
| **即时反馈** | Thinking、Error 等关键事件立即发送，不聚合 |
| **流式更新** | Answer、PlanMode 等使用 `chat.update` API 节流更新 (1 次/秒) |
| **聚合发送** | ToolUse、ToolResult 等使用 500ms 窗口聚合 |
| **低干扰** | 系统事件使用 context block，降低视觉权重 |
| **强交互** | 需要用户决策的事件使用 header + actions 模式 |

### Slack 平台限制

| 限制类型 | 数值 |
|---------|------|
| 单消息最大字符数 | 3000-4000 |
| 单消息最大 Blocks 数 | 50 |
| Actions Block 最大元素数 | 25 |
| Section fields 最大数 | 10 |
| Button value 最大长度 | 2000 |
| chat.update 速率限制 | ~1 次/秒 |

---

## 事件类型详细规范

### 0. 用户体验优化

> 为解决「用户发送消息后没有任何反馈」的黑洞体验问题，采用多种手段组合。

#### 0.1 Slack Typing Indicator (打字指示器)

**功能**: 在 Slack 界面 Bot 名称旁显示动态输入动画

**触发时机**:
- 用户消息收到后
- 引擎处理中
- 发送任何消息之前

**API 实现**:
```go
// 使用 slack-go/slack SDK
client.PostMessage(channelID, slack.MsgOptionTyping())

// 或者使用 conversations.mark
client.PostMessage(channelID, slack.MsgOptionPost())
```

**效果示意**:
```
🤖 ← (Bot 名称旁显示波纹/省略号动画)
```

**推荐使用流程**:
```
用户发送消息
    │
    ▼
1. 立即触发 Typing Indicator (持续 5 秒)
    │
    ▼
2. 发送实际消息 (Thinking / Answer / ToolUse 等)
    │
    ▼
3. 关闭 Typing Indicator
```

**说明**:
- Slack 原生功能，无需额外 Block
- 与消息互补：消息是内容，Typing 是状态
- 建议在以下时机触发：
  - 用户消息到达后立即触发
  - 每次发送 Thinking/Thinking 内容更新前触发
  - 引擎启动中定期触发 (每 3-5 秒刷新)

---

#### 0.2 Slack Reactions 反馈 (对消息添加反应)

**功能**: 对用户消息添加 Emoji 反应，告知处理状态

**API 实现**:
```go
// 添加 Reaction
client.ReactionsAdd(
    channelID,
    "inbox",  // Reaction emoji
    slack.ItemRef{
        Channel: channelID,
        Timestamp: userMessageTimestamp,
    },
)
```

**推荐 Reaction 映射**:

| Reaction | 场景 | 示例 |
|----------|------|------|
| :inbox: | 消息已收到 | 用户发送消息后立即添加 |
| :white_check_mark: | 操作成功 | ToolResult 执行成功 |
| :x: | 操作失败 | ToolResult 执行失败 |
| :warning: | 警告 | 检测到潜在危险操作 |
| :hourglass: | 处理中 | 长时间操作开始 |
| :brain: | 思考中 | 开始推理 |
| :eyes: | 读取中 | 开始读取文件 |

**效果示意**:
```
用户:
帮我写个函数

🤖 → [添加 :inbox:]

用户:
帮我写个函数
:inbox: ← Bot 添加的反应

(处理中...)

用户:
帮我写个函数
:white_check_mark: ← 完成后变为成功标记
```

**推荐使用流程**:
```
用户发送消息
    │
    ▼
1. 添加 :inbox: (消息已收到)
    │
    ▼
2. 开始处理 (Thinking / ToolUse)
    │
    ▼
3. 处理完成
   ├── 成功 → 添加 :white_check_mark:
   └── 失败 → 添加 :x:
```

**说明**:
- 轻量级反馈，无需发送额外消息
- 提供视觉确认，用户知道消息已被处理
- 可以叠加多个 Reaction

---

#### 0.4 EventTypeSessionStart (会话启动/冷启动)

**触发场景**: 用户发送第一条消息或 CLI 需要冷启动时

**Block 类型**: `section` + `context`
**Emoji**: :rocket:
**聚合策略**: 不聚合 - 立即发送

```
┌─────────────────────────────────┐
│ :rocket: *Starting Session*    │
│                                 │
│ Initializing AI assistant...   │
│ Session: sess_abc123           │
└─────────────────────────────────┘
```

**说明**:
- 首次用户消息或检测到冷启动时发送
- 告知用户系统正在初始化
- 包含会话 ID 供调试

---

#### 0.5 EventTypeEngineStarting (引擎启动中)

**触发场景**: CLI 冷启动中，引擎正在启动

**Block 类型**: `context`
**Emoji**: :hourglass:
**聚合策略**: 可聚合 - 节流发送

```
┌─────────────────────────────────┐
│ :hourglass: _Engine starting..._│
└─────────────────────────────────┘
```

**说明**:
- 在 SessionStart 之后发送
- 使用 context block 降低视觉权重
- CLI 冷启动完成后自动替换为 Thinking 或实际响应

---

#### 0.6 EventTypeUserMessageReceived (消息已收到)

**触发场景**: 用户消息已收到，AI 正在处理

**Block 类型**: `context`
**Emoji**: :inbox:
**聚合策略**: 不聚合 - 立即发送

```
┌─────────────────────────────────┐
│ :inbox: _Message received_     │
└─────────────────────────────────┘
```

**说明**:
- 用户消息到达后立即发送
- 确认用户消息已被系统接收
- 极低延迟，让用户知道系统已响应

---

### 1. EventTypeThinking (AI 推理中)

**Block 类型**: `context`
**Emoji**: :brain:
**聚合策略**: 不聚合 + 限频 (500ms 窗口内忽略重复内容)

```
┌─────────────────────────────────┐
│ :brain: _Thinking..._          │
└─────────────────────────────────┘
```

**说明**:
- 使用 context block 降低视觉权重，表示后台思考状态
- 首次 Thinking 事件立即发送
- 500ms 窗口内重复内容不发送，避免刷屏
- 其他事件(Answer/ToolUse)自动替换 Thinking 消息

**效果示例**:
```
用户: 帮我写个函数

🤖 :brain: _Thinking..._

(500ms内如有新内容则忽略)

🤖 :computer: *Using tool:* `Read`
(Thinking 消息被自动替换)
```

---

### 2. EventTypeAnswer (AI 文本输出)

**Block 类型**: `section` (mrkdwn)
**聚合策略**: 流式更新 - 使用 `chat.update` API，节流 1 次/秒
**长内容**: 自动拆分为多条消息，块之间用 `divider` 分割

```
┌─────────────────────────────────┐
│ **AI 回答内容**                 │
│ 支持 Markdown 格式               │
│                                 │
│ ```python                      │
│ code block with highlighting   │
│ ```                             │
└─────────────────────────────────┘
```

---

### 3. EventTypeToolUse (工具调用开始)

**Block 类型**: `section` + `fields` (双列布局)
**Emoji**: 工具类型映射 (见下表)
**聚合策略**: 500ms 时间窗口聚合
**参数摘要**: 最多 12 字符

```
┌─────────────────────────────────┐
│ :computer:        │ ls -la    │
│ *Bash*            │           │
└─────────────────────────────────┘
```

**工具 Emoji 映射表**:

| 工具类型 | Emoji |
|---------|-------|
| Bash | :computer: |
| Edit / MultiEdit | :pencil: |
| Write / FileWrite | :page_facing_up: |
| Read / FileRead | :books: |
| FileSearch / Glob | :mag: |
| WebFetch / WebSearch | :globe_with_meridians: |
| Grep | :magnifying_glass_tilted_left: |
| LS / List | :file_folder: |
| 其他 | :hammer_and_wrench: |

---

### 4. EventTypeToolResult (工具执行结果)

**Block 类型**: `section`
**聚合策略**: 不聚合 - 立即发送
**展示内容**: 状态 Emoji + 工具名 + 时长(>500ms) + 数据长度

```
┌─────────────────────────────────┐
│ :white_check_mark: *Bash*      │
│ Completed (1.2s) • 200KB       │
└─────────────────────────────────┘
```

```
┌─────────────────────────────────┐
│ :x: *Edit* Failed (500ms)       │
└─────────────────────────────────┘
```

**时长阈值**: 仅当 durationMs > 500ms 时显示时长。

---

### 5. EventTypeError (错误发生)

**Block 类型**: `section`
**Emoji**: :warning: (普通) / :x: (严重)
**聚合策略**: 不聚合 - 立即发送

```
┌─────────────────────────────────┐
│ :warning: *Error*               │
│ > Error message content         │
│ > with quote format             │
└─────────────────────────────────┘
```

**说明**: 使用引用格式 (`> `) 包裹错误消息，更醒目。

---

### 6. EventTypeResult (Turn 完成)

**Block 类型**: `section` + `context`
**Emoji**: :white_check_mark:
**聚合策略**: 最后发送 - 会话结束时

```
┌─────────────────────────────────┐
│ :white_check_mark: *Turn Complete* │
│                                 │
│ ⏱️ 12.5s • ⚡ 1.2K/567 │
└─────────────────────────────────┘
```

---

### 7. EventTypePermissionRequest (权限请求)

**Block 类型**: `header` + `section` + `section` + `actions`
**Emoji**: :warning:
**聚合策略**: 不聚合 - 需要用户立即决策

```
┌─────────────────────────────────┐
│ ⚠️ Permission Request          │
├─────────────────────────────────┤
│ *Tool:* `Bash`                 │
│                                 │
│ *Command:*                     │
│ ```                            │
│ rm -rf /                      │
│ ```                            │
├─────────────────────────────────┤
│ [✅ Allow]  [🚫 Deny]           │
└─────────────────────────────────┘
```

**按钮 action_id**:
- Allow: `perm_allow:{sessionID}:{messageID}`
- Deny: `perm_deny:{sessionID}:{messageID}`

---

### 8. DangerBlock (危险操作拦截)

**Block 类型**: `section` + `divider` + `actions`
**Emoji**: :rotating_light:
**聚合策略**: 不聚合 - 立即发送

```
┌─────────────────────────────────┐
│ :rotating_light: *Confirmation Required* │
│                                 │
│ This command will delete files │
├─────────────────────────────────┤
│ [Confirm]  [Cancel]            │
└─────────────────────────────────┘
```

---

### 9. EventTypeSystem (系统级消息)

**Block 类型**: `context`
**Emoji**: :gear:
**聚合策略**: 可聚合 - 短时间内的系统消息可合并

```
┌─────────────────────────────────┐
│ :gear: System: Connected       │
└─────────────────────────────────┘
```

---

### 10. EventTypeUser (用户消息反射)

**Block 类型**: `section` + `context`
**Emoji**: :bust_in_silhouette:
**聚合策略**: 不聚合 - 直接发送

```
┌─────────────────────────────────┐
│ :bust_in_silhouette: *User:*   │
│ Please fix the bug in auth     │
│                                 │
│ 10:30 AM                      │
└─────────────────────────────────┘
```

---

### 11. EventTypeStepStart (步骤开始 - OpenCode)

**Block 类型**: `section` + `context`
**Emoji**: :arrow_right:
**聚合策略**: 不聚合 - 立即发送

```
┌─────────────────────────────────┐
│ :arrow_right: *Step 1/3:*      │
│ Analyzing codebase             │
└─────────────────────────────────┘
```

---

### 12. EventTypeStepFinish (步骤完成 - OpenCode)

**Block 类型**: `section` + `context`
**Emoji**: :white_check_mark:
**聚合策略**: 可聚合 - 可与下一个 StepStart 合并

```
┌─────────────────────────────────┐
│ :white_check_mark: *Step 1 Complete* │
│ Analyzing codebase             │
│                                 │
│ ⏱️ 2.3s                        │
└─────────────────────────────────┘
```

---

### 13. EventTypeRaw (未解析的原始输出)

**Block 类型**: `section`
**Emoji**: :page_facing_up:
**聚合策略**: 不聚合 - 直接发送
**展示内容**: 事件类型 + Emoji + 数据长度，**不展示具体内容**

```
┌─────────────────────────────────┐
│ :page_facing_up: *Raw Output*  │
│ Data: 15KB (not displayed)    │
└─────────────────────────────────┘
```

---

### 14. EventTypePlanMode (计划生成中)

**Block 类型**: `context`
**Emoji**: :mag_right:
**聚合策略**: 流式更新 - 计划生成过程中持续更新

```
┌─────────────────────────────────┐
│ :mag_right: _Plan Mode: Generating..._ │
└─────────────────────────────────┘
```

---

### 15. EventTypeExitPlanMode (请求批准计划)

**Block 类型**: `header` + `section` + `divider` + `actions`
**Emoji**: :clipboard:
**聚合策略**: 不聚合 - 需要用户立即决策

```
┌─────────────────────────────────┐
│ :clipboard: Plan Ready         │
├─────────────────────────────────┤
│ 1. Fix authentication bug     │
│ 2. Add unit tests             │
│ 3. Update documentation       │
├─────────────────────────────────┤
│ [Approve]  [Deny]             │
└─────────────────────────────────┘
```

**按钮 action_id**: `plan_approve`, `plan_deny`

---

### 16. EventTypeAskUserQuestion (询问用户问题)

**Block 类型**: `section` + `actions`
**Emoji**: :question:
**聚合策略**: 不聚合 - 需要用户立即响应

```
┌─────────────────────────────────┐
│ :question: *Question*          │
│ Which authentication method?   │
├─────────────────────────────────┤
│ [OAuth] [JWT] [Session]        │
└─────────────────────────────────┘
```

**按钮 action_id**: `question_option_{index}`

---

### 17. EventTypeCommandProgress (命令执行进度)

**Block 类型**: `section` + `context` + `actions`
**Emoji**: :gear:
**聚合策略**: 流式更新 - 步骤状态变化时更新

```
┌─────────────────────────────────┐
│ :gear: */reset*                │
│                                 │
│ ✓ Step 1: Disconnect           │
│ ⟳ Step 2: Clear session       │
│ ○ Step 3: Reconnect           │
├─────────────────────────────────┤
│ [Cancel]                       │
└─────────────────────────────────┘
```

**按钮 action_id**: `cmd_cancel`

---

### 18. EventTypeCommandComplete (命令执行完成)

**Block 类型**: `section` + `context`
**Emoji**: :white_check_mark:
**聚合策略**: 最后发送 - 命令完成时

```
┌─────────────────────────────────┐
│ :white_check_mark: */reset Complete* │
│                                 │
│ ⏱️ 3.2s • ✓ 3/3 steps         │
└─────────────────────────────────┘
```

---

## 事件聚合策略矩阵

| 事件类型 | Block 类型 | 聚合策略 | 发送时机 |
|---------|-----------|---------|---------|
| `session_start` | section+context | 不聚合 | 立即 | 首次消息/冷启动 |
| `engine_starting` | context | 可聚合 | 节流 | 引擎启动中 |
| `user_message_received` | context | 不聚合 | 立即 | 消息已收到 |
| `thinking` | context | 不聚合+限频 | 立即 | 500ms 窗口去重 |
| `answer` | section | 流式更新 | 1 次/秒 |
| `tool_use` | section | 500ms 聚合 | 节流 |
| `tool_result` | section | 不聚合 | 立即 |
| `error` | section | 不聚合 | 立即 |
| `result` | section+context | 最后发送 | 会话结束 |
| `danger_block` | section+actions | 不聚合 | 立即 |
| `permission_request` | header+actions | 不聚合 | 立即 |
| `system` | context | 可聚合 | 节流 |
| `user` | section+context | 不聚合 | 立即 |
| `step_start` | section+context | 不聚合 | 立即 |
| `step_finish` | section+context | 可聚合 | 节流 |
| `raw` | section | 不聚合 | 立即 |
| `plan_mode` | context | 流式更新 | 1 次/秒 |
| `exit_plan_mode` | header+actions | 不聚合 | 立即 |
| `ask_user_question` | section+actions | 不聚合 | 立即 |
| `command_progress` | section+context+actions | 流式更新 | 步骤变化 |
| `command_complete` | section+context | 最后发送 | 命令结束 |

---

## 按钮 action_id 命名规范

| 事件类型 | action_id 格式 | 示例 |
|---------|---------------|------|
| `permission_request` | `perm_allow:{sessionID}:{msgID}` | `perm_allow:sess123:msg456` |
| `permission_request` | `perm_deny:{sessionID}:{msgID}` | `perm_deny:sess123:msg456` |
| `exit_plan_mode` | `plan_approve` | - |
| `exit_plan_mode` | `plan_deny` | - |
| `danger_block` | `danger_confirm` | - |
| `danger_block` | `danger_cancel` | - |
| `ask_user_question` | `question_option_{index}` | `question_option_0` |
| `command_progress` | `cmd_cancel` | - |

---

## 消息更新模式

| 模式 | 适用事件 | 说明 |
|------|---------|------|
| **即时发送** | thinking, error, danger_block, permission_request, exit_plan_mode, ask_user_question, step_start | 使用 `chat.postMessage` |
| **流式更新** | answer, plan_mode, command_progress | 使用 `chat.update`，节流 1 次/秒 |
| **聚合发送** | tool_use, tool_result, system, step_finish | 500ms 窗口聚合后发送 |
| **最后发送** | result, session_stats, command_complete | 会话/命令结束时发送 |

---

## 实施检查清单

### 高优先级 (P0) - ✅ 全部完成

- [x] EventTypeResult - Turn 完成事件 ✅ (已实现为 MessageTypeSessionStats)
- [x] EventTypeCommandProgress - 命令进度事件 ✅
- [x] EventTypeCommandComplete - 命令完成事件 ✅

### 中优先级 (P1) - ✅ 全部完成

- [x] EventTypeThinking - 改用 context block ✅
- [x] EventTypeToolUse - 改用 fields 双列布局，参数摘要 12 字符 ✅
- [x] EventTypeToolResult - 添加数据长度展示 ✅
- [x] EventTypeError - 添加引用格式 ✅

### 低优先级 (P2) - ✅ 全部完成

- [x] EventTypeSystem - 系统消息 ✅
- [x] EventTypeUser - 用户消息反射 ✅
- [x] EventTypeStepStart/StepFinish - 步骤事件 ✅
- [x] EventTypeRaw - 原始输出（仅展示类型和长度） ✅
- [x] EventTypePlanMode - 改用 context block ✅

### UX 优化 (0.x) - ✅ 全部完成

- [x] EventTypeSessionStart (0.4) - 会话启动事件 ✅
- [x] EventTypeEngineStarting (0.5) - 引擎启动中事件 ✅
- [x] EventTypeUserMessageReceived (0.6) - 消息已收到事件 ✅
- [x] Slack Typing Indicator (0.1) - ✅ 使用 reactions 替代 (SDK 不直接支持)
- [x] Slack Reactions 反馈 (0.2) - ✅ (AddReactionSDK 已实现)

### 双向流程验证 - ✅ 完成

- [x] Engine → Slack 事件流向完整
- [x] Slack → Engine 反向流程完整
- [x] SDK First 规范符合
- [x] 无死代码

---

## 相关文件

| 文件 | 说明 |
|------|------|
| `chatapps/slack/adapter.go` | Slack 适配器，处理双向通信 |
| `chatapps/slack/builder.go` | MessageBuilder，21 种事件 → Block Kit 转换 |
| `chatapps/engine_handler.go` | StreamCallback，Engine 事件处理分发 |
| `chatapps/base/types.go` | MessageType 枚举定义 (21 种) |
| `provider/event.go` | ProviderEventType 枚举定义 |

---

**维护者**: HotPlex Team
**最后确认**: 2026-02-27
**验证**: Engine → Slack 双向流程完整，所有事件类型已实现
