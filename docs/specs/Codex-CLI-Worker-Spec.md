---
type: spec
tags:
  - project/HotPlex
  - worker/codex
  - architecture/integration
date: 2026-05-18
status: draft
progress: 0
estimated_hours: 16
---

# Codex CLI Worker 集成规格

> **目标**：为 HotPlex 添加第三个 Worker 类型 —— OpenAI Codex CLI (`@openai/codex`)，使其成为与 Claude Code、OpenCode Server 并列的 AI Coding Agent 后端。

---

## 1. 背景与动机

### 1.1 现状

HotPlex 当前支持两个 Worker：

| Worker | 进程模型 | Transport | 协议 |
|--------|---------|-----------|------|
| Claude Code | per-session 持久进程 | stdio | NDJSON (stream-json) |
| OpenCode Server | 共享单例进程 | HTTP REST + SSE | JSON (BusEvent) |

用户希望接入 OpenAI Codex CLI 作为第三个 Worker，为终端用户提供更多模型选择（GPT-5 系列、o4 等）。

### 1.2 Codex CLI 概述

OpenAI Codex CLI (`@openai/codex`) 是 OpenAI 官方出品的本地 AI Coding Agent：

| 属性 | 值 |
|------|---|
| **仓库** | [github.com/openai/codex](https://github.com/openai/codex) — 83K ★, Apache-2.0 |
| **语言** | Rust (96.2%) |
| **安装** | `npm i -g @openai/codex` 或 `brew install --cask codex` |
| **二进制** | `codex`（单一静态二进制） |
| **当前版本** | v0.130.0 (2026-05) |
| **非交互模式** | `codex exec --json "<prompt>"` |
| **输出格式** | JSONL (每行一个 JSON 事件，stdout) |
| **认证** | `CODEX_API_KEY` 环境变量（for exec）或 `~/.codex/auth.json` |
| **会话管理** | SQLite-backed Threads，`resume --last` 续接 |
| **沙箱** | `read-only` / `workspace-write` / `danger-full-access` |

### 1.3 验证状态

| 验证项 | 状态 | 详情 |
|--------|------|------|
| 二进制可用 | ✅ | `/opt/homebrew/bin/codex` v0.130.0 |
| 认证就绪 | ✅ | `codex login status` → API key 已配置 |
| `codex exec --json` | ✅ | 确认输出 JSONL 事件流 |
| 事件格式 | ✅ | 通过源码级分析确认 11 种 TurnItem + legacy EventMsg |
| 沙箱模式 | ✅ | `workspace-write` + 审批策略 `never` 可用于自动化 |

---

## 2. 架构设计

### 2.1 集成模式选择

Codex CLI 的 `codex exec` 是 **one-shot per prompt** 模式（与 Claude Code 的持久进程不同）：每次调用传入一个 prompt，Codex 处理完成后输出 JSONL 事件流并退出。

```
HotPlex Bridge
  └→ worker.NewWorker(TypeCodexCLI)
    └→ Start(ctx, sessionInfo)
      └→ proc.Manager.Start("codex", ["exec", "--json", "--sandbox", "workspace-write",
            "--ask-for-approval", "never", "--cd", projectDir,
            "--ephemeral", "-m", model, prompt], env, projectDir)
      └→ go readOutput() — 从 stdout 逐行读取 JSONL
        └→ ParseLine → Map → trySend(AEP Envelope)
      └→ proc.Manager.Wait() — 等待进程退出
```

**对比**：

| 维度 | Claude Code Worker | Codex CLI Worker |
|------|-------------------|-----------------|
| 进程生命周期 | 长驻（多轮复用） | 单次（执行即退出） |
| Input 注入 | stdin NDJSON 流 | CLI 参数（一次性） |
| 多轮对话 | stdin 逐轮发送 | `resume --last` 重新 spawn |
| 流式输出 | stdout NDJSON (stream-json) | stdout JSONL (item-based) |
| 会话 ID | `--session-id` flag | SQLite Thread ID（内部） |
| 嵌入基类 | `*base.BaseWorker` + `*base.Conn` | `*base.BaseWorker`（stdin conn 仅用于控制响应） |

### 2.2 架构定位

```
Gateway (AEP v1)  ──协议桥接──►  Codex CLI Worker  ──JSONL stdout──►  codex exec  ──API──►  OpenAI Models
                                         │
                             Gateway session ID ↔ Codex Thread ID（可选映射）
```

### 2.3 Transport × Protocol × Lifecycle

| 维度 | Codex CLI Worker |
|------|-----------------|
| **Transport** | stdio（stdout JSONL 读取，stdin 用于 prompt 注入） |
| **Protocol** | JSONL item-based events（参考 §3） |
| **Lifecycle** | ephemeral（per-turn spawn，`--ephemeral` 默认） |

---

## 3. 协议映射

### 3.1 Codex JSONL 事件类型

Codex `exec --json` 输出以下事件（基于 `codex-rs/protocol/src/items.rs` 源码分析）：

#### 顶层事件

| 事件 type | 说明 |
|-----------|------|
| `thread.started` | 会话线程创建，含 `thread_id` |
| `turn.started` | 新一轮开始 |
| `turn.completed` | 本轮完成，含 `usage`（token 统计） |
| `turn.failed` | 本轮失败 |
| `error` | 通用错误，含 `message` |

#### Item 事件（`item.started` / `item.completed`）

Items 通过 `item.started` → `item.completed` 配对出现，`item.type` 决定内部结构：

| item.type | 关键字段 | 说明 |
|-----------|---------|------|
| `agent_message` | `text` (string), `phase` (optional) | Agent 最终回复文本 |
| `reasoning` | `summary_text` (string[]), `raw_content` (string[]) | 推理/思考过程 |
| `command_execution` | `command`, `cwd`, `stdout`, `stderr`, `exit_code` | Shell 命令执行 |
| `file_change` | `changes` (map[path]FileChange), `status`, `stdout`, `stderr` | 文件修改（含 patch apply 状态） |
| `mcp_tool_call` | `server`, `tool`, `arguments`, `result`, `error`, `duration` | MCP 工具调用 |
| `web_search` | `query`, `action` | 网页搜索 |
| `image_view` | `path` | 图片查看 |
| `image_generation` | `status`, `revised_prompt`, `result`, `saved_path` | 图片生成 |
| `plan` | `text` | 计划大纲 |
| `context_compaction` | （无额外字段） | 上下文压缩 |

#### 事件流时序示例

```
thread.started → turn.started
  → item.started (reasoning)
  → item.completed (reasoning: {summary_text: [...], raw_content: [...]})
  → item.started (command_execution)
  → item.completed (command_execution: {command: "ls", stdout: "...", exit_code: 0})
  → item.started (agent_message)
  → item.completed (agent_message: {text: "当前目录包含..."})
→ turn.completed ({usage: {input_tokens: 1500, output_tokens: 200}})
```

### 3.2 AEP 事件映射

| Codex 事件 | AEP Kind | AEP Data Type | 映射逻辑 |
|-----------|----------|---------------|---------|
| `item.completed(agent_message)` | `MessageDelta` | `MessageDeltaData{MessageID, Content: item.text}` | 提取 text 字段 |
| `item.completed(reasoning)` | `Reasoning` | `ReasoningData{Content: strings.Join(summary_text, "\n")}` | 合并 summary_text 数组 |
| `item.started(command_execution)` | `ToolCall` | `ToolCallData{ID: item.id, Name: "shell", Input: {command, cwd}}` | 封装为 shell tool call |
| `item.completed(command_execution)` | `ToolResult` | `ToolResultData{ID: item.id, Output: stdout, Error: stderr}` | 映射执行结果 |
| `item.started(file_change)` | `ToolCall` | `ToolCallData{ID: item.id, Name: "file_edit", Input: {changes}}` | 封装为 file edit tool call |
| `item.completed(file_change)` | `ToolResult` | `ToolResultData{ID: item.id, Output: status, Error: stderr}` | 映射 patch apply 结果 |
| `item.started(mcp_tool_call)` | `ToolCall` | `ToolCallData{ID: item.id, Name: "mcp:"+tool, Input: arguments}` | 封装为 MCP tool call |
| `item.completed(mcp_tool_call)` | `ToolResult` | `ToolResultData{ID: item.id, Output: result, Error: error.message}` | 映射 MCP 结果 |
| `turn.completed` | `Done` | `DoneData{Success: true, Stats: {TokensIn, TokensOut}}` | 提取 usage |
| `turn.failed` / `error` | `Error` + `Done` | `ErrorData{Code, Message}` + `DoneData{Success: false}` | 错误终止 |
| `item.completed(plan)` | `State` | `StateData{State: "planning", Metadata: {plan_text}}` | 可选暴露 |
| `item.completed(image_generation)` | `ToolResult` | `ToolResultData{ID: item.id, Output: saved_path}` | 映射生成结果 |

### 3.3 流式输出策略

`codex exec --json` 当前不支持逐 token 的 delta 事件（所有 `agent_message` 文本在 `item.completed` 时一次性输出）。缓解方案：

1. **多 item 自然分段**：reasoning → command_execution → file_change → agent_message 的序列化自然产生进度感
2. **reasoning 预曝光**：在 agent_message 到达前，reasoning 事件提供"思考过程"可见性
3. **未来增强**：若 Codex 后续版本支持 `item.updated` 增量事件，映射到 `MessageDelta` 即可实现真正的流式输出

---

## 4. 修改范围

| # | 文件 | 改动类型 | 说明 |
|:--|:-----|:---------|:-----|
| 1 | `internal/worker/worker.go` | 修改 | 添加 `TypeCodexCLI WorkerType = "codex_cli"` 常量 |
| 2 | `internal/worker/codexcli/worker.go` | **新建** | Worker 结构体（嵌入 `*base.BaseWorker`）、`init()` 注册、`Start`/`Input`/`Resume`/`ResetContext` |
| 3 | `internal/worker/codexcli/parser.go` | **新建** | JSONL 逐行解析 → 内部 `CodexEvent` 类型 |
| 4 | `internal/worker/codexcli/mapper.go` | **新建** | `CodexEvent` → AEP `Envelope` 映射 |
| 5 | `internal/worker/codexcli/types.go` | **新建** | Codex 特定事件类型定义（TurnItem, AgentMessageItem 等） |
| 6 | `internal/worker/codexcli/control.go` | **新建** | 控制请求处理（SendControlRequest, 审批自动应答） |
| 7 | `cmd/hotplex/main.go` | 修改 | 添加 `_ ".../worker/codexcli"` 空白导入 |
| 8 | `cmd/hotplex/gateway_run.go` | 修改 | 添加 `codexcli.InitConfig(cfg.Worker.CodexCLI)` 启动初始化 |
| 9 | `internal/config/config.go` | 修改 | 添加 `CodexCLI CodexCLIConfig` 字段到 `WorkerConfig`、定义 `CodexCLIConfig` 结构体、添加 Default() 默认值 |
| 10 | `configs/config.yaml` | 修改 | 添加 `codex_cli:` 配置节示例 |
| 11 | `internal/security/tool.go` | 修改 | 将 `codex` 命令注册到安全命令白名单 |
| 12 | `internal/worker/codexcli/worker_test.go` | **新建** | Worker 基础测试 |

**不修改**：`internal/gateway/bridge.go`、`internal/gateway/bridge_worker.go`、`internal/gateway/handler.go`、`internal/session/key.go`、`internal/messaging/bridge.go` — 这些层已通过 `WorkerFactory` 接口完全类型擦除，无需变更。

---

## 5. 详细设计

### Step 1: Worker 类型常量

**文件**：`internal/worker/worker.go`（第 72-77 行附近）

```go
const (
    TypeClaudeCode  WorkerType = "claude_code"
    TypeOpenCodeSrv WorkerType = "opencode_server"
    TypeCodexCLI    WorkerType = "codex_cli"          // 新增
    TypeACP          WorkerType = "acp"
    TypeUnknown     WorkerType = "unknown"
)
```

### Step 2: CodexCLIConfig 配置结构

**文件**：`internal/config/config.go`

```go
// CodexCLIConfig Codex CLI Worker 特定配置
type CodexCLIConfig struct {
    Command       string        `mapstructure:"command"`        // codex 二进制路径，默认 "codex"
    Model         string        `mapstructure:"model"`          // 模型名称，默认空（使用 Codex 配置）
    Sandbox       string        `mapstructure:"sandbox"`        // 沙箱模式，默认 "workspace-write"
    ApprovalMode  string        `mapstructure:"approval_mode"`  // 审批模式，默认 "never"
    Ephemeral     bool          `mapstructure:"ephemeral"`      // 不持久化会话，默认 true
    StartupTimeout time.Duration `mapstructure:"startup_timeout"` // 启动超时，默认 30s
}
```

**WorkerConfig 新增字段**：

```go
type WorkerConfig struct {
    // ... 现有字段
    CodexCLI CodexCLIConfig `mapstructure:"codex_cli"`  // 新增
}
```

**默认值**（`Default()` 函数）：

```go
CodexCLI: CodexCLIConfig{
    Command:        "codex",
    Sandbox:        "workspace-write",
    ApprovalMode:   "never",
    Ephemeral:      true,
    StartupTimeout: 30 * time.Second,
},
```

**YAML 示例**（`configs/config.yaml`）：

```yaml
worker:
  codex_cli:
    command: "codex"
    model: ""                    # 使用 Codex 默认模型
    sandbox: "workspace-write"
    approval_mode: "never"
    ephemeral: true
    startup_timeout: 30s
```

### Step 3: Worker 注册

**文件**：`internal/worker/codexcli/worker.go`

```go
package codexcli

import (
    "log/slog"
    "github.com/hrygo/hotplex/internal/worker"
    "github.com/hrygo/hotplex/internal/worker/base"
)

func init() {
    worker.Register(worker.TypeCodexCLI, func() (worker.Worker, error) {
        return &Worker{
            BaseWorker: base.NewBaseWorker(slog.Default(), nil),
        }, nil
    })
}

// 编译时接口检查
var _ worker.Worker = (*Worker)(nil)
```

**空白导入**（`cmd/hotplex/main.go` 第 9-10 行附近）：

```go
_ "github.com/hrygo/hotplex/internal/worker/codexcli"  // 新增
```

### Step 4: Worker 结构体

```go
type Worker struct {
    *base.BaseWorker

    cfg     CodexCLIConfig
    mu      sync.Mutex
    started bool
}
```

**接口实现策略**：

| 接口方法 | 实现策略 |
|---------|---------|
| `Type()` | 返回 `worker.TypeCodexCLI` |
| `SupportsResume()` | `true`（通过 `resume --last` 重新 spawn） |
| `SupportsStreaming()` | `true`（item 事件序列化输出） |
| `SupportsTools()` | `true`（command_execution, file_change, mcp_tool_call） |
| `EnvBlocklist()` | 返回 `base.DefaultEnvBlocklist` + `"CODEX_"` 前缀 |
| `SessionStoreDir()` | `""`（使用 `--ephemeral` 模式，无会话文件） |
| `MaxTurns()` | `0`（无限制） |
| `Modalities()` | `["text", "code"]` |

### Step 5: Start() — 进程启动与输出读取

```go
func (w *Worker) Start(ctx context.Context, session worker.SessionInfo) error {
    w.mu.Lock()
    defer w.mu.Unlock()

    if w.started {
        return fmt.Errorf("codex worker already started")
    }

    // 1. 构建命令行参数
    args := w.buildArgs(session)

    // 2. 构建环境变量
    env := base.BuildEnv(w.Log, session, w.cfg.Command,
        []string{"CODEX_API_KEY"}, // 注入 API key
        w.EnvBlocklist(),
    )

    // 3. 启动进程
    stdin, stdout, _, err := w.Proc.Start(ctx, w.cfg.Command, args, env, session.ProjectDir)
    if err != nil {
        return fmt.Errorf("start codex: %w", err)
    }

    // 4. 创建 stdin conn（仅用于控制响应）
    conn := base.NewConn(w.Log, stdin, session.OwnerID, session.SessionID)
    w.SetConn(conn)

    // 5. 启动输出读取 goroutine
    childCtx, cancel := context.WithCancel(ctx)
    go w.readOutput(childCtx, cancel, stdout, &session)

    w.started = true
    return nil
}

func (w *Worker) buildArgs(session worker.SessionInfo) []string {
    args := []string{
        "exec", "--json",
        "--sandbox", w.cfg.Sandbox,
        "--ask-for-approval", w.cfg.ApprovalMode,
        "--cd", session.ProjectDir,
    }

    if w.cfg.Ephemeral {
        args = append(args, "--ephemeral")
    }

    if w.cfg.Model != "" {
        args = append(args, "-m", w.cfg.Model)
    }

    if session.ResumeSessionID != "" {
        args = append(args, "resume", session.ResumeSessionID)
    }

    // Prompt 作为最后的位置参数
    if session.Prompt != "" {
        args = append(args, session.Prompt)
    }

    return args
}
```

### Step 6: readOutput() — 事件循环

```go
func (w *Worker) readOutput(ctx context.Context, cancel context.CancelFunc,
    stdout io.Reader, session *worker.SessionInfo) {

    defer cancel()
    defer func() {
        if r := recover(); r != nil {
            w.Log.Error("codex readOutput panic", "error", r)
        }
    }()

    scanner := bufio.NewScanner(stdout)
    scanner.Buffer(make([]byte, 64*1024), 10*1024*1024) // 64KB → 10MB

    var (
        parser     = NewParser()
        mapper     = NewMapper(session.SessionID)
        turnFailed bool
    )

    for scanner.Scan() {
        select {
        case <-ctx.Done():
            return
        default:
        }

        line := scanner.Text()
        if line == "" {
            continue
        }

        // 解析 JSONL
        event, err := parser.ParseLine(line)
        if err != nil {
            w.Log.Warn("codex parse error", "line", line, "error", err)
            continue
        }

        // 映射到 AEP
        envelopes := mapper.Map(event)
        for _, env := range envelopes {
            if env == nil {
                continue
            }

            // 背压感知发送
            conn := w.Conn()
            if conn == nil {
                return
            }
            w.trySend(conn, env)
        }

        // 检测终止
        switch event.Type {
        case EventTurnFailed:
            turnFailed = true
        case EventTurnCompleted:
            return
        case EventError:
            if !turnFailed {
                return
            }
        }
    }

    if err := scanner.Err(); err != nil {
        w.Log.Error("codex stdout read error", "error", err)
    }
}

// trySend 非阻塞发送，关键事件（Done/Error）使用阻塞发送
func (w *Worker) trySend(conn worker.SessionConn, env *events.Envelope) {
    switch env.Kind {
    case events.KindDone, events.KindError, events.KindState:
        _ = conn.Send(env) // 阻塞：必须送达
    default:
        _ = conn.TrySend(env) // 非阻塞：背压下丢弃 delta
    }
}
```

### Step 7: Parser — JSONL 解析

**文件**：`internal/worker/codexcli/parser.go`

```go
type CodexEvent struct {
    Type string          `json:"type"`
    Item *CodexItem      `json:"item,omitempty"`      // item.* 事件
    ThreadID string      `json:"thread_id,omitempty"` // thread.started
    Usage   *CodexUsage  `json:"usage,omitempty"`     // turn.completed
    Message string       `json:"message,omitempty"`   // error
}

type CodexItem struct {
    ID   string          `json:"id"`
    Type string          `json:"type"`            // agent_message, reasoning, command_execution, etc.
    Text string          `json:"text,omitempty"`
    SummaryText []string `json:"summary_text,omitempty"`
    RawContent  []string `json:"raw_content,omitempty"`
    Command     string   `json:"command,omitempty"`
    CWD         string   `json:"cwd,omitempty"`
    Stdout      string   `json:"stdout,omitempty"`
    Stderr      string   `json:"stderr,omitempty"`
    ExitCode    int      `json:"exit_code,omitempty"`
    Changes     map[string]CodexFileChange `json:"changes,omitempty"`
    Status      string   `json:"status,omitempty"`
    Server      string   `json:"server,omitempty"`    // MCP
    Tool        string   `json:"tool,omitempty"`       // MCP
    Arguments   json.RawMessage `json:"arguments,omitempty"` // MCP
    Result      json.RawMessage `json:"result,omitempty"`    // MCP
    Error       *CodexItemError `json:"error,omitempty"`     // MCP
    Query       string   `json:"query,omitempty"`      // web_search
    SavedPath   string   `json:"saved_path,omitempty"` // image_generation
}

type CodexUsage struct {
    InputTokens  int `json:"input_tokens"`
    OutputTokens int `json:"output_tokens"`
}

type CodexFileChange struct {
    // ... 文件变更详情
}

type CodexItemError struct {
    Message string `json:"message"`
}

// 内部事件类型常量（用于后续映射判断）
const (
    EventThreadStarted   = "thread.started"
    EventTurnStarted     = "turn.started"
    EventTurnCompleted   = "turn.completed"
    EventTurnFailed      = "turn.failed"
    EventItemStarted     = "item.started"
    EventItemCompleted   = "item.completed"
    EventError           = "error"
)
```

### Step 8: Mapper — AEP 映射

**文件**：`internal/worker/codexcli/mapper.go`

核心映射逻辑（参照 §3.2 映射表）：

```go
type Mapper struct {
    sessionID     string
    messageIDBase string
    seq           atomic.Int64
    pendingTools  map[string]string // item.id → tool call ID
}

func (m *Mapper) Map(event *CodexEvent) []*events.Envelope {
    switch event.Type {
    case EventItemStarted:
        return m.mapItemStarted(event.Item)
    case EventItemCompleted:
        return m.mapItemCompleted(event.Item)
    case EventTurnCompleted:
        return m.mapTurnCompleted(event.Usage)
    case EventTurnFailed:
        return []*events.Envelope{
            events.NewEnvelope(events.KindError, events.ErrorData{
                Code: "TURN_FAILED", Message: "turn failed",
            }),
            events.NewEnvelope(events.KindDone, events.DoneData{Success: false}),
        }
    case EventError:
        return []*events.Envelope{
            events.NewEnvelope(events.KindError, events.ErrorData{
                Code: "CODEX_ERROR", Message: event.Message,
            }),
            events.NewEnvelope(events.KindDone, events.DoneData{Success: false}),
        }
    }
    return nil
}

func (m *Mapper) mapItemCompleted(item *CodexItem) []*events.Envelope {
    switch item.Type {
    case "agent_message":
        return []*events.Envelope{
            events.NewEnvelope(events.KindMessageDelta, events.MessageDeltaData{
                MessageID: m.nextMessageID(),
                Content:   item.Text,
            }),
        }
    case "reasoning":
        return []*events.Envelope{
            events.NewEnvelope(events.KindReasoning, events.ReasoningData{
                Content: strings.Join(item.SummaryText, "\n"),
            }),
        }
    case "command_execution":
        return []*events.Envelope{
            events.NewEnvelope(events.KindToolResult, events.ToolResultData{
                ID:     item.ID,
                Output: item.Stdout,
                Error:  item.Stderr,
            }),
        }
    case "file_change":
        status := "completed"
        if item.Status != "completed" {
            status = "failed"
        }
        return []*events.Envelope{
            events.NewEnvelope(events.KindToolResult, events.ToolResultData{
                ID:     item.ID,
                Output: status,
                Error:  item.Stderr,
            }),
        }
    case "mcp_tool_call":
        return []*events.Envelope{
            events.NewEnvelope(events.KindToolResult, events.ToolResultData{
                ID: item.ID,
                Output: string(item.Result),
                Error:  item.Error.Message,
            }),
        }
    }
    return nil
}
```

### Step 9: Input() — 用户输入

Codex `exec` 是 one-shot 模式：prompt 在启动时传入，不支持运行时 stdin 消息注入。`Input()` 的实现策略：

```go
func (w *Worker) Input(ctx context.Context, content string, metadata map[string]any) error {
    // 1. 处理控制响应（permission/question/elicitation）
    if err := base.DispatchMetadata(ctx, metadata, w); err != nil {
        return err
    }

    // 2. One-shot 模式下，Input 在 Start 之后不可用
    //    多轮对话由上层 Bridge 通过 Resume 处理（重新 spawn）
    return fmt.Errorf("%w: codex exec is one-shot per process; use Resume for follow-up",
        worker.ErrInputRejected)
}
```

**多轮对话流程**：

```
Turn 1: Bridge → Start(prompt="分析这个项目") → codex exec → stdout JSONL → Done
Turn 2: Bridge → Resume(ResumeSessionID=threadID, prompt="继续优化") → codex exec resume <id> → stdout JSONL → Done
Turn N: ...
```

HotPlex 在 `turn.completed` 事件中提取 `thread_id` 并存储，后续使用该 ID 调用 `resume --last` 或 `resume <thread_id>`。

### Step 10: Resume() — 会话恢复

```go
func (w *Worker) Resume(ctx context.Context, session worker.SessionInfo) error {
    // Resume 本质上与 Start 相同，但使用 resume 子命令
    // 由 buildArgs 根据 session.ResumeSessionID 自动添加 resume 参数
    return w.Start(ctx, session)
}
```

### Step 11: ResetContext() — 上下文重置

```go
func (w *Worker) ResetContext(ctx context.Context) error {
    // 终止当前进程
    if err := w.BaseWorker.Terminate(ctx); err != nil {
        w.Log.Warn("codex reset: terminate failed", "error", err)
    }
    w.mu.Lock()
    w.started = false
    w.mu.Unlock()
    // 下一轮 Start 会创建全新进程
    return nil
}
```

### Step 12: 安全命令白名单

**文件**：`internal/security/tool.go`

```go
func init() {
    // ... 现有注册
    RegisterCommand("codex") // 新增
}
```

### Step 13: 启动初始化

**文件**：`cmd/hotplex/gateway_run.go`（第 230-231 行附近）

```go
codexcli.InitConfig(cfg.Worker.CodexCLI)  // 新增
```

```go
// internal/worker/codexcli/config.go
var globalConfig atomic.Pointer[CodexCLIConfig]

func InitConfig(cfg CodexCLIConfig) {
    globalConfig.Store(&cfg)
}

func GetConfig() CodexCLIConfig {
    if p := globalConfig.Load(); p != nil {
        return *p
    }
    return CodexCLIConfig{} // 返回零值，安全默认
}
```

---

## 6. 错误处理

### 6.1 错误分类

| 场景 | 错误类型 | Bridge 处理 |
|------|---------|------------|
| codex 二进制未找到 | `exec.ErrNotFound` | 标记 Worker Unavailable |
| 认证失败 | 进程 stderr 含 "401 Unauthorized" | `WorkerError(ErrKindUnavailable)` |
| API 限流/熔断 | 进程 stderr 含 "503" / "rate limit" | 触发 LLMRetryController |
| 沙箱拒绝 | JSONL `turn.failed` + 错误消息 | 映射到 AEP Error |
| 进程崩溃 | `proc.Manager.Wait()` 非零退出码 | `WorkerError(ErrKindUnavailable)` |
| 超时（无输出超时） | context deadline | Terminate → Kill → WorkerError |

### 6.2 重试策略

复用现有 `LLMRetryController`（`internal/gateway/llm_retry.go`），对于以下可重试错误自动重建 Worker：
- 进程崩溃（exit code ≠ 0）
- API 临时不可用（503）
- 网络超时

```go
// bridge_forward.go 中已有的重试逻辑无需修改
// WorkerError(ErrKindUnavailable) 自动触发 createAndLaunchWorker
```

### 6.3 错误消息增强

Codex stderr 包含丰富的错误上下文，应在 WorkerError 中保留：

```go
func (w *Worker) Wait() (int, error) {
    code, err := w.BaseWorker.Wait()
    if err != nil || code != 0 {
        // stderr 内容由 proc.Manager 自动捕获
        return code, worker.NewWorkerError(
            worker.ErrKindUnavailable,
            fmt.Errorf("codex exited with code %d", code),
        )
    }
    return code, nil
}
```

---

## 7. 实施顺序

```
Phase 1 — 骨架（1h）
  Step 1: WorkerType 常量
  Step 2: CodexCLIConfig 结构体 + Default()
  Step 3: init() 注册 + 空白导入
  Step 12: 安全命令白名单
  Step 13: InitConfig 调用
  └→ make build 验证编译通过

Phase 2 — 核心实现（6h）
  Step 4: Worker 结构体 + Capabilities 接口
  Step 5: Start() 进程启动 + 参数构建
  Step 6: readOutput() 事件循环
  └→ 手动启动 codex exec，验证 stdout 捕获

Phase 3 — 协议映射（4h）
  Step 7: Parser（JSONL → CodexEvent）
  Step 8: Mapper（CodexEvent → AEP Envelope）
  Step 10: Resume() 会话恢复
  └→ 实际调用 codex exec，端到端验证 AEP 事件流

Phase 4 — 完善（3h）
  Step 9: Input() 控制响应处理
  Step 11: ResetContext()
  错误分类 + 重试集成
  └→ make check 全部通过

Phase 5 — 测试与文档（2h）
  单元测试（parser/mapper）
  集成验证（实际 prompt 测试）
  更新 README.md spec 索引
```

---

## 8. 风险与缓解

| 风险 | 概率 | 影响 | 缓解措施 |
|------|------|------|---------|
| **无流式 delta**：agent_message 文本一次性输出，用户体验不如 Claude Code 逐 token 流式 | 高 | 中 — 响应延迟感增加 | reasoning 事件提供中间反馈；future: 监控 Codex 是否增加 `item.updated` |
| **One-shot 开销**：每次对话轮次需要重新 spawn 进程 | 高 | 低 — Rust 二进制启动 < 200ms | `--ephemeral` 跳过文件 I/O；可考虑 long-lived `mcp-server` 作为 v2 |
| **API key 管理**：`CODEX_API_KEY` 与 `OPENAI_API_KEY` 不同，容易混淆 | 中 | 高 — 认证失败导致 Worker 不可用 | 在 BuildEnv 中明确设置 `CODEX_API_KEY`；文档说明区别 |
| **Git repo 依赖**：非 Git 目录需 `--skip-git-repo-check` | 中 | 低 — 仅特殊场景 | Session 创建时自动初始化 git 仓库（已有逻辑） |
| **Codex CLI 快速迭代**：789 releases，API 可能变化 | 低 | 高 — 适配器可能需要跟进 | 解析器对未知事件 type 做忽略处理；监控 Codex changelog |
| **线程 ID 泄露**：`thread.started` 包含内部 SQLite ID | 低 | 低 — 仅内部使用 | 不暴露给最终用户 |
| **审批自动拒绝**：`--ask-for-approval never` 可能导致危险操作 | 低 | 高 — 安全风险 | `--sandbox workspace-write` 限制写入范围；可配置为 `on-request` |

---

## 9. 开放问题

| # | 问题 | 优先级 | 解决方案 |
|---|------|--------|---------|
| 1 | `item.updated` 是否存在（流式 delta）？ | 中 | 实际测试验证；若存在则映射到 MessageDelta |
| 2 | `resume --last` 在并发场景下的可靠性？ | 中 | 测试验证；可能需要存储显式 thread_id |
| 3 | `codex mcp-server` 作为 v2 长驻进程方案？ | 低 | 先验证 exec 方案，mcp-server 作为后续优化 |
| 4 | 是否需要支持 `--output-schema` 结构化输出？ | 低 | 先不实现，按需添加 |

---

## 10. 验收标准

| # | 标准 | 验证方式 |
|---|------|---------|
| AC-1 | `make build` 编译成功，无错误 | CI |
| AC-2 | `codex_cli` worker type 出现在 `RegisteredTypes()` | 单元测试 |
| AC-3 | `Start()` → `readOutput()` 正确解析 `agent_message` 事件 | 单元测试（mock stdout） |
| AC-4 | reasoning → command_execution → agent_message → turn.completed 完整流程 | 集成测试（真实 codex 调用） |
| AC-5 | `turn.failed` 正确映射到 AEP Error + Done | 单元测试 |
| AC-6 | `Resume()` 通过 `resume --last` 继续对话 | 集成测试 |
| AC-7 | `Terminate()` + `Kill()` 正确清理进程 | 单元测试 |
| AC-8 | `CODEX_API_KEY` 环境变量正确注入 | 集成测试 |
| AC-9 | 通过飞书/Slack 发送消息，Codex Worker 正常响应 | 手动 QA |
| AC-10 | 错误场景（无网络、API 限流）正确重试 | 手动 QA |
