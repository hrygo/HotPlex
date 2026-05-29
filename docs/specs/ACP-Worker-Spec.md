---
type: spec
tags:
  - project/HotPlex
  - worker/acp
  - protocol/aep
  - protocol/acp
  - agent/hermes
date: 2026-05-29
status: proposed
progress: 0
estimated_hours: 24
supersedes:
  - Worker-ACPX-Spec.md
  - AEP-ACP-Extension-Spec.md
---

# ACP Worker 集成规格

> 本文档定义 HotPlex 通过 **ACP (Agent Client Protocol) v1** 对接任意 ACP 兼容 Agent 的完整方案。
> Hermes Agent 作为第一个试点实现。
>
> 设计原则：**直连 stdio**、**通用 WorkerType "acp"**、**AEP 向后兼容一步到位扩展**。

---

## 1. 动机

### 1.1 现状

HotPlex 支持 3 种 Worker，各自使用私有协议：

| Worker | Transport | Protocol | 支持 Agent 数 |
|--------|-----------|----------|-------------|
| ClaudeCode | stdio | Claude stream-json | 1 |
| CodexCLI | stdio / HTTP | Codex exec / app-server | 1 |
| OpenCodeServer | HTTP+SSE | SSE/JSON | 1 |

每种 Worker 需要独立的协议解析器、事件映射器、测试套件。新 Agent 接入成本高。

### 1.2 目标

ACP (Agent Client Protocol) 是一个开放协议（JSON-RPC 2.0 over stdio），已被 Claude Code、Codex CLI、OpenCode、Hermes 等 Agent 实现。HotPlex 实现一个通用 ACP Worker 后：

1. **一次实现，多 Agent 接入** — 换一个 `acp.command` 即可连接不同 Agent
2. **AEP 向后兼容扩展** — 新增 3 个 Kind + 扩展字段，现有 Worker/Client 不受影响
3. **Hermes 先行试点** — 验证完整链路后再推广

### 1.3 关联文档

- `docs/specs/ACP-Worker-Spec.md` — 原始文档（已废弃并删除）
- `~/.hermes/hermes-agent/acp_adapter/` — Hermes ACP 实现参考

---

## 2. 架构概览

### 2.1 整体架构

```
                         HotPlex Gateway (Go)
                              │
                    ┌─────────┴─────────┐
                    │    ACP Worker      │
                    │  (internal/worker/ │
                    │       acp/)        │
                    └────┬────┬────┬─────┘
                         │    │    │
                   codec.go client.go mapper.go
                         │    │    │
                    ┌────┴────┴────┴─────┐
                    │  stdio pipe (NDJSON) │
                    └────────┬────────────┘
                             │
                    ┌────────┴────────────┐
                    │  ACP Agent 进程      │
                    │  (hermes-acp /       │
                    │   claude --acp /     │
                    │   codex --acp / ...)  │
                    └─────────────────────┘
```

### 2.2 Transport × Protocol × Lifecycle

| 维度 | ACP Worker |
|------|-----------|
| **Transport** | stdio（stdin/stdout pipe） |
| **Protocol** | ACP v1（JSON-RPC 2.0 over NDJSON） |
| **Lifecycle** | 持久进程，多轮复用 |
| **WorkerType** | `"acp"` |

### 2.3 与现有 Worker 对比

| Worker | Transport | Protocol | 支持 Agent 数 | 新 Agent 接入 |
|--------|-----------|----------|-------------|-------------|
| ClaudeCode | stdio | stream-json | 1 | 写新 Worker |
| CodexCLI | stdio/HTTP | exec/app-server | 1 | 写新 Worker |
| OpenCodeServer | HTTP+SSE | SSE/JSON | 1 | 写新 Worker |
| **ACP** | **stdio** | **JSON-RPC 2.0（通用）** | **无限** | **改配置** |

---

## 3. AEP 协议扩展

> 向后兼容扩展。版本号保持 `aep/v1`，所有新增字段 `omitempty`。

### 3.1 新增 Kind 常量

**文件**: `pkg/events/events.go`

