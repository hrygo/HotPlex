---
title: "AEP 事件类型参考"
weight: 2
description: "所有 AEP v1 事件类型的完整字段参考"
---

# AEP 事件类型参考

> 完整的 AEP v1 事件 Kind 常量、Data 结构体、字段类型和使用说明。

## 概述

所有 AEP 消息共用统一的 `Envelope` 结构（`pkg/events/events.go`）。事件类型通过 `Kind` 常量区分，每种 Kind 对应一个 `Data` 结构体。

```go
type Envelope struct {
    Version   string   `json:"version"`           // 固定 "aep/v1"
    ID        string   `json:"id"`                // "evt_<uuid>"
    Seq       int64    `json:"seq"`               // per-session 单调递增
    Priority  Priority `json:"priority,omitempty"` // "control" | "data"
    SessionID string   `json:"session_id"`        // "sess_<uuid>"
    Timestamp int64    `json:"timestamp"`          // Unix 毫秒
    Event     Event    `json:"event"`
}
```

## Seq 编号规则

- **Per-Session 独立空间**：每个 Session 有独立的 seq 计数器（`internal/gateway/seq.go`）
- **原子递增**：使用 `atomic.Int64` 保证并发安全
- **从 1 开始**：首条消息 seq=1
- **丢弃不消耗 seq**：被 backpressure 丢弃的 `message.delta` 不分配 seq
- **Ping/Pong 无 seq**：心跳消息 seq=0

## Backpressure 机制

| 事件类型 | 丢弃行为 | 说明 |
|---------|---------|------|
| `message.delta` | **可丢弃** | 通道满时静默丢弃，不返回错误 |
| `raw` | **可丢弃** | 同上 |
| `state` / `done` / `error` | **永不丢弃** | 阻塞发送，保证送达 |
| `control` | **直接投递** | 绕过 broadcast 队列 |
| `input.ack` | **直接投递** | control priority；确认持久化与 Worker 投递结果 |

丢弃标记：`done` 事件的 `Data.dropped` 字段为 `true` 时，表示本次 Turn 中有 delta 被丢弃。

## 事件类型总表

### C → S（Client → Server）

#### `init` — 会话初始化

WS 连接后首帧，必须在 30s 内发送。

```go
// Data: 任意 key-value（认证信息、客户端版本等）
```

#### `input` — 用户输入

```go
type InputData struct {
    Content  string         `json:"content"`
    Metadata map[string]any `json:"metadata,omitempty"`
}
```

Envelope `id` 是默认 `client_message_id`。重试必须复用同一 Envelope；相同 ID
和 payload 会返回已有 execution 而不会再次调用 Worker，相同 ID 搭配不同 payload
会返回 `INVALID_MESSAGE`。这里的“重试”指连接中断或 ACK 丢失等结果不明的重发；
已收到 `failed` / `SESSION_BUSY` 后的新尝试应使用新 ID。`unknown` 记录复用原 ID
只查询现状；只有用户接受重复副作用风险时，才应以新 ID 再次执行。

#### `permission_response` — 权限响应

```go
type PermissionResponseData struct {
    ID      string `json:"id"`      // 对应 permission_request 的 ID
    Allowed bool   `json:"allowed"`
    Reason  string `json:"reason,omitempty"`
}
```

#### `question_response` — 问答响应

```go
type QuestionResponseData struct {
    ID      string            `json:"id"`
    Answers map[string]string `json:"answers"` // question → selected label
}
```

#### `elicitation_response` — MCP 用户输入响应

```go
type ElicitationResponseData struct {
    ID      string         `json:"id"`
    Action  string         `json:"action"`  // "accept" | "decline" | "cancel"
    Content map[string]any `json:"content,omitempty"`
}
```

#### `ping` — 心跳

```go
// Data: struct{}{}
// Seq: 0（不分配序号）
// 回复: pong
```

#### `control` — 控制命令

