---
type: spec
status: implemented
---

# Fix: CodexCLI AppServerWorker.Wait() 永久阻塞

**Issue**: #691
**Severity**: P2 (goroutine leak + 日志噪音 + 30 分钟僵尸)
**Scope**: `internal/worker/codexcli/worker.go`, `internal/worker/codexcli/manager.go`, `internal/worker/codexcli/mapper.go`
**Prerequisite**: 熟悉 `CodexAppServerManager` singleton 模式和 bridge `handleWorkerExit` 流程

---

## 1. Problem Statement

CodexCLI `AppServerWorker` 的 `Wait()` 在 `release()` 被调用后永久阻塞。根因是 `release()` 将 `w.doneCh` 置 nil，而 `Wait()` 直接读取该字段 — Go 中 receive from nil channel 永久阻塞。

**影响范围**:
- 所有 CodexCLI session 的终止都走 abandon 路径（10 秒超时 + goroutine 泄漏）
- 依赖 30 分钟 zombie GC 兜底清理，期间占用 pool 配额
- 多 session 共享 singleton 时，一个 session 的终止等待可能阻塞其他 session

---

## 2. Root Cause

### 2.1 核心时序

```
GC goroutine                          forwardEvents goroutine
─────────────                          ──────────────────────
TransitionWithReason(terminated)
  └→ terminateWorkerGracefully
       └→ w.Terminate()
            └→ shutdown()
                 └→ release()
                      ├→ close(doneCh)      ← doneCh 已关闭
                      ├→ w.doneCh = nil      ← 字段置 nil（BUG）
                      ├→ Unsubscribe(tid)    ← 关闭 subscriber channel
                      └→ Release()           ← refs--
                                        ┌→ range recvCh exits
                                        └→ handleWorkerExit()
                                             └→ w.Wait()
                                                  ├→ <-w.crashSub  ← 不触发
                                                  └→ <-w.doneCh   ← nil，永久阻塞!
                                                  （5s 后 bridge Kill，再 5s abandon）
```

### 2.2 为什么 crashSub 也不触发

`Wait()` 同时等待 `crashSub`（来自 singleton 的 `crashCh`），但两种场景均不触发：

| 场景 | 条件 | monitorProcess 行为 | crashCh 关闭? |
|---|---|---|---|
| 其他 session 仍持有 refs | refs > 0 | 进程仍在运行 | No |
| refs 归零，KillIfIdle 杀掉 | refs == 0 | 判定 "normal exit" | No |

`monitorProcess` 仅在 `wasRunning && refs > 0` 时关闭 `crashCh`（crash 路径）。当 `release()` 已将 refs 归零后再杀进程，`monitorProcess` 视为正常退出。

### 2.3 二次 Kill 无效

bridge 超时后调用 `Kill()` → `shutdown()` → `release()`，但 `release()` 检测到 `w.released == true`，提前返回。`doneCh` 仍为 nil，`Wait()` 继续阻塞。

---

## 3. Proposed Changes

### 3.1 Fix 1: Wait() 在 release 后安全返回（核心修复）

**文件**: `internal/worker/codexcli/worker.go`

**方案 A（推荐）: 保留 doneCh 引用，不置 nil**

```go
// release() — 移除 w.doneCh = nil
func (w *AppServerWorker) release() {
    w.mu.Lock()
    if w.released || w.closed {
        w.mu.Unlock()
        return
    }
    w.released = true
    w.closed = true
    doneCh := w.doneCh
    // 不再置 nil: w.doneCh = nil  ← 删除此行
    tid := w.threadID
    w.mu.Unlock()

    if doneCh != nil {
        close(doneCh)
    }
    // ... Unsubscribe, conn.Close, Release 不变
}
```

**安全性**: `w.closed` guard 已阻止 `release()` 被调用两次，因此 `doneCh` 不会被 double-close。

**Wait() 无需改动**: 对已关闭的 channel 做 receive 立即返回零值，行为正确。

**方案 B（防御性增强）: Wait() 原子捕获字段**

```go
func (w *AppServerWorker) Wait() (int, error) {
    w.mu.Lock()
    crashSub := w.crashSub
    doneCh := w.doneCh
    w.mu.Unlock()

    if doneCh == nil && crashSub == nil {
        return 0, nil // 已释放且无 crash 信号
    }
    select {
    case <-crashSub:
        return 1, nil
    case <-doneCh:
        return 0, nil
    }
}
```

**推荐方案 A + B 同时实施**:
- A 消除根因（不置 nil）
- B 提供防御（nil 安全）

### 3.2 Fix 2: monitorProcess 在 KillIfIdle 触发的退出中也通知 workers

**文件**: `internal/worker/codexcli/manager.go`

