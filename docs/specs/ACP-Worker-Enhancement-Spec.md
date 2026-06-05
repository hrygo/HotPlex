---
type: spec
tags:
  - project/HotPlex
  - worker/acp
  - architecture/enhancement
  - priority/P0
date: 2026-05-30
status: verified
progress: 0
estimated_hours: 69
supersedes:
  - ACP-Worker-Spec.md
---

# ACP Worker 全面增强规格 — 成为 HotPlex 首选 Worker

> 本文档定义 ACP Worker 从"可用"到"生产首选"的完整改进路径。
> 目标：功能完备、开发者友好、运维可视、协议前瞻。
> 基于 2026-05-30 代码库现状，对照 ClaudeCode / OCS / CodexCLI 三个成熟 Worker 的差距分析。

---

## 1. 现状评估

### 1.1 已实现能力

| 维度 | 状态 | 说明 |
|------|------|------|
| JSON-RPC 2.0 codec | ✅ 完整 | NDJSON 读写、4 种消息类型分发、64KB/10MB scanner 限制 |
| ACP 握手 | ✅ 完整 | initialize → session/new/load、协议版本协商、能力交换 |
| 事件映射 | ✅ 完整 | 12 种 session/update 类型 → AEP 映射、合成 message.start/end/state |
| 流式输出 | ✅ 完整 | agent_message_chunk → MessageDelta、agent_thought_chunk → Reasoning |
| 权限请求 | ✅ 完整 | request_permission → PermissionRequest、多选项→布尔转换 |
| 工具生命周期 | ✅ 完整 | tool_call → ToolCall、tool_call_update → ToolUpdate/ToolResult |
| 计划/模式 | ✅ 完整 | plan → PlanData、current_mode_update → ModeUpdate |
| 会话恢复 | ✅ 完整 | session/load + history_lost fallback → session/new |
| 优雅终止 | ✅ 完整 | session/cancel → SIGTERM → 5s → SIGKILL |
| 背压控制 | ✅ 完整 | acpConn 256 buffer、droppable/critical 分类、5s blocking timeout |
| Per-bot 命令覆盖 | ✅ 完整 | acp_command 配置传播 |
| Env blocklist | ✅ 完整 | CLAUDECODE / HOTPLEX_ 前缀过滤 |

### 1.2 代码规模

| 文件 | 行数 | 职责 |
|------|------|------|
| `codec.go` | 151 | NDJSON 编解码 |
| `client.go` | 431 | ACP Client（握手/会话/prompt/通知） |
| `mapper.go` | 497 | ACP ↔ AEP 事件映射 |
| `worker.go` | 470 | Worker 生命周期 |
| `conn.go` | 122 | SessionConn 实现 |
| 测试文件 (4) | 1,048 | codec/client/mapper/conn 单元测试 |
| **合计** | **2,719** | |

### 1.3 关键差距总览

对比成熟 Worker（ClaudeCode / OCS），ACP 缺失以下能力：

| 优先级 | 差距 | 影响 |
|--------|------|------|
| **P0** | 无 WorkerCommander（Compact/Rewind/Clear） | 用户无法执行上下文管理命令 |
| **P0** | 无 ControlRequester（context_usage / mcp_status） | Done 统计缺少上下文使用率 |
| **P0** | 无 MCP 配置注入 | mcpServers 始终为空，Agent 无法使用外部工具 |
| **P0** | 无系统提示词注入 | B/C 通道 Agent 配置无法传递给 Agent |
| **P0** | 无 InputRecoverer | Bridge 崩溃恢复无法重投递最后消息 |
| **P1** | 无 Question/Elicitation 响应 | Agent 发起的交互请求被拒绝 |
| **P1** | ResetContext 返回 ErrNotImplemented | /reset 必须杀进程重建，成本高 |
| **P1** | 无 JSON Schema 支持 | 结构化输出能力缺失 |
| **P1** | 无 WorkerCommander.set_model 集成 | 运行时模型切换未暴露给 Bridge |
| **P1** | 无 cost tracking | 运营成本不可追踪 |
| **P1** | 无 ForkSession 桥接 | session/fork 方法存在但未接入 |
| **P2** | 无 AllowedDirs 传递 | 沙箱目录限制未传递 |
| **P2** | 无 MaxTurns / MaxBudgetUSD | Agent 无 turn/预算限制 |
| **P2** | pending permissions 无 TTL | 遗弃请求可能内存累积 |
| **P2** | 无 Worker 集成测试 | Start/Input/Terminate 全链路未测试 |
| **P2** | 无 AGENTS.md 文档 | 缺少模块级开发文档 |

---

## 2. 功能需求

### FR-01: WorkerCommander 接口实现

**目标**：实现 `worker.WorkerCommander` 接口，支持 `/compact`、`/clear`、`/rewind` 命令。

**现状**：ACP Worker 未实现 WorkerCommander。ACP v1 无 Compact/Rewind 标准方法。

**可行方案**（对照 ClaudeCode/OCS 实现验证）：

```go
// internal/worker/interfaces.go 定义：
// type WorkerCommander interface {
//     Compact(ctx context.Context, args map[string]any) error
//     Clear(ctx context.Context) error
//     Rewind(ctx context.Context, targetID string) error
// }
var _ worker.WorkerCommander = (*Worker)(nil)
```

| 命令 | ACP 映射 | 依据 |
|------|---------|------|
| `Compact()` | 返回 `worker.ErrNotImplemented` | ClaudeCode Clear 也返回 ErrNotImplemented。ACP 无等效方法，Terminate+Start 成本过高。 |
| `Clear()` | `session/new`（新会话，同 cwd） | 等价于 ResetContext，复用进程创建新 session |
| `Rewind()` | 返回 `worker.ErrNotImplemented` | ACP 无 rewind 方法，无法截断历史到指定消息 |

**Bridge 侧路由**（`worker_cmds.go:132-161`）：如果 Worker 未实现 WorkerCommander，命令会 fallback 到 `w.Input(ctx, content, nil)` 作为纯文本发送。实现后走 Commander 路径。

**验收标准**：

| ID | 验收条件 |
|----|---------|
| AC-FR01-01 | `var _ worker.WorkerCommander = (*Worker)(nil)` 编译通过 |
| AC-FR01-02 | `Clear()` 创建新 ACP session，acpSessionID 变更，PID 不变 |
| AC-FR01-03 | `Compact()` 和 `Rewind()` 返回 `worker.ErrNotImplemented`，不触发进程操作 |
| AC-FR01-04 | 用户通过 Slack/飞书发送 `/clear` 后上下文被清空，新 prompt 从零开始 |
| AC-FR01-05 | 命令执行失败时返回 `worker.WorkerError{Kind: ErrKindUnavailable}` |

---

### FR-02: ControlRequester 接口实现

**目标**：实现 `worker.ControlRequester` 接口，支持 Bridge 查询上下文使用率和运行时控制。

