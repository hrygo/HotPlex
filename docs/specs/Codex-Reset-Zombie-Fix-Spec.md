---
type: spec
tags:
  - project/HotPlex
  - worker/codex
  - bug/reset
  - bug/zombie
  - area/gateway
  - area/session
date: 2026-05-30
status: draft
progress: 0
estimated_hours: 4
---

# Codex AppServer Reset & Zombie Lifecycle Fix Spec

> **目标**：修复 codex app-server worker 在 `/reset` 和 zombie GC 场景下的两个 P1 缺陷，确保 reset 后会话可正常接收新 input，zombie 清理后进程即时释放。

---

## 1. 缺陷描述

### 1.1 Bug A: Reset 导致会话误杀

**症状**：用户在飞书发送 `/reset` 后，当前 codex 会话立即终止（`running → terminated`），而非保持活跃等待新 input。用户需要重新发消息才能恢复。

**日志证据**（2026-05-30 09:44:17 ~ 09:44:20，session `5eb6cba8`）：

```
09:44:18.52  codex-app-server: subscribed thread_id=...ae12e
09:44:18.53  gateway: control received action=reset
09:44:18.53  codex-app-server: unsubscribed  ← release() 清理旧 thread
09:44:18.53  codex-app-server: release refs=0 ← idle drain 30m
09:44:18.53  bridge: forwardEvents goroutine started resumed=false ← 新 goroutine
09:44:18.56  bridge: worker reset, old forwardEvents exiting gen=0→1
09:44:20.53  WARN bridge: Wait() timed out, force-killing  ← 2s 超时
09:44:20.53  session: transitioned from=running to=terminated ← 误杀
```

### 1.2 Bug B: Zombie GC 后进程残留 30 分钟

**症状**：session 在无 I/O 30 分钟后被 zombie polling 正确终止，但 codex-app-server 单例进程继续运行 30 分钟（idle drain timer），占用内存和 MCP 连接。

**日志证据**（同 session）：

```
09:52:29  turn 26 完成（最后一次 I/O）
10:22:07  WARN session: zombie IO polling triggered, terminating ghost process
10:22:07  codex-app-server: release refs=0, starting idle drain timer 30m
10:22:09  WARN bridge: Wait() timed out, force-killing  ← 又一次超时
10:52:07  codex-app-server: idle drain expired, killing process  ← 30m 后才杀
```

---

## 2. 根因分析

### 2.1 Bug A 根因链

**核心问题：`ResetContext` 清理旧 thread 后未重建新 thread，导致新 forwardEvents 读取已关闭的 recvCh。**

逐帧追踪（涉及 4 个文件的交叉调用）：

```
bridge.go:ResetSession (line 379)
  │
  ├─ rg.IncResetGeneration()  ← gen 0→1
  │
  ├─ w.ResetContext(ctx)
  │    │
  │    ├─ w.Terminate(ctx)
  │    │    └─ w.release()           [worker.go:582-606, sync.Once]
  │    │         ├─ close(doneCh)     ← 旧 doneCh 关闭
  │    │         ├─ manager.Unsubscribe(threadID)
  │    │         │    └─ close(subscriberCh)  ← 关键：关闭 recvCh！
  │    │         └─ manager.Release()
  │    │              └─ refs=0, idle drain 30m
  │    │
  │    ├─ w.mu.Lock()
  │    │    threadID = ""
  │    │    recvCh = nil
  │    │    closed = false
  │    │    doneCh = make(chan struct{})  ← 新 doneCh（开放）
  │    │    releaseOnce = sync.Once{}    ← 重置 once
  │    │    ⚠️ conn 未清理！仍指向旧 appConn（recvCh 已关闭）
  │    └─ w.mu.Unlock()
  │
  │  ⚠️ 未调用 Start()！worker 处于无 thread 状态
  │
  ├─ return nil ← ResetContext 结束
  │
  └─ go forwardEvents(w, ..., forwardOpts{})
       │
       ├─ recvCh := w.Conn().Recv()  ← 返回旧 appConn 的已关闭 recvCh
       ├─ for range recvCh {}         ← 立即退出（closed channel）
       ├─ handleWorkerExit(...)
       │    ├─ gen check: 1==1 ← 匹配！不跳过
       │    ├─ w.Wait()  ← 阻塞在新 doneCh（开放）+ 旧 crashSub
       │    └─ 2s timeout → w.Kill() → session terminated
       └─ ❌ 会话误杀
```