```go
const (
	// ... 现有 27 种 ...

	// ACP 扩展 — 任何 ACP 兼容 Agent 可使用
	ToolUpdate  Kind = "tool_update"  // 工具调用中间状态（ACP tool_call_update）
	Plan        Kind = "plan"         // 计划/待办更新（ACP AgentPlanUpdate）
	ModeUpdate  Kind = "mode_update"  // Agent 模式切换（ACP CurrentModeUpdate）
)
```

### 3.2 新增 Data 结构

```go
// ToolUpdateData 工具调用中间状态更新。
// 表达 ACP tool_call_update 的中间状态：pending → in_progress。
// 现有 Worker（ClaudeCode/Codex/OCS）不使用此事件。
type ToolUpdateData struct {
	ID        string    `json:"id"`
	Status    string    `json:"status"`              // pending / in_progress
	Content   any       `json:"content,omitempty"`
	Diff      *FileDiff `json:"diff,omitempty"`
	RawOutput string    `json:"raw_output,omitempty"`
}

// PlanData 计划/待办更新。
type PlanData struct {
	Items []PlanItem `json:"items"`
}

// PlanItem 计划中的单个项目。
// 映射自 ACP PlanEntry：content（描述）、priority（优先级）、status（状态）。
type PlanItem struct {
	Content  string `json:"content"`                   // 任务描述（ACP PlanEntry.content）
	Priority string `json:"priority"`                  // high / medium / low（ACP PlanEntryPriority）
	Status   string `json:"status"`                    // pending / in_progress / completed（ACP PlanEntryStatus）
}

// ModeUpdateData Agent 执行模式切换通知。
// 映射自 ACP CurrentModeUpdate：currentModeId 引用 session/new 返回的 modes 中的 mode ID。
type ModeUpdateData struct {
	CurrentModeID string `json:"current_mode_id"`           // mode ID（ACP CurrentModeUpdate.currentModeId）
}
```

### 3.3 新增共享结构

```go
// FileLocation 文件位置引用。
type FileLocation struct {
	Path string `json:"path"`
	Line int    `json:"line,omitempty"`
}

// FileDiff 文件编辑差异（unified diff 的结构化表达）。
type FileDiff struct {
	Path    string `json:"path"`
	OldText string `json:"old_text"`
	NewText string `json:"new_text"`
}
```

### 3.4 扩展现有 Data 结构

所有新增字段 `omitempty`，旧 Worker 不受影响。

#### ToolCallData 扩展

```go
type ToolCallData struct {
	ID    string         `json:"id"`
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
	// ACP 扩展字段
	Title     string         `json:"title,omitempty"`      // "read: main.go"
	Kind      string         `json:"kind,omitempty"`        // read / edit / delete / move / search / execute / think / fetch / switch_mode / other
	Locations []FileLocation `json:"locations,omitempty"`
}
```

#### ToolResultData 扩展

```go
type ToolResultData struct {
	ID     string `json:"id"`
	Output any    `json:"output"`
	Error  string `json:"error,omitempty"`
	// ACP 扩展字段
	Status string    `json:"status,omitempty"` // completed / failed
	Diff   *FileDiff `json:"diff,omitempty"`
}
```

### 3.5 DoneData.Stats 约定 key

`DoneData.Stats` 已是 `map[string]any`，ACP 扩展新增以下约定 key：

| Key | 类型 | 来源 | 说明 |
|-----|------|------|------|
| `input_tokens` | int | ACP PromptUsage.inputTokens | 输入 token |
| `output_tokens` | int | ACP PromptUsage.outputTokens | 输出 token |
| `thought_tokens` | int | ACP PromptUsage.thoughtTokens | 推理 token |
| `cached_read_tokens` | int | ACP PromptUsage.cachedReadTokens | 缓存读取 token |
| `cached_write_tokens` | int | ACP PromptUsage.cachedWriteTokens | 缓存写入 token |
| `total_tokens` | int | ACP PromptUsage.totalTokens | 总 token |
| `stop_reason` | string | ACP PromptResult.stopReason | end_turn / cancelled / max_tokens / max_turn_requests / refusal |
| `cost` | object | ACP UsageUpdate.cost | `{amount: float, currency: string}` |
| `context_size` | int | ACP UsageUpdate.size | 上下文窗口总大小 |
| `context_used` | int | ACP UsageUpdate.used | 已使用上下文大小 |