**实际接口签名**（`internal/worker/interfaces.go:6`）：

```go
type ControlRequester interface {
    SendControlRequest(ctx context.Context, subtype string, body map[string]any) (map[string]any, error)
}
```

**调用点**（`bridge_forward.go:297-299`）：每个 Done 事件后调用 `fetchContextUsage`，5s 超时。
**调用点**（`worker_cmds.go:37-126`）：用户通过客户端发起 `set_model`、`mcp_status`、`set_permission_mode` 请求。

```go
var _ worker.ControlRequester = (*Worker)(nil)
```

| subtype | 处理方式 | 可行性 |
|---------|---------|--------|
| `get_context_usage` | 从 mapper 缓存的 `usage_update` 提取 size/used，映射为 `MapContextUsageResponse` 期望的 camelCase 键名 `maxTokens`/`totalTokens`/`model` | ✅ `usage_update` 已包含 size/used，需新增 mapper.LastUsage() |
| `set_model` | 委托给 `client.SetSessionModel()` | ✅ client 已实现 |
| `set_permission_mode` | 更新 autoApprove 标志 | ✅ 内存操作 |
| `mcp_status` | 返回空 map（ACP 无 MCP 状态查询方法） | ⚠️ 降级处理 |

**关键设计**：`fetchContextUsage` 调用 `events.MapContextUsageResponse(resp)`（`helpers.go:56-94`），期望 resp 包含 **camelCase** 键名：`totalTokens`、`maxTokens`、`model`。还接受 `percentage`、`memoryFiles`、`mcpTools`、`agents`、`categories`、`skills`（均为可选，零值不影响）。ACP 的 `usage_update` 提供 `size`/`used`，需做字段映射：

```go
// ACP usage_update → ControlRequester response
// MapContextUsageResponse 期望 camelCase 键名（helpers.go:56-94）
return map[string]any{
    "maxTokens":  snapshot.ContextSize,
    "totalTokens": snapshot.ContextUsed,
    "model":       w.lastKnownModel,
}, nil
```

**验收标准**：

| ID | 验收条件 |
|----|---------|
| AC-FR02-01 | `var _ worker.ControlRequester = (*Worker)(nil)` 编译通过 |
| AC-FR02-02 | `SendControlRequest(ctx, "get_context_usage", nil)` 返回非空 maxTokens/totalTokens |
| AC-FR02-03 | Done 事件的 Stats 包含 `context_size` 和 `context_used`（由 `fetchContextUsage` 注入） |
| AC-FR02-04 | `set_model` 请求成功切换 Agent 模型，后续 prompt 使用新模型 |
| AC-FR02-05 | Agent 未发送过 `usage_update` 时，`get_context_usage` 返回零值 map 而非错误（与 OCS 行为一致） |
| AC-FR02-06 | `mcp_status` 返回空 map，不报错 |

---

### FR-03: MCP 配置注入

**目标**：将 SessionInfo 中的 MCP 配置传递给 ACP Agent。

**现状验证**：

- `buildWorkerInfo()`（`bridge.go:620-646`）**已设置** `info.MCPConfig`（从 `b.mcpConfigJSON` 加载）和 `info.StrictMCPConfig = true`
- 传播链已完整：`config.yaml (claude_code.mcp_servers)` → `config.go` → `Bridge.mcpConfigJSON` → `buildWorkerInfo` → `SessionInfo.MCPConfig`
- **唯一缺口**：ACP Worker `Start()` 中 `client.NewSession(cwd, nil)` 和 `client.LoadSession(sid, cwd, nil)` 第三参数始终传 nil

**关键类型转换**：`SessionInfo.MCPConfig` 是 JSON 字符串（`{"mcpServers":{...}}`），但 `client.NewSession` 期望 `mcpServers []any`。需要解析 JSON 并提取 mcpServers 数组。

**方案**（仅改 ACP Worker，不改 Bridge）：

```go
// worker.go Start() 中
func parseMCPServers(mcpConfig string) []any {
    if mcpConfig == "" {
        return []any{}
    }
    var raw map[string]any
    if err := json.Unmarshal([]byte(mcpConfig), &raw); err != nil {
        return []any{}
    }
    if servers, ok := raw["mcpServers"]; ok {
        return normalizeMCPServers(servers)
    }
    return []any{}
}

mcpServers := parseMCPServers(session.MCPConfig)
result, err := w.client.NewSession(ctx, w.cwd, mcpServers)
```

**Cron 特殊处理**：`buildWorkerInfo` 为 cron session 注入空 MCP（`{"mcpServers":{}}` + `StrictMCPConfig=true`），ACP Worker 会正确解析为 `[]any{}`，禁止 Agent 访问任何 MCP server。

**验收标准**：

| ID | 验收条件 |
|----|---------|
| AC-FR03-01 | `config.yaml` 中配置 `mcp_servers` 后，ACP Agent 收到 `session/new` 的 `mcpServers` 参数 |
| AC-FR03-02 | mcpServers 为空时，Agent 收到 `[]`（非 null） |
| AC-FR03-03 | Agent 成功连接 MCP server 后，tool_call 事件中可见 MCP 工具调用 |
| AC-FR03-04 | Cron session 的 mcpServers 为空数组（符合 Bridge 的 suppress MCP 逻辑） |

---

### FR-04: 系统提示词注入

**目标**：将 HotPlex 的 B/C 通道 Agent 配置传递给 ACP Agent 作为系统提示词。

**现状验证**（关键发现）：

- `buildWorkerInfo()`（`bridge.go:620-646`）**未设置** `SystemPrompt` 和 `SystemPromptReplace`
- **但 `injectAgentConfig()`（`bridge_worker.go:268-276`）已设置**：在 `createAndLaunchWorker()` Step 4 中调用 `agentconfig.Load()` + `BuildSystemPrompt()` 填充 `info.SystemPrompt`
- ACP v1 `session/new` 无 system prompt 参数
- **结论**：Bridge 侧无需改动，仅 ACP Worker 侧需消费 `session.SystemPrompt`

**改动范围**：

| 改动层 | 文件 | 说明 |
|--------|------|------|
| ~~Bridge~~ | ~~`bridge.go`~~ | ~~**无需改动**~~ `injectAgentConfig`（bridge_worker.go:268-276）已调用 `agentconfig.Load` + `BuildSystemPrompt` |
| ACP Worker | `internal/worker/acp/worker.go` | `Start()` 缓存 `session.SystemPrompt`，`Input()` 首次 prompt 前注入系统提示词 |

**方案**：仅需 ACP Worker 侧消费 `session.SystemPrompt`（Bridge 侧已由 `injectAgentConfig` 自动填充）。

ACP v1 无标准系统提示词机制。采用 Prompt 前置注入：

