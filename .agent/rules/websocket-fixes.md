---
paths:
  - "**/gateway/**/*.go"
  - "**/session/**/*.go"
  - "**/worker/**/*.go"
  - "packages/ai-sdk-transport/src/client/**/*.ts"
---

# WebSocket Bug 修复规范 (P0-P3)

> 2026-04-05 关键 WebSocket 连接问题修复记录
> 参考文档：`docs/architecture/WebSocket-Full-Duplex-Flow.md`

---

## 修复概述

| ID | 问题 | 根因 | 修复 | 影响 |
|----|------|------|------|------|
| **P0** | Session ID 不匹配 | 客户端在 WS open 时生成 session_id | 服务器在 init 握手时生成 | 客户端从 init_ack 获取 session_id |
| **P1** | Session 孤儿泄漏 | WS 关闭时未保留 worker | 转换到 StateIdle + ResumeSession | 支持断线重连 |
| **P2** | Ping 消耗序号 | Ping/pong 消耗 seq | 心跳消息跳过 seq 分配 | 序号连续性 |
| **P3** | macOS RLIMIT_AS 警告 | macOS 不支持 RLIMIT_AS | 平台检测跳过设置 | 日志清洁 |

---

## P0: Session ID 生成规范

### 问题描述
- 客户端在 WebSocket `onopen` 时生成 `session_id`
- 服务器在 `init` 握手时也生成 `session_id`
- 导致 `init_ack` 返回的 session_id 与客户端不一致

### 修复方案

**服务端 (conn.go:performInit)**：
```go
// 服务器生成或复用 session_id
sessionID := initData.SessionID
if sessionID == "" {
    sessionID = c.sessionID  // 使用 conn 创建时的 ID
}
```

**客户端 (browser-client.ts)**：
```typescript
// ❌ 错误: 客户端不生成 session_id
// private generateSessionId(): string { ... }

// ✅ 正确: 从 init_ack 获取
async init(sessionId?: string): Promise<InitAck> {
    const initEnv = {
        id: this.generateId(),
        version: 'aep/v1',
        session_id: sessionId,  // 可选，服务器决定是否使用
        event: { type: 'init', data: initData }
    }
    // ...
}

// 使用 init_ack 返回的 session_id
onMessage((env) => {
    if (env.event.type === 'init_ack') {
        this.sessionId = env.session_id  // 服务器分配的 ID
    }
})
```

**关键原则**：
- Session ID 的权威来源是服务器
- 客户端可以提供建议（可选），但服务器有最终决定权
- `init_ack` 中的 `session_id` 是唯一可信来源

**文件**：
- `internal/gateway/conn.go:performInit`
- `packages/ai-sdk-transport/src/client/browser-client.ts`
- `packages/ai-sdk-transport/src/client/envelope.ts`

---

## P1: Session Orphan 防护规范

### 问题描述
- WebSocket 意外关闭（网络断开、浏览器 tab 关闭）
- Worker 进程继续运行，占用资源
- 重连时创建新 session，旧 worker 泄漏

### 修复方案

**1. WebSocket 关闭时转换到 StateIdle**：
```go
// conn.go ReadPump defer
defer func() {
    c.hb.Stop()
    c.Close()
    c.hub.UnregisterConn(c)

    // P1 Fix: Transition to IDLE instead of terminating
    if c.sessionID != "" {
        if err := handler.sm.Transition(ctx, c.sessionID, events.StateIdle); err != nil {
            c.log.Debug("gateway: conn close transition to idle", "err", err)
        }
    }
    c.hub.LeaveSession(c.sessionID, c)
}()
```

**2. 重连时恢复 Session (ResumeSession)**：
```go
// conn.go performInit - StateIdle branch
} else if si.State == events.StateIdle {
    c.log.Info("gateway: resuming idle session", "session_id", sessionID)
    if c.starter != nil {
        if err := c.starter.ResumeSession(ctx, sessionID); err != nil {
            c.sendInitError(events.ErrCodeInternalError, "failed to resume session")
            return fmt.Errorf("resume session: %w", err)
        }
    }
}

// bridge.go ResumeSession
func (b *Bridge) ResumeSession(ctx context.Context, id string) error {
    // 1. 清理旧 worker（防止泄漏）
    if existing := b.sm.GetWorker(id); existing != nil {
        func() {
            defer func() {
                if r := recover(); r != nil {
                    b.log.Warn("bridge: GetWorker panicked", "err", r)
                }
            }()
            _ = existing.Terminate(ctx)
        }()
        b.sm.DetachWorker(id)
    }

    // 2. 创建新 worker
    w, err := b.wf.NewWorker(si.WorkerType)
    b.sm.AttachWorker(id, w)

    // 3. 恢复执行
    w.Resume(ctx, workerInfo)
    b.sm.Transition(ctx, id, events.StateRunning)

    return nil
}
```