**对比 ExecWorker 为什么没问题**：ExecWorker 是 per-session 进程，`ResetContext` 调用 `BaseWorker.Terminate(ctx)` 杀死实际进程 → `Wait()` 立即返回（进程退出）。

**对比 OCS 为什么没问题**：OCS 返回 `ResetResult{ConnReplaced: false}`，bridge 不启动新 forwardEvents，原地复用 goroutine。

**AppServerWorker 的独特性**：
- 共享单例进程（不因 reset 而死）
- `release()` 关闭 subscriber channel（不像 OCS 用 HTTP 轮询）
- 返回 `ResetResult{ConnReplaced: true}`（bridge 会启动新 forwardEvents）
- `ResetContext` 不调用 `Start()`（新 forwardEvents 读已关闭的 recvCh）

### 2.2 Bug B 根因链

```
session/manager.go zombiePolling (line 1029-1034)
  │
  ├─ TransitionWithReason(terminated, "zombie")
  │    └─ 触发 session 状态变更通知
  │
  └─ bridge 收到通知 → cleanupCrashedWorker
       └─ worker.Terminate()
            └─ release() → manager.Release()
                 └─ refs=0 → startIdleDrainLocked() [30m]
                      └─ 进程残留 30 分钟

⚠️ 缺失环节：无代码路径在 zombie 清理时立即杀进程
   - Terminate() 走 release → idle drain（优雅释放）
   - Kill() 也走 release（与 Terminate 完全相同！）
   - 没有任何方法强制终止单例进程
```

**`AppServerWorker.Kill()` 当前实现**（worker.go:565-568）：

```go
func (w *AppServerWorker) Kill() error {
    w.release()  // 与 Terminate() 完全相同！
    return nil
}
```

Kill 和 Terminate 语义完全一致，对单例进程均不会触发立即终止。

---

## 3. 修复方案

### 3.1 Fix A: ResetContext 重建 thread

**修改文件**：`internal/worker/codexcli/worker.go`

**设计**：`ResetContext` 清理旧 thread 后，立即调用 `Start()` 建立新 thread。bridge 启动的新 forwardEvents 读取到的是新 thread 的 fresh recvCh。

#### 3.1.1 添加 sessionInfo 持久化

AppServerWorker 需要在首次 `Start()` 时保存 SessionInfo，以便 `ResetContext` 用相同参数重建 thread。

```go
// AppServerWorker 结构新增字段
type AppServerWorker struct {
    *base.BaseWorker
    manager     *CodexAppServerManager
    mu          sync.Mutex
    threadID    string
    sessionID   string
    userID      string
    recvCh      chan *events.Envelope
    crashSub    <-chan struct{}
    doneCh      chan struct{}
    releaseOnce sync.Once
    closed      bool
    conn        *appConn
    commands    *ServerCommander
    sessionInfo worker.SessionInfo  // 新增：保存用于 reset 重建
}
```

#### 3.1.2 Start() 保存 sessionInfo

```go
func (w *AppServerWorker) Start(ctx context.Context, session worker.SessionInfo) error {
    w.mu.Lock()
    defer w.mu.Unlock()

    if w.recvCh != nil {
        return fmt.Errorf("codexcli: app-server already started")
    }

    // 保存 sessionInfo 供 ResetContext 重建 thread 使用
    w.sessionInfo = session

    // ... 其余不变 (Acquire, thread/start, Subscribe, create conn)
```

#### 3.1.3 ResetContext 调用 Start()

```go
func (w *AppServerWorker) ResetContext(ctx context.Context) error {
    // 1. 释放旧 thread
    if err := w.Terminate(ctx); err != nil {
        w.Log.Warn("codexcli: reset terminate", "error", err)
    }

    // 2. 清理状态（Start() 要求 recvCh == nil）
    w.mu.Lock()
    w.threadID = ""
    w.recvCh = nil
    w.closed = false
    w.doneCh = make(chan struct{})
    w.releaseOnce = sync.Once{}
    si := w.sessionInfo  // 捕获保存的 session info
    w.mu.Unlock()

    // 3. 建立新 thread：Acquire + thread/start + Subscribe
    //    这使得 bridge 启动的新 forwardEvents 能读到 fresh recvCh。
    if si.SessionID != "" {
        if err := w.Start(ctx, si); err != nil {
            return fmt.Errorf("codexcli: reset restart: %w", err)
        }
    }

    return nil
}
```

