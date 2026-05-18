---
type: spec
tags:
  - project/HotPlex
  - worker/codex
  - architecture/integration
  - persistent-process
date: 2026-05-18
status: draft
progress: 0
estimated_hours: 20
depends_on:
  - docs/specs/Codex-CLI-Worker-Spec.md
---

# Codex App-Server Worker 集成规格（v2 持久化模式）

> **目标**：将 Codex CLI Worker 从 one-shot `codex exec` 模式升级为 `codex app-server` 持久化进程模式，实现真正的流式输出、零冷启动多轮复用，以及与 OpenCode Server 一致的 Singleton 架构。

---

## 1. 背景与动机

### 1.1 现状

Codex CLI Worker v1（`internal/worker/codexcli/`）使用 `codex exec --json` 作为 one-shot 子进程：

| 维度 | v1 (`codex exec`) | 问题 |
|------|-------------------|------|
| 进程生命周期 | 每 turn 新 spawn | 冷启动 ~200ms/turn |
| 流式输出 | 一次性 JSONL（无 delta） | 用户体验不如 Claude Code 逐 token 流式 |
| 多轮对话 | `resume --last` 重新 spawn | 每次重建上下文，浪费 token |
| 审批交互 | `--ask-for-approval never` | 无法处理用户审批 |
| 架构模式 | Per-session 进程 | 与 OCS singleton 模式不一致 |

### 1.2 Codex App-Server 概述

`codex app-server` 是 OpenAI Codex CLI 的**官方持久化服务器模式**，被 Codex VS Code 扩展、桌面应用和 Web 界面内部使用：

| 属性 | 值 |
|------|---|
| **二进制** | `codex app-server` |
| **传输** | stdio (默认) / WebSocket (实验性) / Unix Socket |
| **协议** | JSON-RPC 2.0（双向，请求/响应 + 服务端推送通知） |
| **成熟度** | **稳定**（VS Code 扩展自 v0.1.0 起使用） |
| **流式支持** | ✅ `item/agentMessage/delta` 逐 token 推送 |
| **会话管理** | SQLite 持久化 Thread，`thread/start` / `thread/resume` |
| **审批交互** | ✅ 双向 `serverRequest/approval` |
| **多路复用** | 单进程服务多个 Thread，按 `threadId` 路由事件 |

### 1.3 升级价值

| 改进 | v1 → v2 |
|------|---------|
| **真正流式输出** | `item/agentMessage/delta` → AEP `message.delta` 逐 token |
| **零冷启动** | 进程常驻，turn 切换 < 10ms |
| **会话持久化** | Thread 自动 SQLite 保存，resume 无需额外处理 |
| **审批交互** | 双向协议，不再需要 `--ask-for-approval never` |
| **架构一致** | 与 OpenCode Server 完全相同的 Singleton 模式 |

---

## 2. 架构设计

### 2.1 集成模式选择

对标 OpenCode Server 的 `SingletonProcessManager` 模式：一个长驻 `codex app-server` 进程服务所有 session，通过引用计数管理生命周期。

```
HotPlex Gateway
  └── codexcli.InitSingleton(log, cfg.Worker.CodexCLI)
        └── CodexAppServerManager (atomic.Pointer 单例)
              ├── Acquire() → 懒启动 codex app-server (stdio 模式)
              │     ├── proc.Start("codex", ["app-server"])
              │     ├── 发送 initialize → 等待 initialized 响应
              │     └── go readNotifications() + go monitorProcess()
              ├── Release() → 引用计数 -1, 空闲 30m 排空
              ├── Subscribe(threadId) → 通知通道
              └── Shutdown() → 杀进程 + 清理

Worker (per-session 轻量适配器)
  ├── Start() → manager.Acquire() → thread/start → Subscribe(threadId)
  ├── Input(content) → turn/start {threadId, input}
  ├── Terminate() → thread/unsubscribe → manager.Release()
  └── 流式接收: item/started → item/agentMessage/delta → item/completed → turn/completed
```

### 2.2 传输选择：stdio vs WebSocket