### 3.6 Raw 透传约定

以下低频 ACP 事件通过现有 `Raw` Kind 透传：

| ACP 事件 | Raw.Kind 值 | 说明 |
|----------|------------|------|
| `config_option_update` | `"acp.config_option_update"` | 配置变更 |
| `available_commands_update` | `"acp.available_commands_update"` | 可用命令列表 |
| `session_info_update` | `"acp.session_info_update"` | 会话元信息 |

Client 通过 `data.kind` 前缀 `acp.` 识别为 ACP 透传事件。

### 3.7 Bridge 侧处理

`internal/gateway/bridge_forward.go` 的 `processForwardedEvent()` 对新增 Kind 无需特殊处理——走统一管线（clone → inject sessionID → forward to hub）。

- `tool_update`：透传，统计已在 `tool_call` 累计
- `plan`：透传，纯 UI 事件
- `mode_update`：透传，纯 UI 事件
- 扩展字段（Title, Kind, Status, Locations, Diff）随 Envelope 透传

### 3.8 向后兼容性

| 场景 | 影响 |
|------|------|
| 旧 Client 连接新 Gateway | ✅ 新 Kind 被旧 Client 忽略 |
| 新 Client 连接旧 Gateway | ✅ 新 Data 字段在旧 Worker 中为 omitempty |
| 旧 Worker 连接新 Gateway | ✅ 旧 Worker 不发新事件 |
| 新 Worker（ACP）连接新 Gateway | ✅ 全部事件可用 |
| Event Store 持久化 | ✅ json.RawMessage 自动序列化 |
| AEP 版本号 | **保持 `aep/v1`** |

---

## 4. ACP JSON-RPC 协议层

### 4.1 Transport

```
HotPlex Gateway (Go)
    │
    ├── stdin pipe  ──→  ACP Agent 进程（JSON-RPC Request）
    ├── stdout pipe ←──  ACP Agent 进程（JSON-RPC Response + Notification）
    └── stderr pipe ←──  ACP Agent 进程（日志，重定向到 slog）
```

格式：NDJSON（每行一个 JSON-RPC 2.0 对象，`\n` 分隔）。stdout 专用于 JSON-RPC，stderr 用于日志。

### 4.2 codec.go — NDJSON 编解码

核心职责：
- 从 stdout 读取一行，解析为 `json.RawMessage`，分发为 Request/Response/Notification
- 序列化 JSON-RPC 消息，追加 `\n`，写入 stdin
- `json.RawMessage` 零拷贝传递，不强制解析 params

类型定义：

```go
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"` // 固定 "2.0"
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

