# 跨渠道交互式消息端到端可靠性设计

日期：2026-07-10

## 背景与目标

HotPlex 已通过统一 AEP 事件表达三类 Worker 交互：工具授权、问题回答和 MCP elicitation。然而飞书、Slack、WebChat 对“响应已成功交给 Worker”的判定不同，Worker 原生协议也存在差异，造成卡片误报成功、失败不可重试、空消息卡以及 Codex 首次授权无法提交等问题。

本设计保证以下完整组合都能端到端工作：

- 渠道：飞书、Slack、WebChat。
- Worker：OpenCode Server、Claude Code、Codex CLI、ACP。
- 交互类型：`permission_request`、`question_request`、`elicitation_request`。

主路径共 `3 × 4 × 3 = 36` 条。每条主路径还必须验证允许/拒绝或接受/拒绝、原生投递失败后重试、超时、重复响应、非发起人、Worker 退出以及会话取消等适用分支。

## 非目标

- 不把交互请求持久化成跨网关重启的任务队列。
- 不改变 Worker 的外部原生协议。
- 不在飞书或 Slack 回调中等待 Agent 完成后续任务；只等待 Worker 原生响应出口接受本次交互响应。
- 不向未指定的生产群或用户发送自动化验证消息。

## 已确认根因与缺口

### 飞书消息卡分裂

请求到达后，飞书立即创建占位流式卡。Worker 在没有正文 delta 的情况下返回错误时，`handleError` 先关闭仍为空正文的占位卡，再另发错误消息，形成“空消息卡 + 错误消息卡 + Turn Summary”。错误应原位终结已有消息卡。

### Codex 首次授权失败

Codex app-server 的 server request ID 是合法的 JSON-RPC `RequestId`，支持整数和字符串，整数 `0` 也合法。HotPlex 使用 `ID != 0` 判断 `id` 字段是否存在，导致 app-server 的首个 server request（`id=0`）被误判为 notification，没有建立业务 request ID 到 JSON-RPC ID 的映射。

Codex 还缺少当前协议中的两类交互：

- `item/tool/requestUserInput`：应映射为 `question_request`，响应为按问题 ID 组织的 `{answers: {id: {answers: [...]}}}`。
- `item/permissions/requestApproval`：应映射为 `permission_request`；允许时返回原请求权限的授权子集，拒绝时返回空权限集。

### Slack 提前完成

Slack 按钮和文字回复路径在调用异步 `SendResponse` 前就从 `InteractionManager` 完成交互，并立即渲染成功。Worker 投递失败时请求已经丢失，无法重试。Slack 多问题卡片也把回答压缩到 `_` 单键，不能完整表达多个问题和原顺序。

### WebChat 乐观成功

浏览器的发送方法只把帧写入 WebSocket，却被 UI 当作可等待的成功 Promise。UI 随即从 `submitting` 进入 `resolved`，而 Gateway 真正投递失败时才异步发送 Error。WebChat 缺少与 request ID 关联的正向确认。

## 统一交互契约

### 状态机

服务端渠道使用 `InteractionManager` 的原子状态转换：

```text
pending -> claimed -> completed
                   \-> pending      (投递失败，可重试)
pending/claimed -> expired          (超时、会话取消或终止)
```

- `Claim` 保证按钮、文字回复和超时处理只有一个投递者。
- 只有 Worker 原生响应出口返回 nil 才能 `CompleteClaimed`。
- 投递失败必须 `Release`，并向用户显示可重试状态。
- 超时处理也必须先 claim，避免与用户点击并发双投递。

WebChat 不在 Gateway 中复制平台级 pending map，但 UI 状态遵循相同语义：

```text
pending -> submitting -> resolved/rejected
                     \-> failed -> submitting
```

`resolved/rejected` 只能由 Gateway 的正向确认触发，不能由 WebSocket `send()` 成功触发。

### Gateway 响应语义

两种现有输入形式都继续支持：

- 服务端渠道：`Input` 事件中的 `metadata.permission_response/question_response/elicitation_response`。
- WebChat：显式 `PermissionResponse`、`QuestionResponse`、`ElicitationResponse` 事件。

Gateway 对显式 WebChat 响应：

1. 规范化为统一 metadata。
2. 调用当前 Worker 的 `Input`。
3. 成功后把同类型响应事件回送到该 session，作为带 request ID 的权威 ACK。
4. 失败时发送带 `metadata.interaction_error.request_id` 的 Error。

响应 ACK 是 Gateway 到浏览器的单向事件，不会重新进入 Handler，因此不会形成循环。

### 渠道适配

#### 飞书

- 回调先 claim，再使用不超过两秒的上下文同步投递。
- 投递成功后完成请求并返回 resolved raw card。
- 投递失败后 release，并返回保留操作按钮的 retry raw card。
- 遇到 Worker Error 且消息流卡已创建时，把错误正文写入同一张卡并终结；只有卡片未创建或原位更新失败时才发送静态错误回退。
- Turn Summary 仍是独立卡片。

#### Slack

- 先立即 ACK Socket Mode 请求，满足 Slack 回调时限。
- 随后 claim 并使用有界上下文同步投递给 Worker。
- 成功才移除按钮/输入框并渲染 resolved 状态。
- 失败时 release，保留原操作并追加简短失败提示。
- 文字 fallback 使用相同 claim/deliver/complete-or-release 流程。
- 多问题回答按问题 ID 收集并附带 `question_order`，不再折叠为 `_`。