```go
// worker.go Input() 中
func (w *Worker) injectSystemPrompt(content string) string {
    if w.systemPromptInjected || w.systemPrompt == "" {
        return content
    }
    w.systemPromptInjected = true
    return fmt.Sprintf("[SYSTEM INSTRUCTIONS]\n%s\n[/SYSTEM INSTRUCTIONS]\n\n%s", w.systemPrompt, content)
}
```

**验收标准**：

| ID | 验收条件 |
|----|---------|
| AC-FR04-01 | 配置 SOUL.md 后，Bridge 正确填充 `SessionInfo.SystemPrompt` |
| AC-FR04-02 | ACP Worker 首次 prompt 中包含系统提示词内容 |
| AC-FR04-03 | 系统提示词仅注入一次（首次 prompt），后续 prompt 不重复注入 |
| AC-FR04-04 | 空 system prompt 时不注入任何前缀 |
| AC-FR04-05 | 系统提示词长度超过 32KB 时截断并记录警告日志 |
| AC-FR04-06 | 改动对 ClaudeCode Worker 无影响（已有 `--append-system-prompt-file` 机制） |
| AC-FR04-07 | ResetSession 后 SystemPrompt 不丢失（ResetContext 复用进程时内存中已缓存） |

---

### FR-05: InputRecoverer 接口实现

**目标**：Bridge 崩溃恢复时能重投递最后一次用户输入。

**现状验证**：

- Bridge `handleWorkerExit`（`bridge_forward.go:426-430`）从 `w.Conn()` 断言 `InputRecoverer`：
  ```go
  if conn := w.Conn(); conn != nil {
      if ir, ok := conn.(worker.InputRecoverer); ok {
          lastInput = sanitizeLastInput(ir.LastInput())
      }
  }
  ```
- 断言目标是被 `Conn()` 返回的 `SessionConn` 实现，即 `acpConn`
- `acpConn.Send()` 返回 `ErrNotImplemented`（用户输入走 `client.Prompt`），无 lastInput 缓存

**方案**：

```go
// conn.go — 在 acpConn 上新增
type acpConn struct {
    userID    string
    sessionID string
    log       *slog.Logger
    recvCh    chan *events.Envelope
    mu        sync.Mutex
    closed    bool
    lastInput atomic.Pointer[string]  // 新增
}

func (c *acpConn) LastInput() string {  // 新增 — 满足 worker.InputRecoverer
    if p := c.lastInput.Load(); p != nil {
        return *p
    }
    return ""
}

var _ worker.InputRecoverer = (*acpConn)(nil)
```

在 `Worker.Input()` 调用 `client.Prompt()` 前缓存 content 到 `conn.lastInput`。

**验收标准**：

| ID | 验收条件 |
|----|---------|
| AC-FR05-01 | `var _ worker.InputRecoverer = (*acpConn)(nil)` 编译通过 |
| AC-FR05-02 | Input("hello") 后 `conn.LastInput()` 返回 `"hello"` |
| AC-FR05-03 | 连续两次 Input 后 `LastInput()` 返回最新内容 |
| AC-FR05-04 | 未调用 Input 时 `LastInput()` 返回空字符串 |
| AC-FR05-05 | Terminate 后 `LastInput()` 仍可返回缓存值（不随 conn 关闭丢失） |

---

### FR-06: Question/Elicitation 响应支持

**目标**：支持 ACP Agent 发起的 Question 和 Elicitation 交互请求。

**现状**：`HandleQuestionResponse()` 和 `HandleElicitationResponse()` 均返回 `ErrNotImplemented`。

**方案**：ACP v1 标准仅定义了 `session/request_permission` 作为 server-initiated request。Question/Elicitation 不在 ACP v1 标准中。

短期策略：
1. 检测 Agent 发送的未知 server-initiated request（非 `request_permission`），将 method 和 params 包装为 AEP Raw 事件透传给客户端
2. 客户端回复时，Worker 转发回 Agent 作为 JSON-RPC response

长期策略：等待 ACP 协议标准化 Question/Elicitation 方法。

**验收标准**：

| ID | 验收条件 |
|----|---------|
| AC-FR06-01 | Agent 发送非 `request_permission` 的 server-initiated request 时，不返回错误，而是透传为 Raw 事件 |
| AC-FR06-02 | `HandleQuestionResponse()` 能将 AEP 回复转发为 JSON-RPC response |
| AC-FR06-03 | `HandleElicitationResponse()` 能将 AEP 回复转发为 JSON-RPC response |
| AC-FR06-04 | Agent 未发送交互请求时，两个 Handle 方法不会被调用 |

---

### FR-07: ResetContext 改进

**目标**：减少 /reset 的资源开销，避免每次都杀进程重建。

**实际接口签名**（`worker.go:84`）：`ResetContext(ctx context.Context) (ResetResult, error)` — 返回 `ResetResult{ConnReplaced bool}` 描述重置结果。

**Bridge 行为**（已重构，`InPlaceReseter` 已删除）：

```
1. ResetContext() 成功 → 读取 ResetResult.ConnReplaced
   ├─ ConnReplaced == true → 重建 forwardEvents goroutine
   └─ ConnReplaced == false → 保持 forwardEvents goroutine
2. ResetContext() 返回 ErrNotImplemented → Terminate + Start（全量重建）
```

**方案**：P1 实现 session/new 复用进程 + `ResetResult{ConnReplaced: false}` + `internal_reset` 事件：

```go
func (w *Worker) ResetContext(ctx context.Context) (worker.ResetResult, error) {
    w.Mu.Lock()
    defer w.Mu.Unlock()

    // 1. 取消当前 turn
    _ = w.client.Cancel(ctx, w.acpSessionID)

    // 2. 创建新 session（复用同一进程）
    mcpServers := parseMCPServers(w.lastMCPConfig)
    result, err := w.client.NewSession(ctx, w.cwd, mcpServers)
    if err != nil {
        // 回退由 Bridge 处理（Terminate + Start）
        return worker.ResetResult{}, fmt.Errorf("acp reset: %w", err)
    }
    w.acpSessionID = result.SessionID
    w.mapper.Reset()

    // 3. 通过 EventInjector 注入 internal_reset 事件（替代 resetGenerationer）
    w.conn.Inject(events.NewInternalReset(w.LoadResetGeneration()))

    return worker.ResetResult{ConnReplaced: false}, nil
}

// InPlaceReseter and resetGenerationer interfaces have been deleted.
// Replaced by: ResetResult{ConnReplaced: false} + internal_reset event via EventInjector.
```

**关键机制**：

- `BaseWorker` 已提供 `IncResetGeneration()`/`LoadResetGeneration()` 用于 generation 计数
- Worker 通过 `conn.Inject()` 将 `internal_reset` 事件注入 Recv() 流
- `forwardEvents` goroutine 在 `handleInternalReset` 中处理该事件，更新 accumulator
- `ResetResult{ConnReplaced: false}` 告知 Bridge 保持现有 forwardEvents goroutine

**验收标准**：