```go
type ControlData struct {
    Action      ControlAction  `json:"action"` // terminate/delete/gc/reset/cd
    Reason      string         `json:"reason,omitempty"`
    DelayMs     int            `json:"delay_ms,omitempty"`
    Recoverable bool           `json:"recoverable,omitempty"`
}
```

### S → C（Server → Client）

#### `input.ack` — 输入持久化与投递确认

```go
type ExecutionStatus string // accepted / delivered / unknown / failed

type InputAckData struct {
    ClientMessageID string          `json:"client_message_id"`
    ExecutionID     string          `json:"execution_id"`
    Status          ExecutionStatus `json:"status"`
    Duplicate       bool            `json:"duplicate,omitempty"`
    ErrorCode       ErrorCode       `json:"error_code,omitempty"`
}
```

新输入通常产生两次 ACK：持久化后的 `accepted`，以及 Worker 调用返回后的
`delivered`、`unknown` 或 `failed`。`unknown` 表示存在重复副作用风险，Gateway
不会自动重投。重复 Envelope 只返回当前记录，并将 `duplicate` 设为 `true`。

#### `state` — 状态变更

```go
type StateData struct {
    State   SessionState `json:"state"` // created/running/idle/terminated
    Message string       `json:"message,omitempty"`
}
```

**时序约束**：Turn 开始时 `state(running)` 必须是第一个 S→C 事件。

#### `message.start` — 消息流开始

```go
type MessageStartData struct {
    ID          string         `json:"id"`
    Role        string         `json:"role"`         // "assistant"
    ContentType string         `json:"content_type"` // "text" 等
    Metadata    map[string]any `json:"metadata,omitempty"`
}
```

#### `message.delta` — 流式内容片段

```go
type MessageDeltaData struct {
    MessageID string `json:"message_id"`
    Content   string `json:"content"`
}
```

**可被 backpressure 丢弃**，丢弃时不消耗 seq。客户端应支持 delta 缺失时的平滑渲染。

#### `message.end` — 消息流结束

```go
type MessageEndData struct {
    MessageID string `json:"message_id"`
}
```

#### `message` — 完整消息（非流式）

```go
type MessageData struct {
    ID          string         `json:"id"`
    Role        string         `json:"role"`
    Content     string         `json:"content"`
    ContentType string         `json:"content_type,omitempty"`
    Metadata    map[string]any `json:"metadata,omitempty"`
}
```

Turn 结束时的完整消息聚合，兼容非流式场景。

#### `tool_call` — 工具调用通知

```go
type ToolCallData struct {
    ID    string         `json:"id"`
    Name  string         `json:"name"`
    Input map[string]any `json:"input"`
    // ACP extension fields — zero breaking change, omitted by existing workers.
    Title     string         `json:"title,omitempty"`      // "read: main.go"
    Kind      string         `json:"kind,omitempty"`       // read/edit/delete/move/search/execute/think/fetch/switch_mode/other
    Locations []FileLocation `json:"locations,omitempty"`  // 文件位置引用
}
```

> ACP 扩展字段由 ACP 兼容 Worker 填充，现有 Worker（ClaudeCode/Codex/OCS）不发送这些字段。

#### `tool_result` — 工具执行结果

```go
type ToolResultData struct {
    ID     string `json:"id"`     // 对应 tool_call.id
    Output any    `json:"output"`
    Error  string `json:"error,omitempty"`
    // ACP extension fields — zero breaking change, omitted by existing workers.
    Status string    `json:"status,omitempty"` // completed / failed
    Diff   *FileDiff `json:"diff,omitempty"`   // 结构化文件编辑
}
```

**匹配规则**：`tool_result.id` 必须与对应的 `tool_call.id` 匹配。

#### `permission_request` — 权限请求

```go
type PermissionRequestData struct {
    ID          string          `json:"id"`
    ToolName    string          `json:"tool_name"`
    Description string          `json:"description,omitempty"`
    Args        []string        `json:"args,omitempty"`
    InputRaw    json.RawMessage `json:"input_raw,omitempty"`
}
```