| | stdio (推荐) | WebSocket (实验性) |
|---|---|---|
| 复杂度 | 低（子进程管道，与现有 proc.Manager 一致） | 中（需端口发现 + HTTP 客户端） |
| 并发 | stdin 写锁序列化请求 | 天然支持多连接 |
| 成熟度 | 稳定 | experimental / unsupported |
| 故障恢复 | proc.Manager 管理，PGID 隔离 | 需自行处理断连重连 |

**选择 stdio 模式**：与当前 `codex exec` 和 Claude Code Worker 使用相同的 `proc.Manager` 子进程管道，无需端口管理。

### 2.3 架构定位

```
Gateway (AEP v1) ──协议桥接──► Codex AppServer Worker ──JSON-RPC stdio──► codex app-server ──API──► OpenAI Models
                                      │                                        │
                              manager.Acquire()                         Thread Manager
                              thread/start                              Turn Lifecycle
                              turn/start                                Item Streaming
```

---

## 3. 协议映射

### 3.1 App-Server JSON-RPC 方法（客户端 → 服务端）

| 方法 | 参数 | 返回 | 说明 |
|------|------|------|------|
| `initialize` | `{clientInfo: {name, title, version}}` | `{userAgent, codexHome, platformFamily}` | 连接握手（必须首发） |
| `initialized` | `{}` | 无（通知） | 握手确认 |
| `thread/start` | `{model, cwd, sandbox, personality, ephemeral}` | `{thread: {id, createdAt}}` | 创建新线程 |
| `thread/resume` | `{threadId}` | `{thread: {id, turns, ...}}` | 恢复已有线程 |
| `thread/unsubscribe` | `{threadId}` | `{status: "unsubscribed"}` | 取消订阅 |
| `turn/start` | `{threadId, input: [{type: "text", text}]}` | `{turn: {id, status}}` | 发送用户输入 |
| `turn/interrupt` | `{threadId, turnId}` | `{}` | 中断当前 turn |

### 3.2 App-Server 推送通知（服务端 → 客户端）

| 通知 method | params 关键字段 | 说明 |
|-------------|----------------|------|
| `thread/started` | `{thread: {id, status}}` | 线程已创建并加载 |
| `thread/status/changed` | `{threadId, status}` | 线程状态变更 |
| `thread/closed` | `{threadId}` | 线程已卸载 |
| `item/started` | `{item: {id, type, ...}}` | Item 开始（同 exec --json） |
| `item/completed` | `{item: {id, type, text, ...}}` | Item 完成（同 exec --json） |
| `item/agentMessage/delta` | `{itemId, textDelta}` | **逐 token 流式 delta** |
| `turn/started` | `{turn: {id}}` | Turn 开始执行 |
| `turn/completed` | `{turn: {id, usage: {input, output}}}` | Turn 完成 |
| `turn/failed` | `{turn: {id}, error}` | Turn 失败 |
| `serverRequest/approval` | `{requestId, toolName, ...}` | 审批请求（双向） |

### 3.3 通知 → AEP 事件映射

| App-Server 通知 | AEP Kind | AEP Data Type | 映射逻辑 |
|-----------------|----------|---------------|---------|
| `item/started(agent_message)` | `message.start` | `MessageStartData{ID, Role: "assistant"}` | 消息开始标记 |
| `item/agentMessage/delta` | `message.delta` | `MessageDeltaData{MessageID, Content: textDelta}` | **逐 token 流式** |
| `item/completed(agent_message)` | `message.end` | `MessageEndData{MessageID}` | 消息结束标记 |
| `item/completed(reasoning)` | `reasoning` | `ReasoningData{Content: summary_text}` | 合并 summary |
| `item/started(command_execution)` | `tool_call` | `ToolCallData{ID, Name: "shell", Input: {command, cwd}}` | 工具调用 |
| `item/completed(command_execution)` | `tool_result` | `ToolResultData{ID, Output: stdout, Error: stderr}` | 工具结果 |
| `item/started(file_change)` | `tool_call` | `ToolCallData{ID, Name: "file_edit", Input: {changes}}` | 文件编辑 |
| `item/completed(file_change)` | `tool_result` | `ToolResultData{ID, Output: status}` | 编辑结果 |
| `item/started(mcp_tool_call)` | `tool_call` | `ToolCallData{ID, Name: "mcp:"+tool}` | MCP 调用 |
| `item/completed(mcp_tool_call)` | `tool_result` | `ToolResultData{ID, Output: result}` | MCP 结果 |
| `turn/completed` | `done` | `DoneData{Success: true, Stats: {input_tokens, output_tokens}}` | Turn 终止 |
| `turn/failed` | `error` + `done` | `ErrorData{Code: "TURN_FAILED"}` + `DoneData{Success: false}` | 错误终止 |
| `serverRequest/approval` | `permission_request` | `PermissionRequestData{ID, ToolName}` | 审批请求 |

