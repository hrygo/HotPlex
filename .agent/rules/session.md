---
paths:
  - "**/session/*.go"
---

# Session 管理规范

> Session 状态机、GC 策略、并发控制、mutex 规范
> 参考：`docs/specs/Acceptance-Criteria.md` §SM-001 ~ §SM-008

## 5 状态机

```
CREATED → RUNNING → IDLE → TERMINATED → DELETED
   ↑                    ↓            ↑
   └─── RESUME ←────────┘    │
          └──────────────────────┘
```

| 状态 | IsActive() | 说明 |
|------|-----------|------|
| `CREATED` | true | Session 创建，未开始执行 |
| `RUNNING` | true | 正在执行 Worker |
| `IDLE` | true | **P1 Fix**: WebSocket 关闭时转入此状态，等待重连恢复 |
| `TERMINATED` | false | 结束，保留元数据 |
| `DELETED` | false | 终态，DB 记录已删除 |

### P1: Session Orphan 防护规则

**WebSocket 关闭时**：
```go
// conn.go ReadPump defer
defer func() {
    c.hb.Stop()
    c.Close()
    c.hub.UnregisterConn(c)
    
    // P1 Fix: Transition to IDLE instead of TERMINATED
    if c.sessionID != "" {
        if err := handler.sm.Transition(ctx, c.sessionID, events.StateIdle); err != nil {
            c.log.Debug("gateway: conn close transition to idle", "session_id", c.sessionID, "err", err)
        }
    }
    c.hub.LeaveSession(c.sessionID, c)
}()
```

**重连恢复时 (ResumeSession)**：
```go
// bridge.go ResumeSession
func (b *Bridge) ResumeSession(ctx context.Context, id string) error {
    si, err := b.sm.Get(id)
    
    // P1 Fix: Clean up stale worker from previous connection
    if existing := b.sm.GetWorker(id); existing != nil {
        // Terminate with panic recovery
        func() {
            defer func() {
                if r := recover(); r != nil {
                    b.log.Warn("bridge: GetWorker panicked", "err", r, "session_id", id)
                }
            }()
            _ = existing.Terminate(ctx)
        }()
        b.sm.DetachWorker(id)
    }
    
    // Create and attach new worker
    w, err := b.wf.NewWorker(si.WorkerType)
    b.sm.AttachWorker(id, w)
    
    // Transition IDLE → RUNNING
    b.sm.Transition(ctx, id, events.StateRunning)
    
    return b.hub.SendToSession(ctx, stateEvt)
}
```

**关键原则**：
- StateIdle 是"暂停"状态，worker 暂停但未终止
- StateTerminated 是"终止"状态，需要完全重启
- 重连时检测 StateIdle 会触发 ResumeSession
- ResumeSession 先清理旧 worker（防止泄漏），再创建新 worker

### P0: Session ID 生成规则

**客户端禁止生成 session_id**：
- 客户端在 WebSocket open 时不生成 session_id
- 服务器在 `init` 握手时生成 session_id
- `init_ack` 返回服务器生成的 session_id
- 客户端使用 `init_ack` 中的 session_id 进行后续通信

```go
// browser-client.ts
// ❌ 错误: 客户端不生成 session_id
// const sessionId = generateSessionId()

// ✅ 正确: 从 init_ack 获取
const initAck = await init(sessionId)
this.sessionId = initAck.session_id
```

### 合法转换规则
```go
var ValidTransitions = map[State][]State{
    CREATED:    {RUNNING, TERMINATED},
    RUNNING:    {IDLE, TERMINATED},
    IDLE:       {RUNNING, TERMINATED},
    TERMINATED: {RUNNING, DELETED}, // resume / GC
    DELETED:    {},                  // 终态
}
```

### Turn 生命周期
- `CREATED → RUNNING`：fork+exec 成功或 resume
- `RUNNING → IDLE`：Worker 执行完毕
- `IDLE → RUNNING`：收到新 input
- `IDLE → TERMINATED`：idle_timeout / max_lifetime / GC kill
- `TERMINATED → RUNNING`：resume（重启 runtime）
- `TERMINATED → DELETED`：GC retention_period 过期