**超时**：默认 5 分钟自动拒绝（auto-deny），由 `InteractionManager` 管理。

#### `question_request` — 问答请求

```go
type QuestionRequestData struct {
    ID        string     `json:"id"`
    ToolName  string     `json:"tool_name,omitempty"`
    Questions []Question `json:"questions"`
}

type Question struct {
    Question    string           `json:"question"`
    Header      string           `json:"header"`
    Options     []QuestionOption `json:"options"`
    MultiSelect bool             `json:"multi_select"`
}

type QuestionOption struct {
    Label       string `json:"label"`
    Description string `json:"description,omitempty"`
    Preview     string `json:"preview,omitempty"`
}
```

#### `elicitation_request` — MCP 用户输入请求

```go
type ElicitationRequestData struct {
    ID              string         `json:"id"`
    MCPServerName   string         `json:"mcp_server_name"`
    Message         string         `json:"message"`
    Mode            string         `json:"mode,omitempty"`
    URL             string         `json:"url,omitempty"`
    ElicitationID   string         `json:"elicitation_id,omitempty"`
    RequestedSchema map[string]any `json:"requested_schema,omitempty"`
}
```

#### `reasoning` — 思考/推理过程

```go
type ReasoningData struct {
    ID      string `json:"id"`
    Content string `json:"content"`
    Model   string `json:"model,omitempty"`
}
```

#### `step` — 执行步骤标记

```go
type StepData struct {
    ID       string         `json:"id"`
    StepType string         `json:"step_type"`
    Name     string         `json:"name,omitempty"`
    Input    map[string]any `json:"input,omitempty"`
    Output   map[string]any `json:"output,omitempty"`
    ParentID string         `json:"parent_id,omitempty"`
    Duration int64          `json:"duration,omitempty"` // milliseconds
}
```

#### `raw` — Worker 原始事件透传

```go
type RawData struct {
    Kind string `json:"kind"` // 原始事件类型标识
    Raw  any    `json:"raw"`  // 原始事件载荷（透传 Agent 特定消息）
}
```

**可被 backpressure 丢弃**，丢弃时不消耗 seq。用于将 Worker（如 Claude Code）的 Agent 特定事件原样透传给客户端。

#### `tool_update` — 工具调用中间状态（ACP 扩展）

ACP Agent 工具执行过程中的中间状态更新。现有 Worker（ClaudeCode/Codex/OCS）不发送此事件。

```go
type ToolUpdateData struct {
    ID        string    `json:"id"`
    Status    string    `json:"status"`              // pending / in_progress
    Content   any       `json:"content,omitempty"`
    Diff      *FileDiff `json:"diff,omitempty"`
    RawOutput string    `json:"raw_output,omitempty"`
}
```

#### `plan` — 计划/任务列表更新（ACP 扩展）

ACP Agent 的任务计划更新。映射自 ACP `AgentPlanUpdate`。

```go
type PlanData struct {
    Items []PlanItem `json:"items"`
}

type PlanItem struct {
    Content  string `json:"content"`  // 任务描述（ACP PlanEntry.content）
    Priority string `json:"priority"` // high / medium / low
    Status   string `json:"status"`   // pending / in_progress / completed
}
```

#### `mode_update` — Agent 模式切换（ACP 扩展）

ACP Agent 执行模式变更通知。映射自 ACP `CurrentModeUpdate`。

```go
type ModeUpdateData struct {
    Mode string `json:"mode"` // Agent 当前模式标识
}
```

#### ACP 公共结构体

```go
type FileLocation struct {
    Path string `json:"path"`
    Line int    `json:"line,omitempty"`
}

type FileDiff struct {
    Path    string `json:"path"`
    OldText string `json:"old_text"`
    NewText string `json:"new_text"`
}
```

#### `context_usage` — Context Window 使用报告