### 3.4 Delta 状态机

`item/agentMessage/delta` 是 v2 的核心改进。Mapper 需要维护 message 状态追踪：

```
item/started(type=agent_message) → message.start (首次)
item/agentMessage/delta           → message.delta (可多次)
item/agentMessage/delta           → message.delta
item/agentMessage/delta           → message.delta
item/completed(type=agent_message)→ message.end (终结)
```

---

## 4. 修改范围

| # | 文件 | 改动类型 | 说明 |
|:--|:-----|:---------|:-----|
| 1 | `internal/worker/codexcli/manager.go` | **新建** | `CodexAppServerManager` — 单例进程管理（仿 OCS singleton.go） |
| 2 | `internal/worker/codexcli/worker.go` | **重写** | 从 one-shot spawn → singleton 轻量适配器 |
| 3 | `internal/worker/codexcli/commands.go` | **新建** | JSON-RPC 控制请求（仿 OCS commands.go） |
| 4 | `internal/worker/codexcli/parser.go` | **修改** | `ParseLine` → `ParseNotification`，增加 JSON-RPC 帧解析 |
| 5 | `internal/worker/codexcli/mapper.go` | **修改** | 新增 `item/agentMessage/delta` → `message.delta` 映射，message 状态追踪 |
| 6 | `internal/worker/codexcli/types.go` | **修改** | 新增 `JSONRPCNotification` / `JSONRPCResponse` 类型 |
| 7 | `internal/worker/codexcli/config.go` | **修改** | 新增 `InitSingleton` / `ShutdownSingleton` |
| 8 | `cmd/hotplex/gateway_run.go` | **修改** | `InitSingleton` 替代 `InitConfig`，添加 `ShutdownSingleton` |
| 9 | `internal/config/config.go` | **修改** | `CodexCLIConfig` 增加 `IdleDrainPeriod` 等单例管理字段 |
| 10 | `configs/config.yaml` | **修改** | `codex_cli` 配置节增加 `mode` 和 `idle_drain_period` |
| 11 | `internal/worker/codexcli/worker_test.go` | **修改** | 更新 Parser 测试，新增 Manager 测试 |

**不修改**：`mapper.go` 的核心映射逻辑（item → AEP）完全不变，仅增加 delta 映射和状态追踪。`internal/gateway/bridge.go`、`internal/session/key.go` 无需变更 — WorkerFactory 接口已完全类型擦除。

---

## 5. 详细设计

### Step 1: CodexAppServerManager（新文件 manager.go）

参照 `internal/worker/opencodeserver/singleton.go` 的 `SingletonProcessManager` 模式：

```go
// CodexAppServerManager 管理一个共享的 codex app-server 进程。
// 生命周期：idle → starting → running → (crash → restarting → running) → stopped
type CodexAppServerManager struct {
    log    *slog.Logger
    cfg    config.CodexCLIConfig

    mu       sync.Mutex
    proc     *proc.Manager
    stdin    *os.File      // 写入 JSON-RPC 请求
    stdout   *os.File      // 读取 JSON-RPC 响应+通知
    refs     int
    state    managerState
    crashCh  chan struct{} // 进程崩溃时关闭

    // JSON-RPC 通信
    pending   sync.Map              // map[int64]chan json.RawMessage（请求 ID → 响应通道）
    nextReqID atomic.Int64

    // 通知分发（按 threadId 路由）
    notifyMu    sync.RWMutex
    subscribers map[string]chan *events.Envelope
    notifyCancel context.CancelFunc

    converter  *Converter
    idleTimer  *time.Timer
}

type managerState int
const (
    stateIdle     managerState = iota
    stateStarting
    stateRunning
    stateStopped
)

// Acquire 增加引用计数，首次调用时启动进程。
func (m *CodexAppServerManager) Acquire(ctx context.Context) (crashCh <-chan struct{}, err error)

// Release 减少引用计数，归零时启动空闲排空计时器。
func (m *CodexAppServerManager) Release()

// Subscribe 为指定 threadId 订阅通知通道。
func (m *CodexAppServerManager) Subscribe(threadID string) chan *events.Envelope

// Unsubscribe 取消订阅。
func (m *CodexAppServerManager) Unsubscribe(threadID string)

// Call 发送 JSON-RPC 请求并等待响应。
func (m *CodexAppServerManager) Call(method string, params any) (json.RawMessage, error)

// Notify 发送 JSON-RPC 通知（无响应）。
func (m *CodexAppServerManager) Notify(method string, params any) error

// Shutdown 强制终止进程（用于网关关闭）。
func (m *CodexAppServerManager) Shutdown(ctx context.Context)
```

