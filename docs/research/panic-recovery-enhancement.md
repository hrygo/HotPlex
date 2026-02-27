# Panic Recovery Enhancement - System Research Report

## Executive Summary

**Objective**: 系统深度调研，规避系统整体进程崩溃，加强 Panic Recovery 能力

**Key Finding**: HotPlex 存在严重的 panic recovery 缺陷 - 整个代码库仅 1 处 recover()，但有 30+ 处 goroutine spawn 点缺乏保护。

**Risk Level**: 🔴 HIGH - 单个 goroutine panic 可导致整个进程崩溃

---

## 1. Current State Analysis

### 1.1 Panic Recovery Coverage

| Component | Has Recover | Goroutines | Risk |
|-----------|-------------|------------|------|
| `chatapps/slack/socket_mode.go` | ✅ Yes (line 280) | 2 | LOW |
| `internal/engine/session.go` | ❌ No | 3 | **HIGH** |
| `internal/engine/pool.go` | ❌ No | 1 | **HIGH** |
| `internal/server/hotplex_ws.go` | ❌ No | 1 | **HIGH** |
| `chatapps/slack/adapter.go` | ❌ No | 2 | **MEDIUM** |
| `chatapps/base/adapter.go` | ❌ No | 2 | **MEDIUM** |
| `chatapps/base/webhook.go` | ❌ No | 1 | **MEDIUM** |
| `chatapps/health.go` | ❌ No | 1 | **LOW** |
| `chatapps/manager.go` | ❌ No | 1 | **LOW** |
| `cmd/hotplexd/main.go` | ❌ No | 1 | **LOW** |

### 1.2 Existing Recovery Pattern (Only Example)

```go
// chatapps/slack/socket_mode.go:277-290
func (s *SocketModeConnection) readLoop() {
    defer func() {
        if r := recover(); r != nil {
            s.logger.Error("Panic recovered in readLoop", "recover", r)
        }
        // ... cleanup code
    }()
    // ... main loop
}
```

---

## 2. Identified Risk Points

### 2.1 Critical Risk - Core Engine

#### internal/engine/session.go

| Line | Goroutine | Risk Description |
|------|-----------|------------------|
| 108 | `go func() { waitForReady... }` | 无 recover，若 isAliveLocked() panic 会崩溃 |
| 323 | `go sess.ReadStdout()` | 处理外部 CLI 输出，无 recover |
| 324 | `go sess.ReadStderr()` | 处理外部 CLI 错误，无 recover |

**Impact**: CLI 进程异常输出可能导致整个 hotplexd 进程崩溃

#### internal/engine/pool.go

| Line | Goroutine | Risk Description |
|------|-----------|------------------|
| 326 | `go func() { cmd.Wait()... }` | 监控子进程退出，无 recover |

**Impact**: 子进程状态监控失败可能导致资源泄漏 + 进程崩溃

### 2.2 High Risk - Server Layer

#### internal/server/hotplex_ws.go

| Line | Goroutine | Risk Description |
|------|-----------|------------------|
| 131 | `go h.handleExecute(...)` | 处理客户端执行请求，无 recover |

**Impact**: 恶意/畸形请求处理 panic 会崩溃整个 WebSocket 服务

### 2.3 Medium Risk - Adapter Layer

#### chatapps/slack/adapter.go

| Line | Goroutine | Risk Description |
|------|-----------|------------------|
| 516 | `go func() { handleResetCommand... }` | HTTP webhook path |
| 654 | `go func() { handleResetCommand... }` | Socket Mode path |

#### chatapps/slack/socket_mode.go

| Line | Goroutine | Risk Description |
|------|-----------|------------------|
| 330 | `go func() { reconnect() }` | 重连逻辑无保护 |

#### chatapps/base/adapter.go

| Line | Goroutine | Risk Description |
|------|-----------|------------------|
| 225 | `go func() { ListenAndServe... }` | HTTP 服务器 |
| 233 | `go a.cleanupSessions()` | 会话清理循环 |

#### chatapps/base/webhook.go

| Line | Goroutine | Risk Description |
|------|-----------|------------------|
| 32 | `go func() { handler(ctx, msg)... }` | 消息处理，可能调用用户代码 |

---

## 3. Root Cause Analysis