```go
type ContextUsageData struct {
    TotalTokens int               `json:"total_tokens"`
    MaxTokens   int               `json:"max_tokens"`
    Percentage  int               `json:"percentage"`
    Model       string            `json:"model,omitempty"`
    Categories  []ContextCategory `json:"categories,omitempty"`
    MemoryFiles int               `json:"memory_files,omitempty"`
    MCPTools    int               `json:"mcp_tools,omitempty"`
    Agents      int               `json:"agents,omitempty"`
    Skills      ContextSkillInfo  `json:"skills,omitempty"`
}
```

#### `mcp_status` — MCP 服务器状态

```go
type MCPStatusData struct {
    Servers []MCPServerInfo `json:"servers"`
}

type MCPServerInfo struct {
    Name   string `json:"name"`
    Status string `json:"status"`
}
```

#### `skills_list` — Skills 列表

```go
type SkillsListData struct {
    Skills []SkillEntry `json:"skills"`
    Total  int          `json:"total"`
    Filter string       `json:"filter,omitempty"`
}
```

#### `worker_command` — Worker stdio 命令触发

```go
type WorkerCommandData struct {
    Command WorkerStdioCommand `json:"command"`
    Args    string             `json:"args,omitempty"`
    Extra   map[string]any     `json:"extra,omitempty"`
}
```

#### `done` — Turn 终止符

```go
type DoneData struct {
    Success bool           `json:"success"`
    Stats   map[string]any `json:"stats,omitempty"`
    Dropped bool           `json:"dropped,omitempty"` // 有 delta 被丢弃
}
```

**时序约束**：必须是 Turn 的最后一个 S→C 事件。

#### `error` — 错误通知

```go
type ErrorData struct {
    Code    ErrorCode `json:"code"`
    Message string    `json:"message"`
}
```

**时序约束**：必须在 `done` 之前发送。

#### `pong` — 心跳响应

```go
// Data: struct{}{}
// Seq: 0
```

> **注意**：`pong` 的 `data` 为空结构体 `struct{}{}`（Go 端），在 TS SDK 中对应 `PongData` 类型（可能包含 `state` 等附加字段）。Go SDK 不导出 `PongData` 常量，需通过 `Event.Type == "pong"` 匹配。

## 错误码参考

| 错误码 | 含义 |
|--------|------|
| `WORKER_START_FAILED` | Worker 进程启动失败 |
| `WORKER_CRASH` | Worker 进程崩溃 |
| `WORKER_TIMEOUT` | Worker 执行超时 |
| `WORKER_OOM` | Worker 内存不足 |
| `PROCESS_SIGKILL` | Worker 被 SIGKILL 终止 |
| `WORKER_OUTPUT_LIMIT` | 单行输出超限（10MB） |
| `INVALID_MESSAGE` | 消息格式无效 |
| `SESSION_NOT_FOUND` | Session 不存在 |
| `SESSION_BUSY` | Session 正忙（硬拒绝） |
| `SESSION_EXPIRED` | Session 已过期 |
| `SESSION_TERMINATED` | Session 已终止 |
| `SESSION_INVALIDATED` | Session 被失效 |
| `UNAUTHORIZED` | 认证失败 |
| `AUTH_REQUIRED` | 需要认证 |
| `INTERNAL_ERROR` | 内部错误 |
| `PROTOCOL_VIOLATION` | 协议违规 |
| `VERSION_MISMATCH` | 协议版本不匹配 |
| `CONFIG_INVALID` | 配置校验失败 |
| `RATE_LIMITED` | 请求频率超限 |
| `GATEWAY_OVERLOAD` | Gateway 过载 |
| `EXECUTION_TIMEOUT` | Worker 僵死超时 |
| `RECONNECT_REQUIRED` | 服务端要求客户端重连 |
| `RESUME_RETRY` | Session resume 失败，建议重试 |
| `NOT_SUPPORTED` | 操作不支持 |
| `TURN_TIMEOUT` | Turn 执行超时 |

## 参考

- [AEP 协议](aep-protocol.md)：协议完整规范
- [Session 管理](../guides/developer/session-management.md)：Session 生命周期