**startProcessLocked 实现要点**：

```go
func (m *CodexAppServerManager) startProcessLocked() error {
    m.state = stateStarting

    args := []string{"app-server"}  // stdio 模式，默认传输

    m.proc = proc.New(proc.Opts{Logger: m.log})
    stdin, stdout, _, err := m.proc.Start(context.Background(),
        m.cfg.Command, args, m.buildEnv(), "")
    if err != nil {
        m.state = stateIdle
        return err
    }

    m.stdin = stdin
    m.stdout = stdout

    // 握手：initialize → initialized
    if err := m.handshake(); err != nil {
        _ = m.proc.Kill()
        m.state = stateIdle
        return err
    }

    m.state = stateRunning

    go m.monitorProcess()
    go m.readNotifications()

    return nil
}

func (m *CodexAppServerManager) handshake() error {
    // 发送 initialize
    resp, err := m.callLocked("initialize", map[string]any{
        "clientInfo": map[string]string{
            "name":    "hotplex",
            "title":   "HotPlex Gateway",
            "version": "1.0.0",
        },
        "capabilities": map[string]any{
            "experimentalApi": true,
        },
    })
    if err != nil {
        return fmt.Errorf("initialize: %w", err)
    }
    // 发送 initialized
    if err := m.notifyLocked("initialized", map[string]any{}); err != nil {
        return fmt.Errorf("initialized: %w", err)
    }
    return nil
}
```

### Step 2: Worker 重写（worker.go）

从 one-shot spawn 模式改为 singleton 引用模式：

```go
type Worker struct {
    *base.BaseWorker

    manager   *CodexAppServerManager
    threadID  string
    releaseOnce sync.Once
    crashSub  <-chan struct{}

    mu       sync.Mutex
    conn     *conn         // 基于通知通道的 SessionConn
    commands *ServerCommander
}

func (w *Worker) Start(ctx context.Context, session worker.SessionInfo) error {
    // 1. 获取单例进程引用
    crashCh, err := w.manager.Acquire(ctx)
    if err != nil {
        return fmt.Errorf("codexcli: acquire: %w", err)
    }
    w.crashSub = crashCh

    // 2. 创建 thread
    threadID, err := w.createThread(session)
    if err != nil {
        w.manager.Release()
        return err
    }
    w.threadID = threadID

    // 3. 订阅通知
    ch := w.manager.Subscribe(threadID)
    w.conn = newConn(session.UserID, session.SessionID, ch)

    // 4. 初始化命令处理器
    w.commands = NewServerCommander(w.manager, threadID)

    return nil
}

func (w *Worker) createThread(session worker.SessionInfo) (string, error) {
    cfg := resolveConfig()
    params := map[string]any{
        "model":      cfg.Model,
        "cwd":        session.ProjectDir,
        "sandbox":    cfg.Sandbox,
        "personality": "friendly",
        "ephemeral":  cfg.Ephemeral,
    }
    if cfg.Model == "" {
        delete(params, "model")
    }

    resp, err := w.manager.Call("thread/start", params)
    if err != nil {
        return "", err
    }

    var result struct {
        Thread struct {
            ID string `json:"id"`
        } `json:"thread"`
    }
    json.Unmarshal(resp, &result)
    return result.Thread.ID, nil
}

func (w *Worker) Input(ctx context.Context, content string, metadata map[string]any) error {
    // 处理控制响应
    handled, err := base.DispatchMetadata(ctx, metadata, w)
    if err != nil { return err }
    if handled {
        w.SetLastIO(time.Now())
        return nil
    }

    // 发送 turn/start
    params := map[string]any{
        "threadId": w.threadID,
        "input": []map[string]string{
            {"type": "text", "text": content},
        },
    }
    _, err = w.manager.Call("turn/start", params)
    if err != nil {
        return fmt.Errorf("codexcli: turn/start: %w", err)
    }

    w.SetLastIO(time.Now())
    return nil
}

func (w *Worker) Terminate(ctx context.Context) error {
    w.release()
    return nil
}

func (w *Worker) release() {
    w.releaseOnce.Do(func() {
        if w.manager != nil && w.threadID != "" {
            _ = w.manager.Notify("thread/unsubscribe", map[string]string{
                "threadId": w.threadID,
            })
            w.manager.Unsubscribe(w.threadID)
            w.manager.Release()
        }
        if w.conn != nil {
            _ = w.conn.Close()
        }
    })
}
```