| ID | 验收条件 |
|----|---------|
| AC-FR07-01 | `ResetContext()` 不返回 ErrNotImplemented |
| AC-FR07-02 | reset 后 PID 不变（复用进程） |
| AC-FR07-03 | reset 后 acpSessionID 为新值 |
| AC-FR07-04 | reset 后 mapper 状态已清空（msgActive=false, turnActive=false） |
| AC-FR07-05 | reset 失败时返回错误，Bridge 自动回退到 Terminate + Start |
| AC-FR07-06 | `InPlaceReset()` 返回 true，Bridge 不重建 forwardEvents goroutine |
| AC-FR07-07 | `resetGenerationer` generation 变更后，forwardEvents 自动重置 turn 计数 |

---

### FR-08: ForkSession 桥接

**目标**：将 `session/fork` 能力暴露给 Bridge。

**现状验证**：

- client.go 有 `ForkSession` 方法但未在 Worker 层使用
- `SessionInfo.ForkSession` 是 **`bool` 类型**（非 string），表示 "恢复时创建分叉而非复用"
- `buildWorkerInfo()` **未设置** `ForkSession` — 需要改动 Bridge 或通过其他机制传递
- `client.ForkSession(sessionID)` 接受 ACP session ID，返回新 session

**可行性限制**：`ForkSession` 的语义是"从已有 session 分叉"，但 `buildWorkerInfo` 不传递这个标志。当前 Bridge 不区分 fork 和 resume。降级为 P2，待 Bridge 支持后再接入。

**验收标准**：

| ID | 验收条件 |
|----|---------|
| AC-FR08-01 | Worker.Start() 中检测 ForkSession 标志，调用 ForkSession 而非 NewSession |
| AC-FR08-02 | Fork 后的新 session 保留原始会话的上下文 |
| AC-FR08-03 | Fork 失败时 fallback 到 NewSession |
| AC-FR08-04 | 需 Bridge 改动传递 ForkSession 标志（P2 前置条件） |

---

### FR-09: JSON Schema 支持

**目标**：将 `SessionInfo.JSONSchema` 传递给 ACP Agent，支持结构化输出。

**方案**：ACP v1 `session/prompt` 的 prompt content 支持 `type: "structured"` 或通过 `_meta.jsonSchema` 传递。具体映射取决于 Agent 实现。

短期方案：将 JSONSchema 注入到 prompt 的 `_meta` 扩展字段。

**验收标准**：

| ID | 验收条件 |
|----|---------|
| AC-FR09-01 | 配置 json_schema 后，prompt 请求中包含 schema 信息 |
| AC-FR09-02 | Agent 返回符合 schema 的结构化输出 |
| AC-FR09-03 | 无 schema 配置时 prompt 格式不受影响 |

---

### FR-10: cost tracking

**目标**：从 `usage_update` 中提取 cost 信息并传递到 Done 统计。

**现状**：mapper 内部消费 `usage_update` 但仅提取 token 计数，忽略 cost 字段。

**方案**：扩展 mapper 的 usage 缓存，增加 cost 提取：

```go
type usageSnapshot struct {
    InputTokens       int
    OutputTokens      int
    ThoughtTokens     int
    CachedReadTokens  int
    CachedWriteTokens int
    TotalTokens       int
    ContextSize       int    // 新增
    ContextUsed       int    // 新增
    Cost              *CostInfo // 新增
}

type CostInfo struct {
    Amount   float64 `json:"amount"`
    Currency string  `json:"currency"`
}
```

**验收标准**：

| ID | 验收条件 |
|----|---------|
| AC-FR10-01 | `usage_update` 包含 cost 字段时，Done Stats 中包含 `cost` |
| AC-FR10-02 | 无 cost 字段时，Done Stats 中不含 `cost` key |
| AC-FR10-03 | 多次 `usage_update` 时，cost 累加计算 |

---

## 3. 易用性需求

### U-01: 配置简化

**目标**：降低 ACP Worker 的配置门槛。

**现状**：需要手动配置 `worker_type: acp`、`acp.command`、`acp.auto_approve`。

**方案**：

| 改进 | 说明 |
|------|------|
| 自动检测 Agent | `Start()` 时检测 Agent 二进制是否可用，不可用时给出明确错误信息和安装指引 |
| 配置验证 | 启动时验证 ACP 配置完整性，缺失关键字段时给出可操作的修复建议 |
| 默认 worker_type | 新安装默认 `worker_type: acp`（如果 acp.command 可用） |
| 配置模板 | 提供 `hotplex setup acp` 交互式配置命令 |

**验收标准**：

| ID | 验收条件 |
|----|---------|
| AC-U01-01 | `acp.command` 二进制不存在时，启动日志包含 "command not found" + 安装指引 |
| AC-U01-02 | `hotplex setup acp` 交互式完成 ACP 配置并验证 Agent 可用 |
| AC-U01-03 | 配置错误（如 worker_type=acp 但未配 command）时，启动失败并给出修复建议 |

---

### U-02: Agent 发现与健康检查

**目标**：运行时可见 Agent 状态和健康度。

**方案**：

| 检查点 | 实现 |
|--------|------|
| Agent 版本 | 从 initialize 响应提取 agentInfo.version，写入 WorkerHealth |
| 协议版本 | 从 initialize 响应提取 protocolVersion |
| Agent 能力 | 从 initialize 响应提取 agentCapabilities，影响功能降级 |
| 握手延迟 | 记录 initialize → session/new 耗时 |
| Session 数 | `ListSessions()` 调用（可选，P2） |

```go
type ACPHealth struct {
    AgentName        string
    AgentVersion     string
    ProtocolVersion  int
    Capabilities     AgentCapabilities
    HandshakeLatency time.Duration
    SessionID        string
}
```

**验收标准**：

| ID | 验收条件 |
|----|---------|
| AC-U02-01 | `Worker.Health()` 返回包含 agent version、protocol version、session ID |
| AC-U02-02 | Agent 不支持某能力时（如 loadSession=false），相关功能优雅降级 |
| AC-U02-03 | 启动 Banner 显示 Agent 名称和版本 |

---

### U-03: 错误信息可操作性

**目标**：每个错误场景都提供可操作的下一步。

**方案**：

| 错误场景 | 当前消息 | 改进后消息 |
|---------|---------|-----------|
| Agent 二进制不存在 | "exec: not found" | "ACP agent '{command}' not found in PATH. Install: brew install {command} / or set acp.command in config.yaml" |
| 握手超时 | context.DeadlineExceeded | "ACP agent handshake timed out after 30s. Check: 1) agent is running 2) API keys are configured 3) network connectivity" |
| 协议版本不匹配 | 无处理 | "ACP protocol version mismatch: agent=v{agent}, gateway=v1. Update agent or gateway." |
| Session load 失败 | silent fallback | "Session load failed: {reason}. Falling back to new session (history lost)." |

**验收标准**：