**关键保证**：
- `Start()` 要求 `recvCh == nil`（worker.go:458）— ResetContext 先清空 ✓
- `Start()` 会 `Acquire()`（refs++），取消 idle drain timer ✓
- `Start()` 会 `Subscribe()` 新 thread，创建新 recvCh ✓
- `Start()` 会替换 `w.conn` 为新 appConn ✓
- bridge 的新 forwardEvents 读到的是 fresh recvCh ✓

**`release()` 中 sync.Once 保证**：Terminate → release() 执行一次（旧 thread）。ResetOnce 后 Start() 内部不再调用 release()。后续新 forwardEvents 退出时，handleWorkerExit 会调用 Kill()/Terminate() → release() 第二次执行（新 thread）。

#### 3.1.4 连接重建验证

修复后的 reset 流程：

```
bridge.go:ResetSession
  ├─ rg.IncResetGeneration()  ← gen 0→1
  ├─ w.ResetContext(ctx)
  │    ├─ w.Terminate(ctx) → release()
  │    │    ├─ close(doneCh)
  │    │    ├─ Unsubscribe(oldThread) → close(oldRecvCh)
  │    │    └─ Release() → refs=0, idle drain
  │    ├─ 清理状态: threadID="", recvCh=nil, new doneCh, resetOnce
  │    └─ w.Start(ctx, sessionInfo)
  │         ├─ Acquire() → refs=1, idle drain 取消 ✓
  │         ├─ thread/start → 新 thread 创建 ✓
  │         ├─ Subscribe(newThread) → 新 recvCh ✓
  │         └─ conn = &appConn{recvCh: newRecvCh} ✓
  │
  ├─ 旧 forwardEvents(gen=0): oldRecvCh 关闭 → 退出 → gen check → 干净退出
  └─ 新 forwardEvents(gen=1): 读 fresh recvCh → 阻塞等待事件 → ✓
```

### 3.2 Fix B: Kill() 强制终止 idle 单例

**修改文件**：`internal/worker/codexcli/manager.go`, `internal/worker/codexcli/worker.go`

**设计**：区分 Terminate（优雅释放，可能触发 idle drain）和 Kill（强制终止，立即杀进程）。对 zombie 清理路径使用 Kill。

#### 3.2.1 Manager 新增 KillIfIdle

```go
// KillIfIdle 如果 manager 引用计数为 0，立即终止单例进程。
// 用于 bridge force-kill 路径和 zombie GC 清理，避免 30m idle drain 延迟。
func (m *CodexAppServerManager) KillIfIdle() {
    m.mu.Lock()
    defer m.mu.Unlock()

    if m.refs == 0 && m.proc != nil && m.state == stateRunning {
        m.log.Info("codex-app-server: killing idle process (no active refs)")
        _ = m.proc.Kill()
        if m.idleTimer != nil {
            m.idleTimer.Stop()
            m.idleTimer = nil
        }
    }
}
```

**安全保证**：`m.refs == 0` 检查确保不会影响活跃 session 使用的进程。

#### 3.2.2 Kill() 区分于 Terminate()

```go
// Terminate 优雅释放 worker 资源（unsubscribe + release）。
// 单例进程可能继续运行（idle drain）供其他 session 复用。
func (w *AppServerWorker) Terminate(ctx context.Context) error {
    w.release()
    return nil
}

// Kill 强制释放并终止 idle 单例进程。
// 用于 bridge force-kill 路径：当 Wait() 超时或 zombie GC 清理时，
// 不应等待 30m idle drain，而应立即回收进程资源。
func (w *AppServerWorker) Kill() error {
    w.release()
    if w.manager != nil {
        w.manager.KillIfIdle()
    }
    return nil
}
```

#### 3.2.3 bridge_forward.go Wait() 超时后不再残留

修复前：`Wait()` 超时 → `Kill()` → release + idle drain → 进程残留
修复后：`Wait()` 超时 → `Kill()` → release + `KillIfIdle()` → 进程立即终止

---

## 4. 影响范围

| 文件 | 修改类型 | 说明 |
|------|----------|------|
| `internal/worker/codexcli/worker.go` | 修改 | ResetContext 调用 Start()；Kill() 调用 KillIfIdle；新增 sessionInfo 字段；Start() 保存 sessionInfo |
| `internal/worker/codexcli/manager.go` | 新增 | KillIfIdle() 方法 |
| `internal/gateway/bridge.go` | 不修改 | ResetSession 逻辑不变，AppServerWorker 自行处理重建 |
| `internal/gateway/bridge_forward.go` | 不修改 | Wait() 超时路径不变，Kill() 行为改进 |