### Step 3: 流式 Delta 映射（mapper.go 增强）

在 mapper.go 中新增 message 状态追踪和 delta 映射：

```go
type messageTracker struct {
    mu       sync.Mutex
    messages map[string]*messageState  // itemId → state
}

type messageState struct {
    messageID string
    started   bool
}

func (m *Mapper) MapNotification(method string, params json.RawMessage) []*events.Envelope {
    switch method {
    case "item/started":
        var p struct {
            Item struct {
                ID   string `json:"id"`
                Type string `json:"type"`
            } `json:"item"`
        }
        json.Unmarshal(params, &p)
        if p.Item.Type == "agent_message" {
            m.tracker.startMessage(p.Item.ID)
            return []*events.Envelope{
                newEnvelope(events.MessageStart, events.MessageStartData{
                    ID:   m.tracker.getMessageID(p.Item.ID),
                    Role: "assistant",
                }, m.sessionID, m.nextSeq()),
            }
        }

    case "item/agentMessage/delta":
        var p struct {
            ItemID    string `json:"itemId"`
            TextDelta string `json:"textDelta"`
        }
        json.Unmarshal(params, &p)
        return []*events.Envelope{
            newEnvelope(events.MessageDelta, events.MessageDeltaData{
                MessageID: m.tracker.getMessageID(p.ItemID),
                Content:   p.TextDelta,
            }, m.sessionID, m.nextSeq()),
        }

    case "item/completed":
        // ... 现有映射逻辑（agent_message → message.end, reasoning → reasoning, etc.）
        // 对于 agent_message 类型，额外发送 message.end
        if item.Type == "agent_message" {
            envs = append(envs, newEnvelope(events.MessageEnd, events.MessageEndData{
                MessageID: m.tracker.getMessageID(item.ID),
            }, m.sessionID, m.nextSeq()))
            m.tracker.endMessage(item.ID)
        }
    }
    return envs
}
```

### Step 4: 通知路由（manager.go readNotifications）

```go
func (m *CodexAppServerManager) readNotifications() {
    defer func() {
        if r := recover(); r != nil {
            m.log.Error("codexcli: readNotifications panic", "panic", r)
        }
    }()

    scanner := bufio.NewScanner(m.stdout)
    scanner.Buffer(make([]byte, 64*1024), 10*1024*1024)

    for scanner.Scan() {
        line := scanner.Bytes()

        var frame struct {
            ID     *int64          `json:"id"`
            Method string          `json:"method"`
            Result json.RawMessage `json:"result,omitempty"`
            Params json.RawMessage `json:"params,omitempty"`
            Error  *jsonRPCError   `json:"error,omitempty"`
        }
        if err := json.Unmarshal(line, &frame); err != nil {
            continue
        }

        if frame.ID != nil {
            // 响应 → 交给等待的调用方
            if ch, ok := m.pending.LoadAndDelete(*frame.ID); ok {
                select {
                case ch.(chan json.RawMessage) <- frame.Result:
                default:
                }
            }
        } else {
            // 通知 → 按 threadId 路由
            m.dispatchNotification(frame.Method, frame.Params)
        }
    }
}

func (m *CodexAppServerManager) dispatchNotification(method string, params json.RawMessage) {
    // 提取 threadId
    var extract struct {
        ThreadID string `json:"threadId"`
    }
    json.Unmarshal(params, &extract)

    threadID := extract.ThreadID
    if threadID == "" {
        // 某些通知（如 serverRequest/approval）可能需要其他路由方式
        return
    }

    // 查找订阅者
    m.notifyMu.RLock()
    ch, ok := m.subscribers[threadID]
    m.notifyMu.RUnlock()
    if !ok {
        return
    }

    // 转换为 CodexEvent → AEP Envelope
    envs := m.converter.Convert(method, params)
    for _, env := range envs {
        select {
        case ch <- env:
        default:
            m.log.Warn("codexcli: subscriber channel full, dropping",
                "thread_id", threadID, "method", method)
        }
    }
}
```