| ID | 验收条件 |
|----|---------|
| AC-U03-01 | 每种错误场景的日志消息包含错误原因 + 至少一个可操作的下一步 |
| AC-U03-02 | 错误消息不暴露内部实现细节（如 goroutine id、channel state） |
| AC-U03-03 | 错误消息可被日志系统结构化检索（slog JSON 格式） |

---

### U-04: 调试模式

**目标**：提供 ACP 协议级调试能力。

**方案**：

```yaml
acp:
  debug: true  # 启用协议级日志
```

启用后：
- 记录所有 JSON-RPC 请求/响应到 `~/.hotplex/logs/acp-trace-{sessionID}.jsonl`
- 每个 JSON-RPC 消息一行，包含时间戳和方向（→/←）
- 不影响正常功能，仅增加磁盘 I/O

**验收标准**：

| ID | 验收条件 |
|----|---------|
| AC-U04-01 | `acp.debug: true` 后，`~/.hotplex/logs/` 下生成 acp-trace 文件 |
| AC-U04-02 | trace 文件中每行为完整 JSON-RPC 消息 |
| AC-U04-03 | `acp.debug: false`（默认）时不生成 trace 文件 |
| AC-U04-04 | trace 文件大小上限 50MB，超出自动轮转 |

---

## 4. UI/UX 需求

### UX-01: Plan 事件可视化

**目标**：将 ACP Agent 的 plan 事件渲染为结构化 UI 组件。

**现状**：plan 事件已映射为 AEP `PlanData`，但飞书/Slack 适配器未特殊渲染。

**方案**：

| 平台 | 渲染方式 |
|------|---------|
| 飞书 | 富文本卡片：checkbox 列表，status 映射为 ✓/○/✗，priority 映射为颜色标签 |
| Slack | Block Kit：checkbox group + emoji 状态 + priority badge |
| WebChat | React 组件：可折叠 plan panel，带进度条 |

**验收标准**：

| ID | 验收条件 |
|----|---------|
| AC-UX01-01 | Plan 事件到达后，飞书/Slack 消息中出现结构化的任务列表 |
| AC-UX01-02 | 任务状态变化（pending → in_progress → completed）实时更新 |
| AC-UX01-03 | 高优先级任务视觉突出 |

---

### UX-02: Mode 切换通知

**目标**：Agent 切换模式时通知用户。

**现状**：`current_mode_update` 映射为 `ModeUpdate` 事件但未在客户端渲染。

**方案**：发送一条轻量级状态消息，格式：`🔄 Mode: {mode_name}`

**验收标准**：

| ID | 验收条件 |
|----|---------|
| AC-UX02-01 | Agent 切换模式时，客户端收到模式变更通知 |
| AC-UX02-02 | 模式名称为人类可读（非 mode ID） |

---

### UX-03: Tool Call 进度展示

**目标**：利用 `tool_update` 事件展示工具调用中间状态。

**现状**：`tool_update` 已映射但客户端未区分 pending/in_progress 状态。

**方案**：

| 状态 | 渲染 |
|------|------|
| pending | 显示工具名 + "⏳ Pending..." |
| in_progress | 显示工具名 + "🔄 Running..." + 文件路径 |
| completed | 显示工具名 + "✅" + diff 摘要 |
| failed | 显示工具名 + "❌" + 错误信息 |

**验收标准**：

| ID | 验收条件 |
|----|---------|
| AC-UX03-01 | 工具调用在 pending 状态时客户端显示等待指示器 |
| AC-UX03-02 | 工具调用进入 in_progress 后状态更新为执行中 |
| AC-UX03-03 | completed/failed 后显示最终结果 |

---

### UX-04: Usage 实时统计

**目标**：在 Agent 运行过程中展示 token 使用情况。

**方案**：将 `usage_update` 的累积数据通过 `state` 事件推送，客户端在 UI 中显示进度条（context used / context size）。

**验收标准**：

| ID | 验收条件 |
|----|---------|
| AC-UX04-01 | Agent 处理过程中，客户端实时更新 token 计数 |
| AC-UX04-02 | context used 接近 context size 时显示警告 |

---

## 5. 非功能性需求

### NFR-01: 性能

| 指标 | 目标 | 测量方式 |
|------|------|---------|
| 端到端延迟（首 token） | ≤ 500ms（不含 LLM 推理） | prompt 发送到第一个 message_delta 的时间差 |
| 事件映射开销 | ≤ 50μs/event | mapper.MapNotification 单次调用 benchmark |
| 内存占用（per-session） | ≤ 10MB | pprof heap snapshot |
| JSON-RPC 吞吐 | ≥ 10,000 msg/s | codec benchmark |
| CPU 占用（空闲） | ≤ 0.1% | top/ps 测量 |

**验收标准**：

| ID | 验收条件 |
|----|---------|
| AC-NFR01-01 | `go test -bench=BenchmarkMapNotification -benchmem` 显示 ≤ 50μs/op, ≤ 1 alloc/op |
| AC-NFR01-02 | `go test -bench=BenchmarkCodec -benchmem` 显示 ≥ 10,000 ops/s |
| AC-NFR01-03 | 单 session 空闲时 RSS ≤ 10MB |

---

### NFR-02: 可靠性

| 场景 | 预期行为 |
|------|---------|
| Agent 进程崩溃 | readLoop 检测到 EOF → 发送 error + done → Bridge 触发重启 |
| Agent 无响应 | turnTimeout（可配置，默认 10min）触发 Cancel + Terminate |
| Agent 协议错误（非 JSON） | codec 记录日志并跳过该行，不崩溃 |
| Agent 发送超大消息（>10MB） | codec scanner 拒绝并记录日志 |
| 并发 Input 调用 | Worker.mu 保证串行化 |
| 网络分区（stdio pipe 断开） | readLoop 退出 → error + done 事件 |

**验收标准**：

| ID | 验收条件 |
|----|---------|
| AC-NFR02-01 | Agent 崩溃后，用户收到错误消息，Bridge 可自动重启新 session |
| AC-NFR02-02 | Agent 发送 100 行非 JSON 输出到 stdout，Worker 不崩溃 |
| AC-NFR02-03 | 超时后 Worker 自动 Cancel 并 Terminate |
| AC-NFR02-04 | 使用 `-race` 运行全部测试无 data race |

---

### NFR-03: 安全

| 安全域 | 要求 |
|--------|------|
| 环境变量隔离 | env blocklist 阻止 CLAUDECODE*/HOTPLEX* 前缀 |
| 命令白名单 | acp.command 必须通过 `security.RegisterCommand()` 注册 |
| 输入验证 | prompt 内容长度限制（≤ 1MB） |
| 资源限制 | 单 session 文件描述符 ≤ 100，内存 ≤ 512MB（Agent 侧限制由 proc.Manager 管理） |
| 权限最小化 | auto_approve=false 为默认值，Agent 请求权限必须经用户确认 |