type JSONRPCNotification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type JSONRPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}
```

分发逻辑：

```go
func ReadMessage(reader *bufio.Reader) (any, error) {
    line, err := reader.ReadBytes('\n')
    // ... 解析 JSON，根据 "id" 字段有无分发 ...
    // 有 "id" + 有 "method" → Request
    // 有 "id" + 有 "result" 或 "error" → Response
    // 无 "id" + 有 "method" → Notification
}
```

### 4.3 client.go — ACP Client

封装完整的 ACP 握手和会话生命周期。

| 方法 | ACP Method | 说明 |
|------|-----------|------|
| `Initialize()` | `initialize` | 版本协商、能力交换（params 含 `protocolVersion`、`clientCapabilities`、`clientInfo`） |
| `NewSession(cwd, mcpServers)` | `session/new` | 创建会话，返回 sessionId + models + modes + configOptions |
| `LoadSession(sessionID, cwd, mcpServers)` | `session/load` | 恢复已有会话（Agent 回放历史） |
| `ResumeSession(sessionID, cwd, mcpServers)` | `session/resume` | 恢复中断的会话 |
| `Prompt(sessionID, content)` | `session/prompt` | 发送 prompt |
| `Cancel(sessionID)` | `session/cancel` | 取消当前 turn |
| `RespondPermission(reqID, outcome)` | `session/request_permission` response | 回复权限请求（AllowedOutcome 或 DeniedOutcome） |
| `SetSessionModel(sessionID, modelID)` | `session/set_model` | 切换模型（P2） |
| `SetSessionMode(sessionID, modeID)` | `session/set_mode` | 切换模式（P2） |
| `ForkSession(sessionID)` | `session/fork` | 分叉会话（P2） |
| `ListSessions()` | `session/list` | 列出会话（P2） |

内部状态：

```go
type ACPClient struct {
    stdin          io.Writer
    stdout         *bufio.Reader
    nextID         int64
    pending        map[string]chan *JSONRPCResponse // id → response channel
    pendingMu      sync.Mutex
    NotificationCh chan *JSONRPCNotification        // session/update 通知
    done           chan struct{}
}
```

读循环 goroutine 持续从 stdout 读取，根据 `id` 字段分发：
- 有 `id` 且包含 `result` 或 `error` → 匹配 pending channel
- 无 `id` → 投递到 `NotificationCh`

### 4.4 握手序列

```
Gateway ──→ Agent : {"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1,"clientCapabilities":{},"clientInfo":{"name":"hotplex","version":"..."}}}
Agent  ──→ Gateway : {"jsonrpc":"2.0","id":1,"result":{"protocolVersion":1,"agentInfo":{"name":"hermes-agent","version":"0.15.1"},"agentCapabilities":{...},...}}
Gateway ──→ Agent : {"jsonrpc":"2.0","id":2,"method":"session/new","params":{"cwd":"/project","mcpServers":[]}}
Agent  ──→ Gateway : {"jsonrpc":"2.0","id":2,"result":{"sessionId":"uuid","models":{...},"modes":{...},"configOptions":[...]}}
```

### 4.5 Prompt 流程

```
Gateway ──→ Agent : {"jsonrpc":"2.0","id":3,"method":"session/prompt","params":{"sessionId":"uuid","prompt":[{"type":"text","text":"hello"}]}}
Agent  ──→ Gateway : {"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"uuid","update":{"sessionUpdate":"agent_thought_chunk","content":{"type":"text","text":"..."}}}}
Agent  ──→ Gateway : {"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"uuid","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"..."}}}}
Agent  ──→ Gateway : {"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"uuid","update":{"sessionUpdate":"tool_call","toolCallId":"tc-xxx",...}}}
Agent  ──→ Gateway : {"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"uuid","update":{"sessionUpdate":"tool_call_update","toolCallId":"tc-xxx","status":"completed",...}}}
Agent  ──→ Gateway : {"jsonrpc":"2.0","id":3,"result":{"stopReason":"end_turn","usage":{"inputTokens":1000,"outputTokens":500,...}}}
```

### 4.6 权限请求流程

```
Agent  ──→ Gateway : {"jsonrpc":"2.0","id":N,"method":"session/request_permission","params":{"sessionId":"uuid","toolCall":{...},"options":[{"optionId":"opt-1","name":"Allow Once","kind":"allow_once"},{"optionId":"opt-2","name":"Deny","kind":"reject_once"}]}}
                                          ↓
                     AEP permission_request → Client（Mapper 将多选项模型简化为 allow/deny）
                                          ↓
                     Client 回复 permission_response
                                          ↓
Gateway ──→ Agent : {"jsonrpc":"2.0","id":N,"result":{"outcome":{"outcome":"selected","optionId":"opt-1"}}}
```

> **注意**：ACP 权限模型是多选项（`PermissionOption[]`，每项有 `optionId`/`name`/`kind`），
> 而 AEP 是布尔模型（`Allowed bool`）。Mapper 负责转换：
> - ACP → AEP：选择第一个 `kind=allow_*` 选项的 `name` 作为 Description
> - AEP → ACP：`Allowed=true` 选择第一个 `kind=allow_once` 选项的 `optionId`；`Allowed=false` 返回 `{"outcome":"cancelled"}`

---

## 5. ACP ↔ AEP 事件映射

### 5.1 mapper.go — 双向映射器

核心函数 `MapNotification(notif *JSONRPCNotification) []*events.Envelope`，将一个 ACP `session/update` 映射为零或多个 AEP Envelope。

Mapper 维护最少状态：

```go
type ACPMapper struct {
    msgActive  bool  // 当前是否有活跃的 message 流
    turnActive bool  // 当前 turn 是否在运行
}
```

每个 `session/prompt` 发送时重置。

### 5.2 完整映射表

| ACP sessionUpdate | AEP Kind | Data 结构 | 映射说明 |
|---|---|---|---|
| `user_message_chunk` | 忽略 | — | 用户输入回显，Gateway 已有原始输入，无需映射 |
| `agent_message_chunk` | `message.start`（首个）+ `message.delta` | `MessageDeltaData` | 首个 chunk 时合成 `message.start`，Content.Text → Text |
| `agent_thought_chunk` | `reasoning` | `ReasoningData` | Content.Text → Text |
| `tool_call` | `tool_call` | `ToolCallData`（含扩展字段） | 映射 title/kind/locations/rawInput（**不含 status**） |
| `tool_call_update`（中间） | `tool_update` | `ToolUpdateData` | status=pending/in_progress |
| `tool_call_update`（完成） | `tool_result` | `ToolResultData`（含扩展字段） | status=completed/failed，含 diff |
| `usage_update` | `context_usage`（内部） | 内部跟踪 | 提取 size/used/cost，不生成外部 Envelope |
| `plan` | `plan` | `PlanData` | 映射 PlanEntry.content/priority/status |
| `current_mode_update` | `mode_update` | `ModeUpdateData` | 映射 currentModeId（引用 session/new 返回的 modes） |
| `available_commands_update` | `raw` | `RawData{Kind:"acp.available_commands_update"}` | 透传 |
| `config_option_update` | `raw` | `RawData{Kind:"acp.config_option_update"}` | 透传 |
| `session_info_update` | `raw` | `RawData{Kind:"acp.session_info_update"}` | 透传 |
| `request_permission` | `permission_request` | `PermissionRequestData` | 多选项→布尔转换（见 §4.6 注） |
| Prompt Response (success) | `message.end` + `done` | `DoneData{Success:true, Stats}` | 合成 message.end + done |
| Prompt Response (error) | `error` + `done` | `ErrorData` + `DoneData{Success:false}` | JSON-RPC Error |

### 5.3 Prompt Response → AEP Done 映射

| ACP stopReason | AEP DoneData.Success | 说明 |
|---|---|---|
| `end_turn` | `true` | 正常完成 |
| `cancelled` | `true` | 用户取消，非错误 |
| `max_tokens` | `false` | 上下文耗尽 |
| `max_turn_requests` | `false` | 达到最大 turn 数 |
| `refusal` | `false` | 模型拒绝 |
| JSON-RPC Error | `false` + ErrorData | 协议级错误 |

### 5.4 合成事件

ACP 没有显式的 `message.start` / `message.end` / `state` 事件，Mapper 负责合成：

| AEP 事件 | 合成时机 | 条件 |
|----------|---------|------|
| `state(running)` | 发送 `session/prompt` 时 | turnActive=false → true |
| `message.start` | 收到第一个 `agent_message_chunk` | msgActive=false → true |
| `message.end` | Prompt Response 到达 | msgActive=true → false |
| `state(idle)` | Prompt Response 到达后 | turnActive=true → false |

### 5.5 ACP tool_call 字段 → AEP ToolCallData 映射

| ACP 字段 | AEP ToolCallData 字段 | 说明 |
|----------|---------------------|------|
| `toolCallId` | `ID` | 工具调用 ID |
| `title` | `Title`（扩展） | 人类可读标题 |
| `kind` | `Kind`（扩展） | read/edit/delete/move/search/execute/think/fetch/switch_mode/other |
| `rawInput` | `Input` | 工具参数 |
| `content[].path` | `Locations`（扩展） | 文件位置 |
| `_meta.claudeCode.toolName` | `Name` | 回退到 meta 中的工具名 |

> **注意**：`tool_call`（ToolCallStart）**不含 `status` 字段**。Status 仅在 `tool_call_update`（ToolCallProgress）中出现。

### 5.6 ACP tool_call_update 字段 → AEP 映射

| 条件 | AEP Kind | 说明 |
|------|---------|------|
| status 为 `pending` 或 `in_progress` | `tool_update` | 中间状态 |
| status 为 `completed` 或 `failed` | `tool_result` | 最终结果 |
| status 为空 + 有 rawOutput | `tool_result` | 兼容不带 status 的完成 |

---

## 6. ACP Worker 实现

### 6.1 目录结构

```
internal/worker/acp/
├── codec.go      # NDJSON 行读写、JSON-RPC 2.0 消息解析
├── client.go     # ACP Client：握手、会话管理、prompt 发送、通知接收
├── mapper.go     # ACP session/update → AEP Envelope 双向映射
├── worker.go     # Worker 接口实现 + TypeACP 常量 + init() 注册
├── worker_test.go
├── mapper_test.go
└── codec_test.go
```

### 6.2 Worker 结构

```go
type Worker struct {
	*base.BaseWorker                      // 嵌入（提供 Proc/Terminate/Kill/Wait/Health/Conn）
	mu               sync.Mutex

	client           *ACPClient
	mapper           *ACPMapper

	acpSessionID     string               // ACP 会话 ID
	command          string               // 可执行文件路径（如 "hermes-acp"）
	args             []string
	cwd              string
	autoApprove      bool                 // 自动批准权限请求
}
```

> **设计说明**：
> - 使用 `*base.BaseWorker` 嵌入（与 ClaudeCode/CodexCLI/OCS 一致），复用 `Proc`、`Terminate`、`Kill`、`Wait` 等方法
> - 发送 Envelope 通过 `w.BaseWorker.Conn.TrySend(env)` — 与现有 Worker 一致（非 `base.Send()`）
> - ACP Worker 需重写 `Terminate()`：先 `client.Cancel()` 再调用 `base.BaseWorker.Terminate()`
> - 需实现 `Health(worker.WorkerType)` 包装（BaseWorker 的 Health 需要 type 参数）
> - 建议实现 `worker.WorkerSessionIDHandler`（`SetWorkerSessionID`/`GetWorkerSessionID`）以便 Bridge 自动持久化 ACP session ID

### 6.3 生命周期

| 阶段 | Worker 方法 | 内部动作 |
|------|------------|---------|
| 启动（新会话） | `Start(ctx, session)` | 1. `base.Proc.Start(command, args)` 启动进程<br>2. `client.Initialize()` 握手<br>3. `client.NewSession(cwd, mcpServers)` 创建会话<br>4. 记录 `acpSessionID`<br>5. 启动 `readLoop` goroutine |
| 启动（恢复会话） | `Start(ctx, session)` | 1. 启动进程<br>2. `client.Initialize()` 握手<br>3. `client.LoadSession(acpSessionID, cwd, mcpServers)` 恢复会话（Agent 回放历史） |
| 输入 | `Input(ctx, content, metadata)` | 构造 `session/prompt` 请求，`client.Prompt()` |
| 输出 | `readLoop`（后台 goroutine） | 从 `client.NotificationCh` 读取 → `mapper.MapNotification()` → `Conn.TrySend()` |
| 权限回复 | `Send(ctx, envelope)` | 收到 `permission_response` → Mapper 将布尔转换回 ACP 选项 → `client.RespondPermission()` |
| 终止 | `Terminate(ctx)` | 1. `client.Cancel()` 优雅取消当前 turn<br>2. `base.BaseWorker.Terminate(ctx)` → `base.Proc.Terminate()`（SIGTERM → 5s → SIGKILL） |

### 6.4 readLoop

```go
func (w *Worker) readLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case notif, ok := <-w.client.NotificationCh:
			if !ok {
				return
			}
			envelopes := w.mapper.MapNotification(notif)
			for _, env := range envelopes {
				w.Conn.TrySend(env) // 背压：TrySend 对 delta 类事件可丢弃
			}
		}
	}
}
```

### 6.5 配置扩展

`internal/config/config.go` 新增：

```go
type ACPConfig struct {
    Command      string   `json:"command" yaml:"command"`            // ACP agent 可执行文件
    Args         []string `json:"args,omitempty" yaml:"args"`        // 额外参数
    AutoApprove  bool     `json:"auto_approve,omitempty" yaml:"auto_approve"` // 自动批准权限
    SessionStore string   `json:"session_store,omitempty" yaml:"session_store"` // 会话存储目录
}
```

YAML 配置示例：

```yaml
# Worker 类型按平台配置（与现有 Worker 一致）
messaging:
  slack:
    worker_type: acp
  feishu:
    worker_type: acp