**不影响的组件**：
- ExecWorker（per-session 进程，ResetContext 走完全不同的路径）
- OCS Worker（返回 `ResetResult{ConnReplaced: false}`，不走新 forwardEvents 路径）
- Claude Code Worker（per-session 进程）
- 其他 worker 适配器

---

## 5. Acceptance Criteria

### 5.1 Reset 功能验证

- [ ] `/reset` 后 session 保持 running 状态（不误终止）
- [ ] `/reset` 后新 forwardEvents goroutine 正常接收事件
- [ ] `/reset` 后用户发送新消息，worker 正常响应（不报 "app-server not started"）
- [ ] `/reset` 后 codex 进程的旧 thread 被 unsubscribe，新 thread 被 subscribe
- [ ] 连续 `/reset` 3 次不导致进程崩溃或 goroutine 泄漏
- [ ] `/reset` 后 forwardEvents goroutine 数量 = 1（无泄漏）

### 5.2 Zombie 清理验证

- [ ] Zombie GC 触发后，codex 单例进程在 5 秒内终止（非 30 分钟）
- [ ] Zombie GC 触发后，无 "Wait() timed out, force-killing" 日志
- [ ] Zombie GC 不影响其他活跃 session（refs > 0 时不杀进程）

### 5.3 回归验证

- [ ] ExecWorker reset 功能不受影响
- [ ] OCS Worker reset 功能不受影响
- [ ] Claude Code Worker reset 功能不受影响
- [ ] 现有 `TestAppServerWorker` 测试全部通过
- [ ] `make test` 无新增失败

### 5.4 新增测试用例

- [ ] `TestAppServerWorker_ResetContext_StartsNewThread`：验证 ResetContext 后 threadID 非空、recvCh 非 nil、conn 非旧 conn
- [ ] `TestAppServerWorker_ResetContext_InputWorks`：验证 ResetContext 后 Input() 不报错
- [ ] `TestCodexAppServerManager_KillIfIdle`：验证 refs=0 时立即杀进程，refs>0 时不杀
- [ ] `TestAppServerWorker_Kill_TerminatesIdleProcess`：验证 Kill() 后进程不再运行

---

## 6. 实现注意事项

### 6.1 Start() 中的锁重入

`ResetContext` 先解锁 `w.mu`，再调用 `Start()`。`Start()` 内部获取 `w.mu`。这两次 lock 不在同一 goroutine 的持锁区间内，不存在死锁风险。

### 6.2 releaseOnce 重置时机

`ResetContext` 重置 `releaseOnce = sync.Once{}` **必须**在 `Start()` 调用之前。否则 `Start()` 内部如果出错需要 cleanup 时，`release()` 不会执行（once 已消耗）。当前代码顺序（清理 → Start）满足此要求。

### 6.3 SessionInfo 中的字段变更

如果用户在 reset 前通过 API 修改了 session 配置（如 project dir），`ResetContext` 使用的是**首次 Start() 保存的** SessionInfo，不是最新配置。这是可接受的：reset 的语义是"恢复初始上下文"，不是"应用最新配置"。配置变更需要新 session。

### 6.4 Codex 进程的 thread 自动卸载

根据 Codex 源码分析（`thread_lifecycle.rs`），当 thread 无 subscriber 且无活动时，codex 进程在 30 分钟后自动卸载 thread。Fix A 中 Unsubscribe 旧 thread 后，旧 thread 将被 codex 进程自动卸载（30m），不占用 HotPlex 资源。新 thread 立即创建，用户端无感知。

---

## 7. 附录：Codex App-Server 协议关键发现

基于 `~/tmp/codex` 源码分析，以下发现支撑了修复方案：

| 发现 | Codex 源码位置 | 对修复的影响 |
|------|----------------|-------------|
| `thread/unsubscribe` 不终止 thread，仅移除 subscriber | `thread_processor.rs:731-757` | 解释了为什么旧 thread 在 unsubscribe 后不立即消失 |
| `thread/start` 是异步的（fire-and-forget） | `thread_processor.rs:381, 979` | Start() 中 Call() 等待响应，但 thread 初始化在后台完成 |
| 无 subscriber 时通知静默丢弃 | `outgoing_message.rs:160-164` | 解释了旧 forwardEvents 退出后无事件丢失风险 |
| Thread 自动卸载延迟 30 分钟 | `thread_lifecycle.rs:3, 54, 87` | 解释了 Bug B 中进程残留的完整机制 |
| App-server 进程无"退出"机制 | `thread_lifecycle.rs` 全文 | Fix B 必须由 HotPlex 侧主动杀进程 |