当前 `monitorProcess` 仅在 `refs > 0` 时关闭 `crashCh`。但 `KillIfIdle()` 是由 worker 的 `Terminate/Kill` 触发的，此时 `release()` 已将 refs 归零，所以 `monitorProcess` 跳过了 crashCh 关闭。

虽然 Fix 1 已解决 `Wait()` 阻塞问题（doneCh 路径），但为保持语义一致性，可考虑在 `KillIfIdle` 路径中记住 "kill origin"，让 `monitorProcess` 正确区分 crash vs intentional kill。

**此 Fix 为可选改进**，非阻塞项。

### 3.3 Fix 3: Resume 死代码路径（代码质量）

**文件**: `internal/gateway/bridge.go`（实际实现位置）

> **Note**: 原始 spec 建议在 `resumeWithOpts` 或 `createAndLaunchWorker` 中跳过 resume。
> 实际实现位于 `StartPlatformSession` 的 `StateTerminated` 分支（bridge.go:404-412），
> 通过查询 `worker.CanResumeTerminated(wt)` capability 接口跳过无效 resume。
> CodexCLI 的 `CanResumeTerminated()` 返回 `false`，因为 singleton 进程在 release 时被终止，
> thread context 已不可恢复。

当前 bridge resume 流程对 CodexCLI 必定失败：
1. `resumeWithOpts()` 创建新的 `AppServerWorker`（state=`appStateNew`）
2. 调用 `w.Resume()` → 检测到 `appStateNew` → 返回 error
3. bridge fallback 到 `Start()`

这是正确行为（CodexCLI 不支持跨进程 resume），但每次 resume 都浪费一次 acquire + config load + detach/attach 周期。

**改进**: 通过 `worker.Capabilities.CanResumeTerminated()` 接口，在 bridge 层跳过不可恢复的 terminated session resume，直接走 `startOrResumeOnInUse`。新增 worker 类型只需实现 `CanResumeTerminated()` 返回正确值，无需修改 bridge。

---

## 4. Implementation Plan

### Phase 1: 核心修复（必须）

| Step | File | Change | Test |
|---|---|---|---|
| 1 | `worker.go:release()` | 移除 `w.doneCh = nil` | 单元测试：release 后 Wait 立即返回 |
| 2 | `worker.go:Wait()` | 加锁原子捕获 doneCh + crashSub | 单元测试：Wait 在 release 前后均安全 |

### Phase 2: 可选改进

| Step | File | Change | Test |
|---|---|---|---|
| 3 | `manager.go:monitorProcess` | 区分 crash vs kill origin | 集成测试 |
| 4 | `bridge.go:resumeWithOpts` | 跳过 codex_cli resume | 集成测试 |

### Test Plan

```
TestAppServerWorkerWait_AfterRelease_ReturnsImmediately
  - 创建 worker，调用 release()
  - 验证 Wait() 返回 (0, nil) 且无阻塞

TestAppServerWorkerWait_BeforeRelease_BlocksUntilRelease
  - 创建 worker，并发调用 Wait()
  - 验证 Wait() 阻塞直到 release() 被调用
  - 验证 Wait() 返回 (0, nil)

TestAppServerWorkerWait_CrashPath
  - 创建 worker，模拟 crashCh 关闭
  - 验证 Wait() 返回 (1, nil)

TestAppServerWorkerKill_DoubleRelease_Guard
  - 调用 Terminate() 然后 Kill()
  - 验证无 panic（doneCh 不被 double-close）

TestAppServerWorkerWait_NilChannels_ReturnsZero
  - 创建未初始化的 worker（crashSub=nil, doneCh=nil）
  - 验证 Wait() 返回 (0, nil) 而非阻塞
```

---

## 5. Risk Assessment

| 风险 | 概率 | 影响 | 缓解 |
|---|---|---|---|
| doneCh double-close | 极低 | panic | `w.closed` guard 已保护；增加 TestAppServerWorkerKill_DoubleRelease_Guard |
| Wait() 语义变更 | 低 | 上层 bridge 行为变化 | Wait() 返回值不变（0 或 1），bridge 无需改动 |
| 其他 worker 类型受影响 | 无 | — | 仅改 CodexCLI worker，ClaudeCode/OCS/ACP 使用 BaseWorker.Wait() |

---

## 6. Acceptance Criteria

- [ ] `Wait()` 在 `release()` 后立即返回，不阻塞
- [ ] `Wait()` 在 `release()` 前阻塞，release 后唤醒
- [ ] CodexCLI session 正常终止时无 `Wait() timed out` 日志
- [ ] CodexCLI session 正常终止时无 `abandoning` 日志
- [ ] `go test -race -count=1 ./internal/worker/codexcli/...` 通过
- [ ] `go test -race -count=1 ./internal/gateway/...` 通过
