# Spec: Codex CLI Worker 问题诊断与修复方案

> **Implementation Record** (2026-06-09): 最终实现与本文原始建议有两处关键偏离：
>
> 1. **Terminated session skip 机制**：原文建议 CodexCLI 的 `Resume()` 返回特殊错误让 bridge 重试（§P1 方案 B）。实际实现为 `CanResumeTerminated() bool` 能力接口——CodexCLI 返回 `false`，bridge 在 `StartPlatformSession` 中跳过 resume 直接走 `startOrResumeOnInUse`。这比错误码方案更显式，且对所有 Worker 类型可扩展。
>
> 2. **能力查询性能**：初始实现 `CanResumeTerminated()` 每次调用临时实例化 Worker。后改为 register-time 缓存（`registry.go: capCache`），在 `Register()` 时创建一个临时实例并缓存结果，后续查询零分配。

**日期**: 2026-06-09
**版本**: v1.26.2
**日志源**: `logs/hotplex.log` (当前 session) + `~/.hotplex/logs/gateway.log` (历史)
**诊断触发**: 用户通过飞书发起 "分析日志，挖掘 codex worker 问题"

---

## 目录

- [P1: Resume 永远失败，每次回退到新建 session](#p1-resume-永远失败每次回退到新建-session)
- [P2: Zombie 进程清理链延迟](#p2-zombie-进程清理链延迟)
- [P3: Turn 号恢复 context canceled](#p3-turn-号恢复-context-canceled)
- [P4: Elicitation 请求悬停阻塞 Worker](#p4-elicitation-请求悬停阻塞-worker)
- [P5: Warning 通知噪音](#p5-warning-通知噪音)
- [优先级与依赖关系](#优先级与依赖关系)

---

## P1: Resume 永远失败，每次回退到新建 session

### 严重度

**高** — 用户在 Slack/飞书的对话线程中发消息，session 过期后每次都丢失上下文。

### 日志证据

```
01:34:27.6267 WARN bridge: resume failed for terminated session, falling back to new session
  err="bridge: resume start: codexcli: app-server not started (state=0)"
01:34:27.6279 INFO session: created  session_id=539773c6... worker_type=codex_cli
```

### 根因分析

**完整调用链**：

```
StartPlatformSession (bridge.go:404)
  → si.State == StateTerminated
  → ResumeSession (bridge.go:407)
    → resumeWithOpts (bridge.go:229)
      → createAndLaunchWorker (bridge_worker.go:52)
        → b.wf.NewWorker(codex_cli) → 新 AppServerWorker 实例
        → startFn = w.Resume(ctx, info) (bridge.go:297)
          → w.state == appStateNew(0) (worker.go:345)
          → return error "app-server not started (state=0)"
```

**核心矛盾**：

| 层面 | 期望 | 实际 |
|------|------|------|
| `createAndLaunchWorker` | 复用旧 Worker 实例 | **每次创建新实例** |
| `NewWorker()` | state 继承自旧实例 | state = `appStateNew(0)` |
| `w.Resume()` | 检查 state > New | state == New → 拒绝 |
| app-server 单例 | 进程可能仍在运行 | 即使运行，Worker 实例 state 也已重置 |

**设计缺陷**：Codex Worker 的 `Resume()` 检查 `w.state == appStateNew || w.state == appStateTerminated` 来拒绝无效调用。但 bridge 的 resume 路径每次都创建**全新的 Worker 实例**，新实例的 state 始终为 `appStateNew`。这意味着 **resume 路径对 codex_cli worker 永远不可能成功**。

### 修复方案

**方案 A（推荐）：Codex Worker 的 Resume 路径改为 Start 语义**

Codex app-server 是单例模式，不存在 per-session 进程。Resume 和 Start 本质相同：
1. Acquire 单例引用（可能重启进程）
2. 创建新 thread（`thread/start`）
3. 订阅通知

区别仅在于 session 信息是否继承。既然每次都创建新 Worker 实例，Resume 应该走 Start 的逻辑。

```go
// worker.go:343
func (w *AppServerWorker) Resume(ctx context.Context, session worker.SessionInfo) error {
    // App-server is a singleton — Resume is functionally identical to Start
    // because every bridge resume path creates a new Worker instance.
    // Just delegate to Start to avoid the state check guard.
    return w.Start(ctx, session)
}
```

**影响范围**：仅 `internal/worker/codexcli/worker.go` 的 `Resume()` 方法。

**风险**：
- `Start()` 检查 `state != appStateNew`，新实例满足条件 ✓
- `Start()` 调用 `Acquire()` 获取单例引用，正确处理 idle drain ✓
- `Start()` 调用 `startNewThread()`，创建新 thread 并订阅 ✓
- bridge 端 `resumeWithOpts` 会检查 `ErrFellBackToFreshStart`，如果需要可让 Start 返回该 sentinel error

**验证**：
- 日志应出现 `codex-app-server: acquire` → `subscribed` → `forwardEvents goroutine started`
- 不再出现 `resume failed for terminated session, falling back to new session`
- 用户在 Slack thread 中继续发消息，对话上下文通过 thread/start 的 session 信息传递

---

## P2: Zombie 进程清理链延迟

### 严重度

**中** — 清理链最多延迟 10s（5s graceful + 5s force），期间 gateway 的 forwardEvents goroutine 被阻塞。历史日志（v1.24.1）中出现 "abandoning"。

### 日志证据

```
12:14:28 WARN session: zombie IO polling triggered  last_io=11:43:42 timeout=30m0s
12:14:28 INFO codex-app-server: killing idle process immediately  pgid=84491
12:14:28 WARN codex-app-server: stdout read error  err="read |0: file already closed"
12:14:33 WARN bridge: Wait() timed out, force-killing
12:14:38 WARN bridge: Wait() did not return after Kill(), abandoning
```

### 根因分析

**时序**：

```
t+0.0s  zombie GC 触发 → Terminate → shutdown() → release() → KillIfIdle()
          KillIfIdle: proc.ForceKill(pgid) 直接 SIGKILL
          monitorProcess: pm.Wait() 检测到进程退出 → state=idle → close(crashCh) → close(subscribers)
t+0.0s  stdout pipe 关闭（进程被 SIGKILL）→ "read |0: file already closed"

t+5.0s  bridge forwardEvents: Wait() 5s 超时 → "force-killing" → w.Kill()
          AppServerWorker.Kill() → shutdown() → release()
          release(): 检查 w.released || w.closed → 已被 Terminate 标记 → 跳过
          close(doneCh) — 但 doneCh 已被 Terminate 关闭 → skip

t+10.0s bridge forwardEvents: killTimeout 5s 超时 → "abandoning"
```

**竞态条件**：
1. `monitorProcess` 关闭 `crashCh`（signal 进程退出）
2. `AppServerWorker.Wait()` 监听 `crashCh` 和 `doneCh`
3. bridge 的 `handleWorkerExit` 调用 `w.Wait()` 的 goroutine 应该在 `crashCh` 关闭时立即返回
4. 但 `forwardEvents` 的主循环在 `range w.Conn().Recv()` 上阻塞，**Recv channel 已被 monitorProcess close** → range 退出 → 进入 `handleWorkerExit` → 此时 Wait 可能已经返回

**真正问题**：`Wait()` 在 bridge 端的 goroutine 中调用（bridge_forward.go:467），而 forwardEvents 的 range-recv 可能比 Wait 更早感知到退出。导致 handleWorkerExit 中 Wait 等待的 channel（crashCh/doneCh）已经被消费过。

### 修复方案

**方案：优化 AppServerWorker.Wait() 的退出信号**

当前 `Wait()` 监听两个 channel，但 release() 和 monitorProcess 的关闭顺序存在竞态。增加 `sync.Once` 保护确保 `doneCh` 只关闭一次：

```go
// worker.go — 增加 closeOnce
type AppServerWorker struct {
    // ...
    closeOnce sync.Once
    // ...
}

func (w *AppServerWorker) closeDoneCh() {
    w.closeOnce.Do(func() {
        if w.doneCh != nil {
            close(w.doneCh)
            w.doneCh = nil
        }
    })
}
```

同时将 `closeAndMarkDone` 和 `release` 中的 `close(doneCh)` 统一调用 `closeDoneCh()`。

**额外优化**：bridge_forward.go 的 `handleWorkerExit` 中，如果 Worker 是 codex_cli 类型（单例），可以跳过 force-kill 阶段——单例进程已被 `KillIfIdle` 处理：

```go
// bridge_forward.go: handleWorkerExit
if _, ok := w.(*codexcli.AppServerWorker); ok {
    // Singleton process already killed by KillIfIdle — skip force-kill
    break
}
```

**影响范围**：`internal/worker/codexcli/worker.go` + `internal/gateway/bridge_forward.go`

---

## P3: Turn 号恢复 context canceled

### 严重度

**低** — 功能无影响，仅日志噪音和 eventstore turn 计数不准确。

### 日志证据

```
01:34:28.3551 WARN turns: restore turn num  error="eventstore: latest turn num: context canceled"
```

### 根因分析

这是 **P1 的副作用**。调用链：

1. `ResumeSession` → `resumeWithOpts` → `createAndLaunchWorker`
2. `createAndLaunchWorker` 调用 `startFn`（即 `w.Resume`）失败 → 返回错误
3. `resumeWithOpts` 返回错误
4. bridge 的 `StartPlatformSession`（line 408）fallback 到 `startOrResumeOnInUse`
5. `startOrResumeOnInUse` → `StartSession` → 新的 `createAndLaunchWorker`
6. 新的 forwardEvents goroutine 启动 → 查询 eventstore 恢复 turn 号
7. **但此时之前的 ctx 可能已被 cancel**（来自 resumeWithOpts 的 ctx）

实际代码中 turn 恢复使用 `opts.ctx`（bridge_forward.go:105），这是 `forwardOpts.ctx`。在 resume 路径中，这个 ctx 来自 bridge.go:288 传入的 `&opts`，而 opts.ctx 在 bridge.go:258 被设置为 `opts.ctx`（默认为 nil，在 createAndLaunchWorker 中被设置为 params.ctx）。

问题出在第一次 resume 失败后，ctx 被 cancel，但第二次 Start 使用的**应该是新的 ctx**。从代码看，第二次调用 `startOrResumeOnInUse` → `StartSession` 使用的是 `StartPlatformSession` 传入的原始 ctx（bridge.go:410），而不是 resume 的 ctx。所以这更可能是**竞态**：第一次 resume 创建的 forwardEvents goroutine 在 cancel 的 ctx 下查询 eventstore。

### 修复方案

**无需独立修复**。修复 P1 后，resume 路径不再失败 → 不会出现 ctx cancel 竞态。P1 的修复是 P3 的充分条件。

如果需要防御性修复，可在 `bridge_forward.go:105` 的 turn 恢复中使用 `context.Background()` 加超时，而不是依赖 `opts.ctx`：

```go
if acc.TurnCount.Load() == 0 && b.turnsQuerier != nil {
    tnCtx, tnCancel := context.WithTimeout(context.Background(), 3*time.Second)
    // ...
}
```

---

## P4: Elicitation 请求悬停阻塞 Worker

### 严重度

**高** — Worker 线程被阻塞，无法继续执行，用户感知为"没反应"。

### 日志证据

```
01:34:28.3501 dispatching notification  method=turn/started
01:34:34.0540 dispatching notification  method=mcpServer/elicitation/request
                                                    threadId=019ea84c-...
（8 分钟无后续 turn/completed 或 turn/failed）
01:42:09 slack: handling message  text="咋样了？"
01:42:15 bridge: StartPlatformSession called (同一 session)
```

### 根因分析

**完整路径**：

```
codex app-server → JSON-RPC notification (method="mcpServer/elicitation/request", ID=0)
  → manager.dispatchFrame() → frame.ID == 0 → notification 路径
  → manager.dispatchNotification()
    → 提取 threadId → 查找 subscriber → 调用 converter.MapNotification()
      → mapper.MapNotification("mcpServer/elicitation/request", params)
        → switch 中无匹配 case → return nil
    → envelope 为 nil → 丢弃
```

**后果**：
1. Codex app-server 发出 elicitation 通知（MCP 服务器需要用户确认）
2. HotPlex mapper 没有 `mcpServer/elicitation/request` 的映射规则
3. 事件被**静默丢弃**，不生成 `ElicitationRequest` AEP 事件
4. 前端（Slack/飞书）收不到任何通知
5. 用户无法回应
6. **Codex 侧可能处于等待状态**（取决于 codex 内部实现）

**对比其他 Worker**：
- Claude Code Worker：`internal/worker/claudecode/parser.go:415` 解析 elicitation 字段
- Claude Code Mapper：`internal/worker/claudecode/mapper.go:241` 映射 elicitation → `ElicitationRequest`
- ACP Worker：`internal/worker/acp/worker.go:1008` 处理非 permission server request
- **Codex CLI Worker：无映射**

**交互层已就绪**：
- `internal/messaging/interaction.go:244` 已支持 `ElicitationRequest`
- `internal/gateway/handler.go:133` 已支持路由 `elicitation_response`
- `internal/worker/codexcli/worker.go:505` 已实现 `HandleElicitationResponse`

**缺失的只有 mapper 映射**。

### 修复方案

**在 mapper.go 中添加 `mcpServer/elicitation/request` 映射**：

```go
// mapper.go:77 — 在 MapNotification 的 switch 中添加
case "mcpServer/elicitation/request":
    return m.mapNotifElicitation(params)
```

```go
// mapper.go — 新增方法
func (m *Mapper) mapNotifElicitation(params json.RawMessage) []*events.Envelope {
    var p struct {
        RequestID     string   `json:"requestId"`
        MCPServerName string   `json:"mcpServerName"`
        Message       string   `json:"message"`
        Mode          string   `json:"mode"`    // "confirm" | "input" | "select"
        Options       []string `json:"options,omitempty"`
        Default       string   `json:"default,omitempty"`
    }
    if err := json.Unmarshal(params, &p); err != nil {
        return nil
    }
    return []*events.Envelope{
        newEnvelope(events.ElicitationRequest, events.ElicitationRequestData{
            ID:            p.RequestID,
            MCPServerName: p.MCPServerName,
            Message:       p.Message,
            Mode:          p.Mode,
            Options:       p.Options,
            Default:       p.Default,
        }, m.sessionID, m.nextSeq()),
    }
}
```

**同时需要确认**：如果 codex app-server 以 serverRequest（ID!=0）发送 elicitation（而非 notification），则 `dispatchServerRequest` 会存储 reqID → frame.ID 映射，`HandleElicitationResponse` 可以通过 `RespondServerRequest` 回复。需要确认 codex 版本的具体行为。

**防御性措施**：如果 elicitation 确实导致 Worker 阻塞（无超时），需要添加 **elicitation 超时自动拒绝**：

```go
// interaction.go — 已有 5min timeout + auto-deny 机制
// 确认 ElicitationRequest 走同一个 InteractionManager 超时路径
```

**影响范围**：`internal/worker/codexcli/mapper.go`（主要）+ `types.go`（可能需要新类型）

**验证**：
- 触发 MCP server 的 elicitation → 前端应显示确认/输入对话框
- 用户回应后 Worker 继续执行
- 超时后自动拒绝

---

## P5: Warning 通知噪音

### 严重度

**低** — 不影响功能，但用户体验有噪音。

### 问题 1：无 threadId 的 notification 刷 DEBUG 日志

**日志证据**：153 行日志中 13 行 `notification without threadId, skipping`，全部是：
- `mcpServer/startupStatus/updated`（MCP 服务器连接状态更新）
- `remoteControl/status/changed`（远程控制状态变更）

**根因**：这些 notification 没有 threadId，`dispatchNotification` 正确跳过但仍写 DEBUG 日志。

**修复**：将高频无 threadId notification 的日志静默：

```go
// manager.go:872 — dispatchNotification
if params.ThreadID == "" {
    switch notif.Method {
    case "mcpServer/startupStatus/updated", "remoteControl/status/changed",
        "skills/changed", "account/rateLimits/updated":
        // 高频无价值 notification，静默跳过
    default:
        m.log.Debug("codex-app-server: notification without threadId, skipping", "method", notif.Method)
    }
    return
}
```

### 问题 2：有 threadId 的 warning 被映射为 Step 事件

**日志证据**：session 开始时连发 3 条 `warning` notification，全部被映射为 `Step{stepType: "warning"}`。

**根因**：`mapNotifWarning` 将所有 warning 直接映射为 Step 事件，不区分严重性。某些 warning（如 MCP 配置提示、sandbox 模式提示）对用户无价值。

**修复方案**：

```go
// mapper.go:467 — 添加 warning 过滤
func (m *Mapper) mapNotifWarning(params json.RawMessage) []*events.Envelope {
    var p struct {
        Message string `json:"message"`
    }
    if err := json.Unmarshal(params, &p); err != nil {
        return nil
    }
    for _, prefix := range warningSuppressPrefixes {
        if strings.HasPrefix(p.Message, prefix) {
            return nil
        }
    }
    return []*events.Envelope{
        newEnvelope(events.Step, events.StepData{
            StepType: "warning",
            Name:     p.Message,
        }, m.sessionID, m.nextSeq()),
    }
}

var warningSuppressPrefixes = []string{
    "MCP server",
    "sandbox",
    "experimental",
}
```

**影响范围**：`internal/worker/codexcli/mapper.go` + `internal/worker/codexcli/manager.go`

---

## 优先级与依赖关系

```
P1 (Resume 永远失败) ─────── 修复后自动解决 ──→ P3 (Turn 号恢复)
   │
   │  独立修复
   ↓
P4 (Elicitation 悬停) ── 高优，Worker 死锁
   │
   │  独立修复
   ↓
P2 (Zombie 清理延迟) ── 中优，优化体验
   │
   │  独立修复
   ↓
P5 (Warning 噪音) ── 低优，日志清理
```

| 问题 | 优先级 | 修复文件 | 估时 | 依赖 |
|------|--------|----------|------|------|
| P1 | P0 | `worker/codexcli/worker.go` | 0.5h | 无 |
| P4 | P0 | `worker/codexcli/mapper.go` | 1h | 需确认 codex elicitation 协议格式 |
| P2 | P1 | `worker/codexcli/worker.go` + `gateway/bridge_forward.go` | 1h | 无 |
| P3 | P2 | 无（P1 修复后自动解决） | 0h | P1 |
| P5 | P2 | `worker/codexcli/mapper.go` + `worker/codexcli/manager.go` | 0.5h | 无 |

**建议实施顺序**: P1 → P4 → P2 → P5（P3 随 P1 自动修复）

---

## 附录：诊断方法

1. **日志源**：
   - 当前 session: `logs/hotplex.log`（`make dev` 输出，153 行）
   - 历史: `~/.hotplex/logs/gateway.log`（953KB，v1.24.1）
2. **过滤命令**：`grep -i codex logs/hotplex.log`
3. **关注级别**：WARN + ERROR → DEBUG（dispatchNotification, turn lifecycle）
4. **关键组件**：`bridge.go` (StartPlatformSession/ResumeSession) → `worker.go` (Start/Resume) → `manager.go` (Acquire/Release/monitorProcess) → `mapper.go` (MapNotification)