### Step 5: 命令处理器（新文件 commands.go）

```go
type ServerCommander struct {
    manager  *CodexAppServerManager
    threadID string
}

func (c *ServerCommander) SendControlRequest(ctx context.Context, subtype string, body map[string]any) (map[string]any, error) {
    switch subtype {
    case "set_model":
        // 内存操作，下次 turn/start 生效
        return nil, nil
    case "get_context_usage":
        resp, err := c.manager.Call("thread/read", map[string]string{
            "threadId": c.threadID,
        })
        // 解析 usage 统计
        return parseContextUsage(resp), err
    default:
        return nil, fmt.Errorf("codexcli: unknown control subtype: %s", subtype)
    }
}
```

---

## 6. 配置设计

### 6.1 config.go 新增字段

```go
type CodexCLIConfig struct {
    Command         string        `mapstructure:"command"`
    Model           string        `mapstructure:"model"`
    Sandbox         string        `mapstructure:"sandbox"`
    ApprovalMode    string        `mapstructure:"approval_mode"`
    Ephemeral       bool          `mapstructure:"ephemeral"`
    StartupTimeout  time.Duration `mapstructure:"startup_timeout"`
    IdleDrainPeriod time.Duration `mapstructure:"idle_drain_period"` // 新增：空闲排空超时
}
```

### 6.2 config.yaml

```yaml
worker:
  codex_cli:
    command: "codex"
    model: ""
    sandbox: "workspace-write"
    approval_mode: "never"
    ephemeral: true
    startup_timeout: 30s
    idle_drain_period: 30m    # 新增：无活跃 session 后多久杀进程
```

---

## 7. 错误处理

### 7.1 错误分类

| 场景 | 错误类型 | 处理 |
|------|---------|------|
| app-server 二进制未找到 | `exec.ErrNotFound` | 标记 Worker Unavailable |
| initialize 握手超时 | `WorkerError(ErrKindUnavailable)` | 杀进程 + 重置 state=Idle |
| JSON-RPC 调用超时 | `context.DeadlineExceeded` | 可重试（LLMRetryController） |
| 进程崩溃 | `monitorProcess` 检测 | `close(crashCh)` → 所有 Worker 收到通知 |
| 通知通道满 | 丢弃 + Warn 日志 | 背压保护（非阻塞发送） |

### 7.2 崩溃恢复

```
monitorProcess() 检测进程退出
  → 如果 state==Running && refs > 0：
    → close(crashCh)           // 通知所有活跃 Worker
    → 关闭所有 subscriber 通道  // Worker 的 forwardBusEvents 退出
    → 创建新 crashCh             // 为下次 Acquire 准备
```

---

## 8. 实施阶段