#### WebChat

- 点击后仅进入 `submitting`。
- 收到同类型响应 ACK 且 request ID 匹配后进入 `resolved/rejected`。
- 收到关联 Error、发送异常、连接断开或客户端确认超时后进入 `failed`，允许重试。
- ACK 或 Error 只能更新 request ID 精确匹配的交互，禁止用“任意 submitting 项”兜底，避免并发交互串扰。

## Worker 原生协议

所有内置 Worker 必须显式满足 `base.MetadataHandler`，并遵循“原生出口成功后才删除 pending 映射”的约束。

| Worker | Permission | Question | Elicitation |
|---|---|---|---|
| Claude Code | stdin control response | stdin control response | stdin control response |
| OpenCode Server | `/permission/{id}/reply` | `/question/{id}/reply` | `/elicitation/{id}/reply` |
| ACP | `RespondRequest` permission outcome | `RespondRequest` answers | `RespondRequest` action/content |
| Codex CLI | JSON-RPC method-specific response | JSON-RPC method-specific response | JSON-RPC action/content |

### Codex JSON-RPC ID

- 解析帧时按 `id` 字段是否存在区分 request、response 和 notification。
- 原样保存 server request ID，支持整数、字符串和 `0`。
- HotPlex 发起的 client request 仍使用内部整数 ID；收到 response 时只允许能解析为整数且能匹配 pending call 的 ID。
- server response 必须原样回写收到的 ID。

### Codex 方法编解码

| 方法 | AEP 类型 | 成功响应 |
|---|---|---|
| `item/commandExecution/requestApproval` | permission | `decision: accept/decline` |
| `item/fileChange/requestApproval` | permission | `decision: accept/decline` |
| `execCommandApproval` / `applyPatchApproval` / legacy approval | permission | `decision: approved/denied` |
| `item/permissions/requestApproval` | permission | `permissions` 授权子集或空集 |
| `item/tool/requestUserInput` | question | 每个问题 ID 对应 `answers: []` |
| `mcpServer/elicitation/request` | elicitation | `action` 和 `content` |

方法、原始 params 和 JSON-RPC ID 在响应成功前保留。`serverRequest/resolved`、turn 完成/中断或会话终止负责清理残留映射。

## 测试与验收

### 共享契约测试

- `InteractionManager`：claim、release、complete、超时竞争、重复响应、取消。
- Gateway：三种 metadata 响应和三种显式响应的成功/失败；WebChat 正向 ACK 与关联 Error。
- `base.DispatchMetadata`：三类 schema、问题顺序、非法结构。

### 36 条组合主链

使用确定性的进程内端到端 harness：

1. 渠道适配器生成或接收交互。
2. 响应经 Gateway 统一 metadata。
3. 真实 Worker adapter 调用受控的原生 fake（stdin、HTTP、JSON-RPC 或 ACP client）。
4. 断言原生请求内容、平台最终状态和 pending 生命周期。
5. 为每个组合验证 Worker 后续事件仍能返回原渠道。

测试表必须显式枚举三个渠道、四个 Worker 和三类交互；禁止只用三个独立单元测试推断 36 条组合全部成立。

### 分支测试

- permission allow/deny；elicitation accept/decline/cancel。
- 单问题、多问题、自定义答案和问题顺序。
- Worker 无对应 pending request、原生写入/HTTP/JSON-RPC 失败后重试成功。
- 回调超时、用户超时、重复点击/回复、非发起人响应。
- Worker 在响应前退出；session reset/GC/terminate 清理交互。
- 飞书占位卡遇到 Error 时原位终结，不产生第二张消息卡。
- Codex server request ID 为 `0`、非零整数和字符串。

### 质量门禁

- 目标包 `go test -count=1 -race`，单模块不超过五秒。
- WebChat 对应测试和 TypeScript 类型检查。
- `make check` 完整通过。
- 当前已配置的飞书测试会话和本地 WebChat 各完成至少一次真实授权闭环；Slack 只有在存在明确安全测试频道时才执行外部 smoke test，否则以 Socket Mode/API mock 的完整 E2E 证据为准。

## 可观测性与安全

- 记录 interaction type、request ID、session ID、worker type、platform、状态转换、失败分类和耗时。
- 不记录完整命令、回答、权限 profile 或 elicitation 内容。
- 用户界面只展示安全的失败分类；原始 Worker/Gateway 错误只进入结构化日志。
- 非发起人响应不改变 pending 状态。

## 实施边界

主要修改范围：

- `internal/messaging/interaction.go`
- `internal/messaging/feishu/*`
- `internal/messaging/slack/*`
- `internal/gateway/handler.go`、`internal/gateway/conn.go`
- `internal/worker/base/metadata.go`
- `internal/worker/{opencodeserver,claudecode,codexcli,acp}`
- `webchat/lib/adapters/hotplex-runtime-adapter.ts`
- `webchat/lib/ai-sdk-transport/client/*`
- 相应 Go/TypeScript 测试与跨组合 E2E harness

不做与交互闭环无关的卡片视觉重构、Worker 生命周期重写或持久化队列建设。