**3. Manager 的 nil guards**：
```go
// session/manager.go
func (m *Manager) GetWorker(id string) worker.Worker {
    if m == nil {  // P1: Test mode guard
        return nil
    }
    // ...
}

func (m *Manager) DetachWorker(id string) {
    if m == nil {  // P1: Test mode guard
        return
    }
    // ...
}
```

**关键原则**：
- `StateIdle` = "暂停"状态，worker 暂停但未终止
- `StateTerminated` = "终止"状态，需要完全重启
- ResumeSession 必须先清理旧 worker（防止泄漏）
- 所有 Manager 方法需要 nil guard（支持 test mode）

**文件**：
- `internal/gateway/conn.go:ReadPump`, `performInit`
- `internal/gateway/bridge.go:ResumeSession`
- `internal/session/manager.go:GetWorker`, `DetachWorker`
- `internal/gateway/handler.go:SessionManager` (interface)

---

## P2: Ping 序号分配规范

### 问题描述
- Ping/pong 消息消耗 seq
- 导致序号不连续（1, 2, [ping=3], 4 → 客户端期望 3 但收到 4）
- 破坏消息流的连续性

### 修复方案

**conn.go ReadPump**：
```go
env.SessionID = c.sessionID
env.OwnerID = c.userID

// P2 Fix: Skip sequence number for heartbeat messages
if env.Event.Type != events.Ping {
    env.Seq = c.hub.NextSeq(c.sessionID)
}
// Ping messages have seq=0 (unassigned)
```

**设计原因**：
- Ping/pong 是 WebSocket 层面的心跳机制
- 与业务消息（input/delta/done）无关
- 客户端收到 ping/pong 不需要按序处理
- 保持业务消息序号的连续性

**关键原则**：
- 只有业务消息（input, delta, done, error, state）消耗 seq
- 控制消息（ping, pong）不消耗 seq
- Seq=0 表示"未分配序号"，用于心跳等控制消息

**文件**：
- `internal/gateway/conn.go:ReadPump:151`

---

## P3: macOS 平台兼容性规范

### 问题描述
- `syscall.Setrlimit(syscall.RLIMIT_AS, ...)` 在 macOS 上失败
- 日志中持续出现 `setrlimit: invalid argument` 警告
- macOS 不支持 RLIMIT_AS（地址空间限制）

### 修复方案

**proc/manager.go Start**：
```go
// P3 Fix: RLIMIT_AS not reliably supported on macOS
if runtime.GOOS != "darwin" && cmd.Process != nil {
    const memLimit = 512 * 1024 * 1024 // 512 MB
    if err := syscall.Setrlimit(syscall.RLIMIT_AS, &syscall.Rlimit{
        Cur: memLimit,
        Max: memLimit,
    }); err != nil {
        m.log.Warn("proc: setrlimit RLIMIT_AS failed", "error", err)
        // Non-fatal: log and continue
    }
}
```

**设计原因**：
- macOS 的 RLIMIT_AS 实现不符合 POSIX 标准
- 调用返回 EINVAL (invalid argument)
- Linux/POSIX 系统正常支持
- 内存限制是优化特性，失败不应阻止进程启动

**关键原则**：
- 使用 `runtime.GOOS` 进行平台检测
- Darwin (macOS) 跳过 RLIMIT_AS 设置
- 其他平台正常设置（Linux, BSD, etc.）
- 失败时记录警告但不中断启动

**文件**：
- `internal/worker/proc/manager.go:138`

---

## 测试覆盖

所有修复都有对应的测试：

**conn_test.go**：
```go
// P1: Test ResumeSession on reconnect
func TestResumeIdleSession(t *testing.T) {
    // ... setup session in StateIdle
    mockSM.On("Get", mockSessionInfo(sessionID, events.StateIdle))
    mockBridge.On("ResumeSession", nil)

    // ... perform init
    require.NoError(t, conn.performInit(handler))
    mockBridge.AssertCalled(t, "ResumeSession", sessionID)
}

// P1: Test nil guards in test mode
func TestNilManagerGuards(t *testing.T) {
    var m *session.Manager
    require.Nil(t, m.GetWorker("test"))
    require.NotPanics(t, func() { m.DetachWorker("test") })
}
```

**运行测试**：
```bash
go test ./internal/gateway -v -run TestResumeIdleSession
go test ./internal/session -v -run TestNilGuards
```

---

## 架构文档更新

相关架构文档已同步更新：
- `docs/architecture/WebSocket-Full-Duplex-Flow.md`
  - 添加了 Connection Close & Reconnect 流程图
  - 添加了 Session Resume 章节
  - 记录了 P0-P3 修复的详细说明

---

## References

- **Commit**: `ab72447` - fix(gateway): session orphan, ping seq, and macOS RLIMIT_AS
- **Commit**: `7609838` - docs(architecture): document P0-P3 WebSocket fixes
- **Architecture**: `docs/architecture/WebSocket-Full-Duplex-Flow.md`
- **Code Review**: All fixes passed simplify review (2026-04-05)