```
Phase 1 — 基础设施（6h）
  Step 1: manager.go — CodexAppServerManager (Acquire/Release/startProcess/handshake/monitorProcess/Shutdown)
  Step 2: types.go — 新增 JSONRPCNotification/JSONRPCResponse 类型
  Step 3: config.go — 新增 IdleDrainPeriod 字段
  └→ make build 验证编译通过

Phase 2 — Worker 重写（6h）
  Step 4: worker.go 重写 — Start(thread/start)/Input(turn/start)/Resume/Terminate(Release)
  Step 5: parser.go 修改 — ParseLine → ParseNotification
  Step 6: manager.go — readNotifications + dispatchNotification + subscriber 管理
  └→ 集成测试（mock app-server 响应）

Phase 3 — 流式 Delta（4h）
  Step 7: mapper.go 增强 — item/agentMessage/delta → message.delta 映射
  Step 8: messageTracker 状态机（message.start → delta → message.end）
  └→ 单元测试覆盖所有通知类型

Phase 4 — 完善（4h）
  Step 9: commands.go — SendControlRequest + Compact/Clear/Rewind
  Step 10: gateway_run.go — InitSingleton/ShutdownSingleton 集成
  Step 11: configs/config.yaml — 新增 idle_drain_period
  └→ make check 全部通过

Phase 5 — 测试与文档（2h，可选）
  更新 worker_test.go（Parser/Manager 测试）
  make quality 全部通过
```

---

## 9. 风险与缓解

| 风险 | 概率 | 影响 | 缓解措施 |
|------|------|------|---------|
| **app-server API 变更**：JSON-RPC 方法签名可能变化 | 中 | 高 | `initialize` 时声明 `clientInfo` 版本；关注 Codex changelog；解析器对未知 method 做忽略处理 |
| **stdio 写锁竞争**：多 Worker 共享同一 stdin 管道 | 高 | 中 | `sync.Mutex` 序列化所有 `Call()` 和 `Notify()` 写入；请求超时 30s 防止死锁 |
| **通知通道阻塞**：subscriber channel 满导致背压 | 中 | 低 | 256 缓冲通道 + 非阻塞发送；delta 事件可丢弃（与现有 backpressure 策略一致） |
| **进程泄漏**：崩溃后新进程未清理旧 subscriber | 低 | 中 | `monitorProcess` 自动关闭所有 subscriber 通道；`crashCh` 通知机制确保 Worker 感知崩溃 |
| **内存增长**：长驻进程可能随 thread 数增长 | 低 | 低 | `ephemeral: true` 跳过 SQLite 写入；idle drain 30m 限制进程存活时间 |

---

## 10. 与 v1 的兼容性

v1（`codex exec`）和 v2（`codex app-server`）可在同一 `codexcli` 包中共存，通过配置切换：

```go
// worker.go init()
func init() {
    worker.Register(worker.TypeCodexCLI, func() (worker.Worker, error) {
        cfg := GetConfig()
        if cfg.UseAppServer {
            return &AppServerWorker{...}, nil  // v2 持久化模式
        }
        return &ExecWorker{...}, nil           // v1 one-shot 模式
    })
}
```

或在 `gateway_run.go` 中根据配置调用不同的初始化函数：

```go
if cfg.Worker.CodexCLI.UseAppServer {
    codexcli.InitSingleton(log, cfg.Worker.CodexCLI)
} else {
    codexcli.InitConfig(cfg.Worker.CodexCLI)
}
```

---

## 11. 验收标准

| # | 标准 | 验证方式 |
|---|------|---------|
| AC-1 | `make build` 编译成功，无错误 | CI |
| AC-2 | `codex_cli` worker type 出现在 `RegisteredTypes()` | 单元测试 |
| AC-3 | `CodexAppServerManager.Acquire()` 懒启动 `codex app-server` 进程 | 集成测试 |
| AC-4 | `thread/start` → `turn/start` → 流式接收 `item/agentMessage/delta` → `turn/completed` | 集成测试（mock 或真实 codex） |
| AC-5 | `item/agentMessage/delta` 正确映射到 AEP `message.delta` | 单元测试 |
| AC-6 | message 状态机：`message.start` → `message.delta` → `message.end` 完整序列 | 单元测试 |
| AC-7 | `turn/failed` 正确映射到 AEP Error + Done | 单元测试 |
| AC-8 | `Release()` 空闲排空：refs=0 后 30m 杀进程 | 单元测试（短超时） |
| AC-9 | 进程崩溃 → `crashCh` 关闭 → Worker.Wait() 返回退出码 1 | 单元测试 |
| AC-10 | `Shutdown()` 正确清理进程和所有 subscriber 通道 | 单元测试 |
| AC-11 | 多个 Worker 并发 `Acquire()/Release()` 引用计数正确 | 单元测试（-race） |