# ACP 特定配置
worker:
  acp:
    command: hermes-acp
    auto_approve: true
```

### 6.6 注册

```go
// internal/worker/acp/worker.go — 注册（与现有 Worker 一致，放在 worker.go 的 init() 中）
const TypeACP worker.WorkerType = "acp" // 新常量，与 TypeACPX = "acpx" 共存

func init() {
	worker.Register(TypeACP, func() (worker.Worker, error) {
		return &Worker{}, nil
	})
}
```

---

## 7. 错误处理

### 7.1 进程启动失败

| 场景 | 处理 |
|------|------|
| 可执行文件不存在 | `Start` 返回 error（含 "not found in PATH"） |
| 权限不足 | `Start` 返回 error（含原始错误） |
| 握手超时（30s） | `Start` 返回 `context.DeadlineExceeded` |

### 7.2 运行时错误

| 场景 | 处理 |
|------|------|
| 进程意外退出 | `readLoop` 检测到 stdout EOF → 发送 `error` + `done{false}` |
| JSON-RPC Error 响应 | Mapper 转换为 AEP `error` 事件 |
| 权限请求超时（60s） | 自动拒绝（deny） |

### 7.3 终止

三层终止机制（复用 `proc.Manager`）：

1. `client.Cancel()` 优雅取消当前 turn
2. SIGTERM → 等待 5s
3. SIGKILL 强制终止

---

## 8. 源码改动清单

### 8.1 AEP 扩展（pkg/events/）

| 文件 | 改动 | 行数估算 |
|------|------|---------|
| `pkg/events/events.go` | 新增 3 个 Kind 常量 + 5 个 Data 结构 + 扩展 2 个 Data 结构 + 2 个共享结构 | ~100 行 |

### 8.2 ACP Worker（internal/worker/acp/）

| 文件 | 改动 | 行数估算 |
|------|------|---------|
| `internal/worker/acp/codec.go` | NDJSON 编解码 + JSON-RPC 类型 | ~150 行 |
| `internal/worker/acp/client.go` | ACP Client（握手/会话/prompt/通知） | ~300 行 |
| `internal/worker/acp/mapper.go` | ACP ↔ AEP 事件映射 | ~350 行 |
| `internal/worker/acp/worker.go` | Worker 接口实现 + TypeACP + init() 注册 | ~200 行 |
| `internal/worker/acp/codec_test.go` | 编解码测试 | ~100 行 |
| `internal/worker/acp/mapper_test.go` | 映射测试 | ~200 行 |
| `internal/worker/acp/worker_test.go` | Worker 集成测试 | ~150 行 |
| **总计** | | **~1450 行** |

### 8.3 配置扩展

| 文件 | 改动 | 行数估算 |
|------|------|---------|
| `internal/config/config.go` | 新增 ACPConfig 结构 | ~15 行 |

### 8.4 无需改动

| 文件/模块 | 说明 |
|----------|------|
| `internal/gateway/bridge_forward.go` | 新增 Kind 走统一管线 |
| `pkg/aep/codec.go` | Envelope 编解码不变 |
| `internal/eventstore/` | json.RawMessage 自动序列化 |
| 现有 Worker（ClaudeCode/Codex/OCS） | 不受影响 |

---

## 9. 实施优先级

### P0（必须，MVP）

- AEP 扩展：3 个新 Kind + 5 个 Data 结构 + 2 个共享结构 + 扩展 2 个现有结构
- codec.go：NDJSON 编解码 + JSON-RPC 类型
- client.go：Initialize + NewSession + Prompt + 通知接收
- mapper.go：完整映射表（所有 session/update 类型）
- worker.go：Start/Input/Terminate
- init.go：注册

### P1（重要）

- 会话恢复（LoadSession）
- 权限请求桥接（request_permission ↔ permission_request/response）
- auto_approve 配置支持
- usage stats 完整映射

### P2（增强）

- SetSessionModel / SetSessionMode
- 会话 fork
- 会话 list
- MCP server 注册（session/new 的 mcpServers 参数）

---

## 10. 验收标准

### AC-ACP-001 — AEP 扩展 Kind 注册

- Given `events.go` 新增 `ToolUpdate` / `Plan` / `ModeUpdate`
- When `Kind("tool_update").String()` 被调用
- Then 返回 `"tool_update"`

### AC-ACP-002 — AEP 扩展字段 omitempty

- Given `ToolCallData{ID:"c1", Name:"read", Input:map[string]any{"path":"main.go"}}`
- When `json.Marshal`
- Then 输出不包含 `title` / `kind` / `status` / `locations` 字段

### AC-ACP-003 — Codec NDJSON 编解码

- Given 一行 `{"jsonrpc":"2.0","id":1,"result":{"sessionId":"abc"}}`
- When `ReadMessage`
- Then 返回 `JSONRPCResponse`，`ID` 为 `1`，`Result` 包含 `sessionId`

### AC-ACP-004 — ACP 握手成功

- Given hermes-acp 在 PATH 中
- When `Worker.Start` 被调用
- Then `initialize` → `session/new` 握手完成
- And `acpSessionID` 被记录

### AC-ACP-005 — ACP 事件 → AEP 映射正确

- Given `session/update` 通知，`sessionUpdate` 为 `agent_message_chunk`
- When `mapper.MapNotification`
- Then 返回 `message.start`（首个）+ `message.delta` Envelope

### AC-ACP-006 — ACP Prompt Response → AEP Done

- Given `session/prompt` Response，`stopReason` 为 `end_turn`
- When Mapper 处理
- Then 生成 `message.end` + `done{Success:true}` + Stats

### AC-ACP-007 — Bridge 透传新事件

- Given ACP Worker 发出 Kind=`tool_update` 的 Envelope
- When `processForwardedEvent` 处理
- Then 事件被正常 clone、注入 sessionID、转发给 hub

### AC-ACP-008 — 旧 Worker 不受影响

- Given ClaudeCode Worker 发出 `ToolCallData{ID, Name, Input}`
- When Bridge 转发给 Client
- Then Client 收到的 JSON 不包含新增字段

### AC-ACP-009 — Worker 注册

- Given `worker.NewWorker("acp")`
- When 调用
- Then 返回 ACP Worker 实例

### AC-ACP-010 — hermes-acp 不在 PATH 时报错

- Given `exec.LookPath("hermes-acp")` 失败
- When `Worker.Start`
- Then 返回 error，包含 "not found"

### AC-ACP-011 — 分层终止

- Given ACP Worker 运行中
- When `Terminate` 被调用
- Then 发送 `session/cancel` → SIGTERM → 5s → SIGKILL

### AC-ACP-012 — 权限请求桥接

- Given ACP Agent 发送 `request_permission`
- When Mapper 收到
- Then 生成 AEP `permission_request`
- And Client 回复 `permission_response`
- Then `client.RespondPermission()` 被调用

---

## 11. Hermes 试点验证计划

### 11.1 前置条件

- `hermes-acp` 二进制在 PATH 中（或配置 `acp.command` 指定路径）
- Hermes 已配置 Provider 和 API Key（`~/.hermes/.env`）

### 11.2 验证步骤

1. **启动测试**：配置 `worker_type: acp` + `acp.command: hermes-acp`，启动 Gateway
2. **基础对话**：通过 WebChat 或 Slack 发送 prompt，验证流式输出
3. **工具调用**：发送需要工具的 prompt（如 "list files"），验证 tool_call + tool_result 映射
4. **会话恢复**：重连后验证上下文保持
5. **权限请求**：发送需要审批的命令，验证权限桥接
6. **终止**：发送取消信号，验证优雅终止

### 11.3 预期结果

- 所有 ACP session/update 事件正确映射为 AEP Envelope
- WebChat / Slack 客户端正确渲染文本流、工具调用、推理内容
- Usage stats 正确统计并展示
- 会话可恢复、可终止