### 3.1 Why This Matters

Go 的 panic 传播机制：
1. Panic 在 goroutine 内传播
2. 若 goroutine 内 panic 未被 recover，整个程序终止
3. **没有任何"上层"recover 可以捕获子 goroutine 的 panic**

```
main()
  └── go funcA()  ──panic──> 💥 整个进程崩溃
          │
          └── (无 recover)
```

### 3.2 Current Architecture Gap

```
                    ┌─────────────────────┐
                    │   hotplexd main     │
                    │   (NO recover)      │
                    └──────────┬──────────┘
                               │
        ┌──────────────────────┼──────────────────────┐
        │                      │                      │
        ▼                      ▼                      ▼
┌───────────────┐    ┌───────────────┐    ┌───────────────┐
│  HTTP Server  │    │  Slack Socket │    │  Engine Pool  │
│  (NO recover) │    │  (PARTIAL)    │    │  (NO recover) │
└───────┬───────┘    └───────┬───────┘    └───────┬───────┘
        │                    │                    │
        ▼                    ▼                    ▼
   go handleExecute    go readLoop ✅      go ReadStdout
   go handleReset      go reconnect❌     go cmd.Wait
```

---

## 4. Recommended Solutions

### 4.1 Solution Architecture

创建统一的 `panicx` 包，提供标准化的 panic recovery 模式。

```
internal/panicx/
├── recover.go      # Core recovery utilities
├── goroutine.go    # Safe goroutine launcher
└── doc.go          # Package documentation
```

### 4.2 Implementation Strategy

#### Phase 1: Core Infrastructure (内部包)

```go
// internal/panicx/goroutine.go
package panicx

import (
    "fmt"
    "log/slog"
    "runtime/debug"
)

// SafeGo launches a goroutine with panic recovery.
// Logs the panic with full stack trace and optional context.
func SafeGo(logger *slog.Logger, fn func()) {
    go func() {
        defer Recover(logger, "SafeGo")
        fn()
    }()
}

// SafeGoWithContext launches a goroutine with context and panic recovery.
func SafeGoWithContext(ctx context.Context, logger *slog.Logger, fn func(context.Context)) {
    go func() {
        defer Recover(logger, "SafeGoWithContext")
        fn(ctx)
    }()
}

// Recover handles panic recovery with structured logging.
// Should be called via defer in goroutine entry points.
func Recover(logger *slog.Logger, context string) {
    if r := recover(); r != nil {
        stack := debug.Stack()
        if logger != nil {
            logger.Error("Panic recovered",
                "context", context,
                "panic", fmt.Sprintf("%v", r),
                "stack", string(stack),
            )
        }
    }
}
```

#### Phase 2: Critical Path Fixes (Priority Order)

| Priority | File | Line | Fix |
|----------|------|------|-----|
| 1 | `internal/engine/session.go` | 108, 323, 324 | Add recover to all goroutines |
| 2 | `internal/engine/pool.go` | 326 | Add recover to cmd.Wait monitor |
| 3 | `internal/server/hotplex_ws.go` | 131 | Use SafeGo for handleExecute |
| 4 | `chatapps/slack/adapter.go` | 516, 654 | Add recover to handleResetCommand |
| 5 | `chatapps/base/webhook.go` | 32 | Add recover to webhook handler |
| 6 | `chatapps/slack/socket_mode.go` | 330 | Add recover to reconnect goroutine |

#### Phase 3: Systematic Adoption

Replace all `go func()` patterns with `panicx.SafeGo()`:

```go
// Before (vulnerable)
go func() {
    if err := a.handleResetCommand(cmd); err != nil {
        a.Logger().Error("handleResetCommand failed", "error", err)
    }
}()

// After (protected)
panicx.SafeGo(a.Logger(), func() {
    if err := a.handleResetCommand(cmd); err != nil {
        a.Logger().Error("handleResetCommand failed", "error", err)
    }
})
```

### 4.3 Recovery Policy Design