---

## TransitionWithInput 原子性

**核心原则**：状态转换和 input 处理**必须在同一 mutex 内完成**，防止竞态。

```go
func (ms *managedSession) TransitionWithInput(ctx context.Context, content string) error {
    ms.mu.Lock()
    defer ms.mu.Unlock()

    // 1. 状态检查
    if !IsActive(ms.info.State) {
        return ErrSessionNotActive
    }
    if ms.info.State == RUNNING {
        return ErrSessionBusy
    }

    // 2. 原子转换 + 记录 input
    if err := ms.sm.Transition(RUNNING); err != nil {
        return err
    }
    return ms.store.RecordInput(ms.info.ID, content)
}
```

### done/input 竞态防护
当 Worker 发送 `done` 同时 Client 发送 `input`：
- 两者共享 `ms.mu.Lock`，input 的 state 检查和转换原子完成
- 第二个并发 input 收到 `SESSION_BUSY`

---

## SESSION_BUSY 硬拒绝

Session 不处于 `CREATED/RUNNING/IDLE` 状态时，**硬拒绝** input，不排队。

```go
func (sm *SessionManager) HandleInput(sessionID, content string) error {
    ms, err := sm.Get(sessionID)
    if err != nil {
        return err
    }
    return ms.TransitionWithInput(ctx, content)
    // err == ErrSessionBusy → 回复 error.code="SESSION_BUSY"
}
```

---

## mutex 规范

```go
// ✅ 正确：显式命名、零值、不 embedding
type managedSession struct {
    mu   sync.RWMutex
    info *SessionInfo
}

// ✅ 正确：写锁用于 TransitionWithInput
func (ms *managedSession) TransitionWithInput(...) error {
    ms.mu.Lock()
    defer ms.mu.Unlock()
}

// ✅ 正确：读锁用于 Get
func (ms *managedSession) Get() *SessionInfo {
    ms.mu.RLock()
    defer ms.mu.RUnlock()
    return ms.info
}

// ❌ 禁止：禁止指针传递
func foo(mu *sync.Mutex) {}  // 禁止

// ❌ 禁止：禁止 embedding
type Bad struct {
    sync.Mutex  // 禁止
}
```

---

## GC 策略

### 触发间隔
```go
scanInterval := cfg.Session.GCScanInterval // 默认 60s
```

### 清理规则
| 条件 | 操作 |
|------|------|
| IDLE session idle_expires_at ≤ now | → TERMINATED（idle_timeout） |
| session expires_at ≤ now（max_lifetime） | → TERMINATED（max_lifetime） |
| RUNNING session LastIO() > execution_timeout | → TERMINATED（zombie） |
| TERMINATED session updated_at ≤ now - retention_period | → DELETE FROM sessions |

### GC goroutine shutdown
```go
func (sm *SessionManager) runGC(ctx context.Context) {
    ticker := time.NewTicker(scanInterval)
    defer ticker.Stop()
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            sm.scan()
        }
    }
}
```

---

## PoolManager 配额

```go
// 全局配额
MaxPoolSize    = 20  // 全局最大活跃 Worker
MaxIdlePerUser = 5   // per-user 最大空闲 Session

func (p *PoolManager) Acquire(userID string) error {
    if p.totalCount.Load() >= MaxPoolSize {
        return ErrPoolExhausted
    }
    if p.perUserCount(userID) >= MaxIdlePerUser {
        return ErrUserQuotaExceeded
    }
    p.totalCount.Add(1)
    p.userCounts[userID].Add(1)
    return nil
}
```

---

## SQLite WAL 模式

```go
func NewSQLiteStore(path string) (*SQLiteStore, error) {
    db, err := sql.Open("sqlite", path)
    if err != nil {
        return nil, err
    }
    // 必须启用 WAL + busy_timeout
    if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
        return nil, err
    }
    if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
        return nil, err
    }
    // 写入通过单写 goroutine 串行化
}
```