**验收标准**：

| ID | 验收条件 |
|----|---------|
| AC-NFR03-01 | 未注册的 acp.command 被拒绝执行 |
| AC-NFR03-02 | HOTPLEX_API_KEY 环境变量不会传递给 Agent 进程 |
| AC-NFR03-03 | prompt 超过 1MB 时返回错误而非截断 |

---

### NFR-04: 可观测性

**现状验证**：HotPlex 使用集中式 Prometheus 指标（`internal/metrics/metrics.go`），通过 `worker_type` label 区分 Worker 类型。**不** 在 Worker 包内注册指标。

**方案**：遵循现有模式，在 `internal/metrics/metrics.go` 中新增 ACP 相关的 label 值。已有指标自动覆盖 ACP Worker（通过 `worker_type="acp"` label）：

| 已有指标 | ACP 自动覆盖 | 说明 |
|---------|-------------|------|
| `hotplex_sessions_total{worker_type="acp"}` | ✅ | 已由 Bridge 注册 |
| `hotplex_workers_running{worker_type="acp"}` | ✅ | 已由 Bridge 注册 |
| `hotplex_worker_starts_total{worker_type="acp",result}` | ✅ | 已由 Bridge 注册 |
| `hotplex_worker_exec_duration_seconds{worker_type="acp"}` | ✅ | 已由 Bridge 注册 |
| `hotplex_worker_crashes_total{worker_type="acp",exit_code}` | ✅ | 已由 Bridge 注册 |
| `hotplex_session_starts_total{worker_type="acp"}` | ✅ | 已由 Bridge 注册 |
| `hotplex_session_errors_total{worker_type="acp",error_type}` | ✅ | 已由 Bridge 注册 |
| `hotplex_session_start_duration_seconds{worker_type="acp"}` | ✅ | 已由 Bridge 注册 |
| `hotplex_worker_memory_bytes{worker_type="acp"}` | ✅ | 已由 Bridge 注册 |
| `hotplex_worker_creation_duration_seconds{worker_type="acp"}` | ✅ | 已由 Bridge 注册 |

**新增指标**（在 `internal/metrics/metrics.go` 中注册）：

| 指标名 | 类型 | Labels | 说明 |
|--------|------|--------|------|
| `hotplex_acp_prompt_tokens_total` | CounterVec | `type` (input/output/cached_read/cached_write/thought) | Token 使用量 |
| `hotplex_acp_tool_calls_total` | CounterVec | `kind` (read/edit/delete/execute/search/other) | 工具调用次数 |
| `hotplex_acp_permission_requests_total` | CounterVec | `outcome` (approved/denied/timeout) | 权限请求结果 |
| `hotplex_acp_handshake_duration_seconds` | Histogram | — | 握手耗时 |

**验收标准**：

| ID | 验收条件 |
|----|---------|
| AC-NFR04-01 | 成功完成一次 prompt 后，`hotplex_workers_running{worker_type="acp"}` 值为 1 |
| AC-NFR04-02 | Token 使用量出现在 `hotplex_acp_prompt_tokens_total` |
| AC-NFR04-03 | 权限请求后 `hotplex_acp_permission_requests_total` 增加 |
| AC-NFR04-04 | `hotplex_worker_exec_duration_seconds{worker_type="acp"}` 记录完整 turn 耗时 |

---

## 6. 扩展性需求

### EX-01: 多 Agent 支持

**目标**：不同平台/Bot 可连接不同 ACP Agent。

**现状**：已支持 per-bot `acp_command` 覆盖。

**扩展方向**：

| 维度 | 当前 | 目标 |
|------|------|------|
| Agent 二进制 | per-bot acp_command | per-bot acp_command + per-session env |
| Agent 参数 | 固定 | 可配置 args（如 `--model`, `--strict`） |
| Agent 配置 | 无 | per-bot agent-config 路径 |

**验收标准**：

| ID | 验收条件 |
|----|---------|
| AC-EX01-01 | 同一 Gateway 实例同时运行 hermes-acp 和 claude --acp |
| AC-EX01-02 | 不同 Bot 的 Agent 进程互不干扰 |
| AC-EX01-03 | `acp.args` 配置项传递给 Agent 进程 |

---

### EX-02: ACP 协议版本前瞻

**目标**：为 ACP v2 协议变更做好准备。

**方案**：

```go
const (
    ACPProtocolV1 = 1
    ACPProtocolV2 = 2  // 未来
)

type ACPClient struct {
    protocolVersion int
    // ...
}
```

- `initialize` 时协商版本，存储到 client
- mapper 根据 protocolVersion 选择映射策略
- codec 对未知消息类型做 graceful skip（而非报错）

**验收标准**：

| ID | 验收条件 |
|----|---------|
| AC-EX02-01 | Agent 返回未知 protocolVersion 时，Worker 记录警告并尝试兼容处理 |
| AC-EX02-02 | 收到未知的 sessionUpdate 类型时，mapper 跳过而非报错 |
| AC-EX02-03 | 收到未知的 server-initiated method 时，Worker 透传为 Raw 事件 |

---

### EX-03: 插件化事件处理

**目标**：支持自定义事件处理 pipeline。

**方案**：

```go
type EventHook func(env *events.Envelope) *events.Envelope

type Worker struct {
    // ...
    eventHooks []EventHook
}

func (w *Worker) RegisterEventHook(hook EventHook)
```

使用场景：
- 敏感信息过滤（移除 prompt 中的密钥）
- 自定义指标收集
- 事件转换（适配特定客户端需求）

**验收标准**：

| ID | 验收条件 |
|----|---------|
| AC-EX03-01 | 注册的 hook 在每个事件通过时被调用 |
| AC-EX03-02 | hook 返回 nil 时事件被丢弃 |
| AC-EX03-03 | hook panic 不影响其他 hook 和主流程 |

---

### EX-04: 流式 HTTP Transport

**目标**：支持 ACP 的 Streamable HTTP transport（草案阶段）。

**方案**：抽象 transport 层。

```go
type Transport interface {
    Send(msg []byte) error
    Receive() ([]byte, error)
    Close() error
}

type stdioTransport struct { /* 当前实现 */ }
type httpTransport struct { /* 未来实现 */ }
```

**验收标准**：

| ID | 验收条件 |
|----|---------|
| AC-EX04-01 | Transport 接口定义完成，stdioTransport 实现该接口 |
| AC-EX04-02 | 切换 transport 不影响 codec/mapper/worker 逻辑 |
| AC-EX04-03 | 配置 `acp.transport: http` + `acp.endpoint: http://...` 时使用 HTTP transport |

---

## 7. 测试需求

### T-01: Worker 集成测试

**目标**：覆盖 Start/Input/Terminate 全生命周期。

**新建文件**：`internal/worker/acp/worker_integration_test.go`