```go
// RecoveryPolicy defines how to handle panics
type RecoveryPolicy int

const (
    PolicyLogAndContinue RecoveryPolicy = iota // Log, continue service
    PolicyLogAndRestart                         // Log, restart component
    PolicyLogAndShutdown                        // Log, graceful shutdown
)

// WithPolicy applies recovery policy after panic
func WithPolicy(logger *slog.Logger, policy RecoveryPolicy, context string) {
    if r := recover(); r != nil {
        stack := debug.Stack()
        logger.Error("Panic recovered",
            "context", context,
            "panic", fmt.Sprintf("%v", r),
            "stack", string(stack),
            "policy", policy,
        )

        switch policy {
        case PolicyLogAndRestart:
            // Signal restart channel
        case PolicyLogAndShutdown:
            // Trigger graceful shutdown
        }
    }
}
```

---

## 5. Metrics & Observability

### 5.1 Panic Metrics

```go
// Add to observability system
var panicCounter = prometheus.NewCounterVec(
    prometheus.CounterOpts{
        Name: "hotplex_panic_recoveries_total",
        Help: "Total number of panic recoveries",
    },
    []string{"component", "goroutine"},
)

func RecoverWithMetrics(logger *slog.Logger, component, goroutine string) {
    if r := recover(); r != nil {
        panicCounter.WithLabelValues(component, goroutine).Inc()
        // ... existing recovery logic
    }
}
```

### 5.2 Alerting Rules

```yaml
# Prometheus alerting rule
groups:
  - name: hotplex_panics
    rules:
      - alert: HighPanicRate
        expr: rate(hotplex_panic_recoveries_total[5m]) > 0.1
        for: 1m
        labels:
          severity: warning
        annotations:
          summary: "High panic recovery rate detected"
```

---

## 6. Testing Strategy

### 6.1 Unit Tests

```go
// internal/panicx/recover_test.go
func TestRecoverCatchesPanic(t *testing.T) {
    var buf bytes.Buffer
    logger := slog.New(slog.NewJSONHandler(&buf, nil))

    done := make(chan bool)
    go func() {
        defer Recover(logger, "test")
        panic("test panic")
    }()

    time.Sleep(100 * time.Millisecond)

    if !strings.Contains(buf.String(), "test panic") {
        t.Error("Panic was not logged")
    }
}
```

### 6.2 Integration Tests

```go
// Test that panic in one session doesn't crash others
func TestSessionPanicIsolation(t *testing.T) {
    pool := createTestPool(t)

    // Create multiple sessions
    sess1, _ := pool.CreateSession(...)
    sess2, _ := pool.CreateSession(...)

    // Trigger panic in sess1's stdout reader
    // Verify sess2 continues to work
}
```

---

## 7. Implementation Checklist

### Phase 1: Core Infrastructure
- [ ] Create `internal/panicx` package
- [ ] Implement `SafeGo`, `Recover`, `WithPolicy`
- [ ] Add unit tests
- [ ] Add metrics integration

### Phase 2: Critical Fixes
- [ ] Fix `internal/engine/session.go` (3 goroutines)
- [ ] Fix `internal/engine/pool.go` (1 goroutine)
- [ ] Fix `internal/server/hotplex_ws.go` (1 goroutine)

### Phase 3: Adapter Layer
- [ ] Fix `chatapps/slack/adapter.go` (2 goroutines)
- [ ] Fix `chatapps/base/webhook.go` (1 goroutine)
- [ ] Fix `chatapps/slack/socket_mode.go` (1 goroutine)

### Phase 4: System-wide
- [ ] Audit all remaining goroutines
- [ ] Update coding standards
- [ ] Add pre-commit hook for `go func()` pattern check

---

## 8. References

- [Go Blog: Defer, Panic, and Recover](https://go.dev/blog/defer-panic-and-recover)
- [Uber Go Style Guide: Don't Panic](https://github.com/uber-go/guide/blob/master/style.md#dont-panic)
- [Effective Go: Recover](https://go.dev/doc/effective_go#recover)

---

## 9. Conclusion

当前 HotPlex 的 panic recovery 机制严重不足。建议按照上述方案分阶段实施：

1. **立即**: 创建 `internal/panicx` 基础设施
2. **短期**: 修复核心引擎 的关键路径
3. **中期**: 覆盖所有 adapter 层
4. **长期**: 建立自动化检测机制

**预期收益**:
- 单个组件 panic 不再导致进程崩溃
- 完整的 panic 日志和堆栈追踪
- 可观测性和告警能力
- 符合生产级可靠性标准