| # | 测试名 | 覆盖点 |
|---|--------|--------|
| 1 | TestACPWorker_StartHandshake | initialize → session/new 完整握手 |
| 2 | TestACPWorker_StartWithLoad | session/load 恢复会话 |
| 3 | TestACPWorker_StartWithLoadFailure | session/load 失败 → fallback to new |
| 4 | TestACPWorker_InputPrompt | prompt 发送 + streaming events 接收 |
| 5 | TestACPWorker_InputPermission | 权限请求 → 回复 → Agent 继续 |
| 6 | TestACPWorker_TerminateGraceful | session/cancel → SIGTERM |
| 7 | TestACPWorker_TerminateForce | SIGTERM 超时 → SIGKILL |
| 8 | TestACPWorker_ConcurrentInput | 并发 Input 串行化 |
| 9 | TestACPWorker_AgentCrash | Agent 进程崩溃 → error + done |
| 10 | TestACPWorker_ResetContext | Reset 创建新 session |
| 11 | TestACPWorker_ForkSession | Fork 分叉会话 |

**测试基础设施**：使用 mock ACP Agent（Go test program 模拟 Agent 行为），通过 exec.Cmd 启动，stdin/stdout pipe 连接。

**验收标准**：

| ID | 验收条件 |
|----|---------|
| AC-T01-01 | 集成测试覆盖 Start/Input/Terminate/ResetContext 生命周期 |
| AC-T01-02 | 所有集成测试在 `-race -count=1` 下通过 |
| AC-T01-03 | mock Agent 覆盖正常/异常/崩溃场景 |

---

### T-02: Mapper 扩展测试

| # | 测试名 | 覆盖点 |
|---|--------|--------|
| 1 | TestMapNotification_UsageUpdate_Cost | cost 字段提取 |
| 2 | TestMapNotification_UsageUpdate_ContextSize | context size/used 提取 |
| 3 | TestMapNotification_UnknownUpdate | 未知 sessionUpdate 优雅跳过 |
| 4 | TestMapNotification_MultipleUsageAccumulation | 多次 usage 累加 |
| 5 | TestMapperReset | Reset() 清空 msgActive/turnActive/usage |

---

### T-03: Client 扩展测试

| # | 测试名 | 覆盖点 |
|---|--------|--------|
| 1 | TestACPClient_Initialize_VersionMismatch | 版本不匹配处理 |
| 2 | TestACPClient_Prompt_LargeContent | 大 prompt（>100KB） |
| 3 | TestACPClient_Cancel_NoActiveTurn | 无活跃 turn 时 cancel |
| 4 | TestACPClient_ServerRequest_Unknown | 未知 server method 处理 |

---

### T-04: 性能基准测试

```go
func BenchmarkMapNotification(b *testing.B)      // 单事件映射
func BenchmarkMapNotification_Stream(b *testing.B) // 连续 1000 事件
func BenchmarkCodec_ReadMessage(b *testing.B)     // NDJSON 解析
func BenchmarkCodec_WriteMessage(b *testing.B)    // NDJSON 序列化
func BenchmarkPrompt_FullTurn(b *testing.B)        // 完整 prompt turn（mock agent）
```

**验收标准**：

| ID | 验收条件 |
|----|---------|
| AC-T04-01 | MapNotification benchmark ≤ 50μs/op |
| AC-T04-02 | Codec benchmark ≥ 10,000 ops/s |

---

## 8. 可行性验证记录

> 以下为 2026-05-30 对照源码逐项验证的结果。每项标注 ✅ 可行 / ⚠️ 需额外改动 / ❌ 不可行。

### 8.1 接口实现验证

| FR | 接口 | 定义位置 | 方法签名 | 可行性 |
|----|------|---------|---------|--------|
| FR-01 | `WorkerCommander` | `internal/worker/interfaces.go:12` | `Compact(ctx, map[string]any) error` / `Clear(ctx) error` / `Rewind(ctx, string) error` | ✅ Clear=session/new, Compact/Rewind=ErrNotImplemented |
| FR-02 | `ControlRequester` | `internal/worker/interfaces.go:6` | `SendControlRequest(ctx, string, map[string]any) (map[string]any, error)` | ✅ usage_update 缓存映射 |
| FR-05 | `InputRecoverer` | `internal/worker/worker.go:141` | `LastInput() string` | ✅ acpConn 新增字段 |
| FR-07 | `ResetResult{ConnReplaced}` | `internal/worker/worker.go` | `ResetContext() (ResetResult, error)` | ✅ 返回 `ConnReplaced: false` |
| FR-07 | `EventInjector` | `internal/worker/worker.go` | `Inject(*events.Envelope)` | ✅ acpConn.Inject 注入 internal_reset 事件 |

### 8.2 数据流验证

| FR | 字段 | Bridge 是否填充 | ACP Worker 是否消费 | 缺口 |
|----|------|----------------|-------------------|------|
| FR-03 | `SessionInfo.MCPConfig` | ✅ `buildWorkerInfo:636-639` | ❌ `Start()` 传 nil | Worker 侧：解析 JSON → `[]any` |
| FR-04 | `SessionInfo.SystemPrompt` | ✅ `injectAgentConfig`（bridge_worker.go:268-276） | ❌ 未消费 | Worker 侧：Start() 缓存 + Input() 首次注入 |
| FR-05 | `acpConn.lastInput` | N/A | N/A | 新增字段 |
| FR-08 | `SessionInfo.ForkSession` | ❌ **未填充**（bool 类型） | ❌ 未消费 | Bridge 侧 + Worker 侧 |
| FR-09 | `SessionInfo.JSONSchema` | ❌ **未填充** | ❌ 未消费 | Bridge 侧 + Worker 侧 |

### 8.3 Bridge 集成路径验证

| 集成点 | 代码位置 | ACP 当前行为 | 改进后 |
|--------|---------|-------------|--------|
| `/compact` 命令 | `worker_cmds.go:136-141` | fallback 到 `w.Input()` | 返回 ErrNotImplemented（WorkerCommander） |
| `/clear` 命令 | `worker_cmds.go:142-146` | fallback 到 `w.Input()` | session/new 新建会话（WorkerCommander） |
| `/rewind` 命令 | `worker_cmds.go:147-151` | fallback 到 `w.Input()` | 返回 ErrNotImplemented（WorkerCommander） |
| `set_model` | `worker_cmds.go:111` | 不支持（无 ControlRequester） | `client.SetSessionModel()`（ControlRequester） |
| `get_context_usage` | `bridge_forward.go:297-299` | 不调用（无 ControlRequester） | 从 mapper 缓存提取（ControlRequester） |
| 崩溃恢复重投递 | `bridge_forward.go:426-430` | 无 lastInput（无 InputRecoverer） | 从 acpConn 读取（InputRecoverer） |
| reset forwardEvents | `bridge.go` | 根据 `ResetResult.ConnReplaced` 决定 | 保持 goroutine（`ConnReplaced: false` + `internal_reset` 事件） |

### 8.4 已发现的不可行项

| 原始提案 | 问题 | 处理 |
|---------|------|------|
| Compact = Terminate + Start | 成本过高，不如直接返回 ErrNotImplemented | 改为 ErrNotImplemented |
| ForkSession = `session.ForkSession != ""` | `ForkSession` 是 `bool` 非 `string` | 修正条件为 `session.ForkSession` |
| `SendControlRequest(ctx, *ControlRequest)` | 实际签名是 `(ctx, string, map[string]any)` | 修正为正确签名 |
| `ResetContext(ctx, *SessionInfo)` | 实际签名是 `ResetContext(ctx) error` | 修正为无参数版本 |
| Worker 包内注册 Prometheus 指标 | 集中式指标模式，通过 worker_type label | 改为集中式 |
| `mcp_status` 返回 MCP 连接信息 | ACP 无 MCP 状态查询方法 | 返回空 map |
| FR-04 Bridge 未注入 SystemPrompt | `injectAgentConfig`（bridge_worker.go:268-276）已实现注入 | 移除 Step 1 Bridge 改动，仅保留 ACP Worker 消费端 |
| FR-02 ControlRequester 键名 snake_case | `MapContextUsageResponse` 期望 camelCase（`maxTokens`/`totalTokens`） | 修正为 camelCase |
| FR-07 `w.mu.Lock()` | BaseWorker 导出字段为 `w.Mu`（大写） | 修正为 `w.Mu.Lock()` |

---

## 9. 实施阶段

### Phase 1: 核心功能补齐（P0）— 33h

| 任务 | 改动范围 | 依赖 | 预估 |
|------|---------|------|------|
| FR-05: InputRecoverer | ACP Worker（conn.go + worker.go） | 无 | 2h |
| FR-03: MCP 配置注入 | ACP Worker（worker.go） | 无 | 3h |
| FR-04: 系统提示词注入 | ACP Worker（worker.go） | 无 | 3h |
| FR-01: WorkerCommander | ACP Worker（worker.go） | 无 | 4h |
| FR-10: cost tracking | ACP Worker（mapper.go） | 无 | 3h |
| FR-02: ControlRequester | ACP Worker（worker.go + mapper.go） | FR-10 | 6h |
| NFR-04: Prometheus 指标 | metrics/metrics.go + ACP Worker | 无 | 4h |
| T-01: Worker 集成测试 | ACP Worker（新建测试文件） | FR-01~05 | 8h |

**里程碑**：ACP Worker 功能与 ClaudeCode Worker 对齐。

### Phase 2: 交互与恢复（P1）— 16h

| 任务 | 改动范围 | 依赖 | 预估 |
|------|---------|------|------|
| FR-06: Question/Elicitation | ACP Worker（worker.go + readLoop） | 无 | 4h |
| FR-07: ResetContext 改进 | ACP Worker（worker.go）+ `ResetResult` + `EventInjector` | FR-01 | 4h |
| U-02: Agent 发现与健康检查 | ACP Worker（worker.go） | 无 | 2h |
| U-03: 错误信息可操作性 | ACP Worker（worker.go） | 无 | 2h |
| T-02/T-03: 扩展测试 | ACP Worker | FR-06~07 | 4h |

**里程碑**：ACP Worker 交互完备，支持所有 ACP v1 方法。

### Phase 3: 体验与扩展（P2）— 20h

| 任务 | 改动范围 | 依赖 | 预估 |
|------|---------|------|------|
| FR-08: ForkSession 桥接 | Bridge + ACP Worker | Bridge 改动 | 4h |
| FR-09: JSON Schema | Bridge + ACP Worker | Bridge 改动 | 3h |
| UX-01~04: UI/UX 渲染 | 飞书/Slack 适配器 | FR-01~10 | 5h |
| EX-01: 多 Agent 增强 | ACP Config | 无 | 2h |
| EX-02: 协议版本前瞻 | ACP Worker | 无 | 2h |
| U-04: 调试模式 | ACP Config + Worker | 无 | 2h |
| T-04: 性能基准测试 | ACP Worker | 无 | 2h |

**里程碑**：ACP Worker 成为 HotPlex 首选 Worker。

---

## 10. 风险与缓解

| 风险 | 概率 | 影响 | 缓解 |
|------|------|------|------|
| ACP 协议 v2 破坏性变更 | 中 | 高 | protocolVersion 协商 + 未知消息 graceful skip |
| Agent 实现差异（Claude vs Hermes vs Codex） | 高 | 中 | initialize 能力检测 → 功能降级 |
| 系统提示词注入被 Agent 忽略 | 中 | 低 | 首次 prompt 后检查 response 是否体现 system prompt |
| Reset 复用进程时 Agent 状态泄漏 | 低 | 高 | session/cancel + IncResetGeneration + forwardEvents 检测 generation |
| mock Agent 测试不覆盖真实 Agent 行为 | 中 | 中 | Phase 1 完成后手动 Hermes E2E 验证 |
| `ForkSession`/`JSONSchema` 需要 Bridge 改动 | 中 | 低 | 降级到 P2，不阻塞核心功能 |

---

## 11. 依赖关系

### 上游依赖

| 依赖 | 说明 | 状态 |
|------|------|------|
| ACP v1 规范 | 定义 JSON-RPC 方法和能力 | ✅ 稳定 |
| Hermes Agent ACP 实现 | 第一个 ACP Agent | ✅ 可用 |
| `proc.Manager` | 进程生命周期管理 | ✅ 已有 |
| `base.BaseWorker` | 共享 Worker 基础设施（含 resetGenerationer） | ✅ 已有 |
| `security.RegisterCommand` | 命令白名单 | ✅ 已有 |
| `agentconfig.BuildSystemPrompt` | B/C 通道系统提示词组装 | ✅ 已有 |

### 下游依赖

| 依赖方 | 说明 | 影响的 FR |
|--------|------|----------|
| 飞书/Slack 适配器 | Plan/ModeUpdate/ToolUpdate 渲染 | UX-01~04 |
| Bridge | ~~SystemPrompt 注入~~（已由 injectAgentConfig 实现） | ~~FR-04~~ |
| Bridge | ForkSession/JSONSchema 传递 | FR-08, FR-09 |
| Bridge | forwardEvents 生命周期 | FR-07（`ResetResult.ConnReplaced` + `internal_reset`） |
| Prometheus | 指标采集 | NFR-04 |

### 已有机制复用（无需新建）

| 机制 | 来源 | FR 复用 |
|------|------|---------|
| `resetGenerationer` | BaseWorker 继承 | FR-07 |
| `worker_type` label 指标 | metrics.go | NFR-04 |
| `DispatchMetadata` | base/metadata.go | FR-06 |
| `WorkerSessionIDHandler` | ACP 已实现 | — |
| `normalizeMCPServers` | FR-03 新增代码 | FR-03 |
