# HotPlex 持久会话机制规格书

> 版本: v1.2
> 日期: 2026-04-06
> 状态: 设计完成，待实现
> 交叉复核: 已对齐 `pkg/events/events.go`、`internal/gateway/handler.go`、`internal/session/manager.go` 源码

---

## 1. 概述

### 1.1 目标

HotPlex Worker Gateway 持久会话机制，支持：

1. **客户端管理的 Session ID** — 客户端通过 `init.session_id` 上送 `client_session_id`，服务端用 UUIDv5 做一致性映射，确保相同 `(owner_id, worker_type, client_session_id)` 永远映射为同一服务端 session
2. **会话重置 (reset)** — 清空 `Session.Context`，终止并重建 Worker（相同 sessionID），Worker 内部删除旧会话文件，状态切至 `RUNNING`
3. **会话归档 (gc)** — 终止 Worker（Worker 内部自行保存状态），状态切至 `TERMINATED`，后续可 resume

### 1.2 设计原则

- **确定性映射**：UUIDv5 算法，同一组输入永远生成相同输出
- **分层透明**：Worker 会话持久化由 Worker 自行实现，上层 Gateway 只发指令
- **reset 实现自由**：Gateway 负责清空 `Session.Context`；Worker 负责清空运行时上下文（in-place 指令 or terminate+start，由 Worker 自行决定）

### 1.3 分层职责

```
┌────────────────────────────────────────────────────────────────┐
│                     HotPlex Gateway (上层)                      │
│                                                                │
│  Session 状态机 + 消息路由 + Worker 生命周期管理               │
│  不感知会话持久化细节（由 Worker 自行实现）                      │
└────────────────────────────────────────────────────────────────┘

  reset  →  sm.ClearContext  →  w.ResetContext  →  Transition(RUNNING)
  gc     →  w.Terminate     →  DetachWorker →  Transition(TERMINATED)
  resume →  Bridge.ResumeSession → w.Resume  →  Transition(RUNNING)

  reset 说明:
    Gateway: sm.ClearContext() → 清空 SessionInfo.Context
    Worker:  w.ResetContext()   → 清空运行时上下文（in-place 或 terminate+start，由 Worker 决定）

┌────────────────────────────────────────────────────────────────┐
│                    Worker Adapter (下层)                         │
│                                                                │
│  各 Worker 自行实现会话持久化，上层透明：                         │
│  ClaudeCode:  claude --resume <session_id>                      │
│  OpenCodeCLI: opencode run --resume <session_id>                │
│  OpenCodeSrv:  HTTP POST /session/<id>/resume                  │
│                                                                │
│  reset 实现（各 Worker 自行决定）：                              │
│  ClaudeCode:  terminate + start（claude 删除旧会话文件）         │
│  OpenCodeCLI: terminate + start                                │
│  OpenCodeSrv:  发送 HTTP POST /session/<id>/reset             │
└────────────────────────────────────────────────────────────────┘
```

---

## 2. Session ID 映射机制

### 2.1 UUIDv5 映射算法

```go
// internal/session/key.go（新建）

package session

import (
    "github.com/google/uuid"
    "github.com/hotplex/hotplex-worker/internal/worker"
)

// hotplexNamespace 是 HotPlex 专属命名空间 UUID（RFC 4122 §4.3）。
// 使用固定值确保跨环境一致性。
var hotplexNamespace = uuid.MustParse("urn:uuid:6ba7b810-9dad-11d1-80b4-00c04fd430c8")

// DeriveSessionKey generates a deterministic server-side session ID using UUIDv5.
// Same (ownerID, workerType, clientSessionID) always maps to the same session.
func DeriveSessionKey(ownerID string, wt worker.WorkerType, clientSessionID string) string {
    // UUIDv5 = SHA-1(name) with namespace
    name := ownerID + "|" + string(wt) + "|" + clientSessionID
    id := uuid.NewHash(hotplexNamespace, name)
    return id.String() // 格式: xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
}
```

### 2.2 init 流程

```
Client init{session_id: "my-chat-001"}
  → DeriveSessionKey(ownerID="user_001", wt="claude_code", clientSessionID="my-chat-001")
  → UUIDv5: "550e8400-e29b-41d4-a716-446655440000"
  → sm.GetOrCreate("550e8400-e29b-41d4-a716-446655440000")
      ├─ 存在 → 返回现有 session（idempotent）
      └─ 不存在 → 创建新 session
```

### 2.3 conn.go 改动

```go
// internal/gateway/conn.go:performInit

// Before
sessionID := initData.SessionID
if sessionID == "" {
    sessionID = c.sessionID
}

// After（无向后兼容）
sessionID := session.DeriveSessionKey(c.userID, initData.WorkerType, initData.SessionID)
```

### 2.4 行为矩阵

| 场景 | 行为 |
|------|------|
| `client_session_id` 存在 | UUIDv5 映射，确定性查找/创建 |
| `client_session_id` 不同 | 映射为不同 sessionID |
| 相同三元组重连 | 映射为同一 sessionID → resume |

---

## 3. Session 状态机

### 3.1 现有状态（源码 `pkg/events/events.go`）

```go
const (
    StateCreated    SessionState = "created"
    StateRunning    SessionState = "running"
    StateIdle       SessionState = "idle"
    StateTerminated SessionState = "terminated"
    StateDeleted    SessionState = "deleted"
)

var ValidTransitions = map[SessionState]map[SessionState]bool{
    StateCreated:    {StateRunning: true, StateTerminated: true},
    StateRunning:    {StateIdle: true, StateTerminated: true, StateDeleted: true},
    StateIdle:       {StateRunning: true, StateTerminated: true, StateDeleted: true},
    StateTerminated: {StateRunning: true, StateDeleted: true},  // resume
    StateDeleted:    {},  // 终态
}
```

### 3.2 状态流转图

```
                          input/resume       reset                  gc/idle_timeout
    ┌────────┐       ┌────────────────┐       ┌────────────┐
    │CREATED │──────►│    RUNNING     │──────►│    IDLE   │
    └────────┘ start  └────────────────┘  done  └─────┬──────┘
                                                       │
                                                       │  gc / idle_timeout
                                                       ▼
                                                  ┌────────────┐
                                                  │ TERMINATED │
                                                  └──────┬─────┘
                                                         │
                                                 retention_period
                                                         │
                                                         ▼
                                                    ┌─────────┐
                                                    │ DELETED │
                                                    └─────────┘
```

| 触发 | 转换 | Worker | 说明 |
|------|------|--------|------|
| `control.reset` | `*` → `RUNNING` | Worker 决定 | Gateway 清 Context；Worker 清自身上下文（in-place 或 terminate+start） |
| `control.gc` | `*` → `TERMINATED` | 终止 | Worker 内部保存状态 |
| WS 断开 | `*` → `IDLE` | 暂停 | Worker 暂停，不终止 |
| resume | `IDLE/TERMINATED` → `RUNNING` | 重建 | 发送 resume 指令 |

---

## 4. Control 事件

### 4.1 新增常量

```go
// pkg/events/events.go

const (
    // ... 现有常量 ...
    ControlActionTerminate  ControlAction = "terminate"
    ControlActionDelete     ControlAction = "delete"

    // 新增
    ControlActionReset ControlAction = "reset"  // 清空上下文，Worker 自行决定 in-place 或 terminate+start
    ControlActionGC    ControlAction = "gc"     // 归档会话，Worker 终止，保留历史
)
```

### 4.2 reset 操作

```
┌────────────────────────────────────────────────────────────────┐
│                        control.reset                            │
├────────────────────────────────────────────────────────────────┤
│  目标: Session.Context 必须清空                                  │
│                                                                │
│  触发: client → gateway (event.type="control")                 │
│  payload: {"action": "reset", "reason": "user_requested"}   │
│                                                                │
│  前置条件:                                                     │
│  - Session.State ∈ {CREATED, RUNNING, IDLE}                  │
│  - Worker 已 attached                                          │
│                                                                │
│  行为:                                                        │
│  1. Gateway: sm.ClearContext(sessionID)                      │
│     → SessionInfo.Context = {}                                │
│  2. Gateway: w.ResetContext(ctx)                             │
│     → Worker 内部清空运行时上下文                               │
│        ├─ Worker 支持 in-place 指令 → 发送 reset 信号，Worker 保持 │
│        └─ Worker 不支持 → terminate + start（物理删除会话文件）  │
│  3. sm.TransitionWithReason(sessionID, StateRunning, "reset")│
│                                                                │
│  响应: gateway → client                                       │
│  → event.type="state", data={"state": "running", "message": "context_reset"}
│                                                                │
│  后置:                                                        │
│  - Session.Context = {}                                        │
│  - Worker 运行时上下文已清空                                     │
│  - 下一条 input 开始全新对话                                    │
└────────────────────────────────────────────────────────────────┘
```

### 4.3 gc 操作

```
┌────────────────────────────────────────────────────────────────┐
│                         control.gc                              │
├────────────────────────────────────────────────────────────────┤
│  触发: client → gateway (event.type="control")                 │
│  payload: {"action": "gc", "reason": "user_idle"}             │
│                                                                │
│  前置条件:                                                      │
│  - Session.State ∈ {CREATED, RUNNING, IDLE}                  │
│  - Worker 已 attached                                           │
│                                                                │
│  行为:                                                          │
│  1. worker.Terminate(ctx)                                      │
│     → Worker 内部自行保存会话状态                                 │
│  2. sm.DetachWorker(sessionID)                                 │
│  3. sm.TransitionWithReason(sessionID, StateTerminated, "gc")│
│                                                                │
│  响应: gateway → client                                        │
│  → event.type="state", data={"state": "terminated", "message": "session_archived"}
│                                                                │
│  后置:                                                          │
│  - Worker 已终止/断开                                            │
│  - 会话历史由 Worker 内部保留                                     │
│  - 可通过 resume 恢复                                            │
└────────────────────────────────────────────────────────────────┘
```

---

## 5. 实现变更清单

### 5.1 文件变更总览

| 文件 | 变更类型 | 说明 |
|------|---------|------|
| `pkg/events/events.go` | 修改 | +2 常量: `ControlActionReset`, `ControlActionGC` |
| `internal/session/key.go` | 新建 | `DeriveSessionKey()` — UUIDv5 映射 |
| `internal/session/manager.go` | 修改 | +1 方法: `ClearContext()` |
| `internal/gateway/conn.go` | 修改 | `performInit` 调用 `DeriveSessionKey` |
| `internal/gateway/handler.go` | 修改 | `handleControl` +2 case: `handleReset`, `handleGC` |
| `internal/worker/worker.go` | 修改 | +1 方法: `Worker.ResetContext()` — Worker 自行决定清空方式 |

**共 6 个文件**（1 新建，5 修改）。

### 5.7 internal/worker/worker.go

```go
// Worker 新增方法
type Worker interface {
    // ... 现有方法 ...

    // ResetContext 清空 Worker 运行时上下文。
    // Worker 自行决定实现方式：
    // - 支持 in-place 清空的 Worker → 发送内部 reset 信号
    // - 不支持的 Worker → terminate + start（物理删除会话文件）
    // 注意：Gateway 层已通过 sm.ClearContext() 清空 SessionInfo.Context。
    ResetContext(ctx context.Context) error
}
```

**实现示例**：

```go
// ClaudeCodeWorker: 不支持 in-place 清空 → terminate + start
func (w *Worker) ResetContext(ctx context.Context) error {
    // 1. 终止旧进程（claude 会删除自身会话文件）
    if err := w.Terminate(ctx); err != nil {
        return fmt.Errorf("terminate: %w", err)
    }
    // 2. 重建进程（使用相同 sessionID，claude 会创建全新会话）
    return w.Start(ctx, w.currentSession)
}

// OpenCodeSrvWorker: 支持 in-place 清空 → 发送 reset 请求
func (w *Worker) ResetContext(ctx context.Context) error {
    return w.client.Post("/session/" + w.sessionID + "/reset")
}
```

### 5.2 pkg/events/events.go

```go
// ControlAction 新增常量
const (
    // ... 现有 ...
    ControlActionTerminate  ControlAction = "terminate"
    ControlActionDelete     ControlAction = "delete"

    // 新增
    ControlActionReset ControlAction = "reset"  // 清空上下文，Worker 自行决定 in-place 或 terminate+start
    ControlActionGC    ControlAction = "gc"     // 归档会话，Worker 终止，保留历史
)
```

### 5.3 internal/session/key.go（新建）

```go
package session

import (
    "github.com/google/uuid"
    "github.com/hotplex/hotplex-worker/internal/worker"
)

// hotplexNamespace 是 HotPlex 专属命名空间 UUID（RFC 4122 §4.3）。
// 使用固定值确保跨环境一致性。
var hotplexNamespace = uuid.MustParse("urn:uuid:6ba7b810-9dad-11d1-80b4-00c04fd430c8")

// DeriveSessionKey generates a deterministic server-side session ID using UUIDv5.
// Same (ownerID, workerType, clientSessionID) always maps to the same session.
func DeriveSessionKey(ownerID string, wt worker.WorkerType, clientSessionID string) string {
    // UUIDv5 = SHA-1(namespace+name) — 确定性，无随机性
    name := ownerID + "|" + string(wt) + "|" + clientSessionID
    id := uuid.NewHash(hotplexNamespace, name)
    return id.String()
}
```

**依赖**: `github.com/google/uuid`（检查是否已引入）

### 5.4 internal/session/manager.go

```go
// ClearContext 清空会话上下文。
// 用于 control.reset 操作：Gateway 层清空 SessionInfo.Context。
// Worker 自身运行时的上下文清空由 Worker.ResetContext() 负责（in-place 或 terminate+start）。
func (m *Manager) ClearContext(ctx context.Context, sessionID string) error {
    if m == nil {
        return ErrSessionNotFound
    }
    ms := m.getManagedSession(sessionID)
    if ms == nil {
        return ErrSessionNotFound
    }

    ms.mu.Lock()
    defer ms.mu.Unlock()

    ms.info.Context = map[string]any{}
    ms.info.UpdatedAt = time.Now()

    return m.store.Upsert(ctx, &ms.info)
}
```

### 5.5 internal/gateway/conn.go:performInit

```go
// Before (约 line 234-237)
sessionID := initData.SessionID
if sessionID == "" {
    sessionID = c.sessionID
}

// After（无向后兼容，直接映射）
sessionID := session.DeriveSessionKey(c.userID, initData.WorkerType, initData.SessionID)
```

### 5.6 internal/gateway/handler.go

```go
func (h *Handler) handleControl(ctx context.Context, env *events.Envelope) error {
    data, ok := env.Event.Data.(map[string]any)
    if !ok {
        return h.sendErrorf(ctx, env, events.ErrCodeInvalidMessage, "control: invalid data")
    }

    action, _ := data["action"].(string)
    h.log.Info("gateway: control received", "action", action, "session_id", env.SessionID)

    switch events.ControlAction(action) {
    // ... 现有 case ...

    case events.ControlActionReset:
        return h.handleReset(ctx, env)

    case events.ControlActionGC:
        return h.handleGC(ctx, env)

    default:
        return h.sendErrorf(ctx, env, events.ErrCodeProtocolViolation, "unknown control action: %s", action)
    }
}

func (h *Handler) handleReset(ctx context.Context, env *events.Envelope) error {
    // 1. 所有权校验
    if err := h.sm.ValidateOwnership(ctx, env.SessionID, env.OwnerID, ""); err != nil {
        if errors.Is(err, session.ErrSessionNotFound) {
            return h.sendErrorf(ctx, env, events.ErrCodeSessionNotFound, "session not found")
        }
        return h.sendErrorf(ctx, env, events.ErrCodeUnauthorized, "ownership required")
    }

    // 2. Gateway: 清空 Session.Context
    if err := h.sm.ClearContext(ctx, env.SessionID); err != nil {
        h.log.Warn("gateway: reset clear context failed", "session_id", env.SessionID, "err", err)
        return h.sendErrorf(ctx, env, events.ErrCodeInternalError, "clear context failed: %v", err)
    }

    // 3. Worker: 清空运行时上下文（Worker 自行决定 in-place 或 terminate+start）
    w := h.sm.GetWorker(env.SessionID)
    if w != nil {
        if err := w.ResetContext(ctx); err != nil {
            h.log.Warn("gateway: worker reset context failed", "session_id", env.SessionID, "err", err)
            return h.sendErrorf(ctx, env, events.ErrCodeInternalError, "worker reset failed: %v", err)
        }
    }

    // 4. Session → RUNNING
    if err := h.sm.TransitionWithReason(ctx, env.SessionID, events.StateRunning, "reset"); err != nil {
        return h.sendErrorf(ctx, env, events.ErrCodeInternalError, "reset transition failed: %v", err)
    }

    // 7. 转发 worker 事件
    go h.bridge.ForwardEvents(w, env.SessionID)

    h.log.Info("gateway: session reset", "session_id", env.SessionID)
    return nil
}

func (h *Handler) handleGC(ctx context.Context, env *events.Envelope) error {
    // 1. 所有权校验
    if err := h.sm.ValidateOwnership(ctx, env.SessionID, env.OwnerID, ""); err != nil {
        if errors.Is(err, session.ErrSessionNotFound) {
            return h.sendErrorf(ctx, env, events.ErrCodeSessionNotFound, "session not found")
        }
        return h.sendErrorf(ctx, env, events.ErrCodeUnauthorized, "ownership required")
    }

    // 2. 终止 Worker（Worker 内部自行保存状态）
    if w := h.sm.GetWorker(env.SessionID); w != nil {
        if err := w.Terminate(ctx); err != nil {
            h.log.Warn("gateway: gc worker terminate failed", "session_id", env.SessionID, "err", err)
        }
        h.sm.DetachWorker(env.SessionID)
    }

    // 3. Session → TERMINATED
    if err := h.sm.TransitionWithReason(ctx, env.SessionID, events.StateTerminated, "gc"); err != nil {
        return h.sendErrorf(ctx, env, events.ErrCodeInternalError, "gc transition failed: %v", err)
    }

    // 4. 发送 state 通知
    stateEvt := events.NewEnvelope(aep.NewID(), env.SessionID, h.hub.NextSeq(env.SessionID), events.State, events.StateData{
        State:   events.StateTerminated,
        Message: "session_archived",
    })
    _ = h.hub.SendToSession(ctx, stateEvt)

    h.log.Info("gateway: session gc'd", "session_id", env.SessionID)
    return nil
}
```

---

## 6. AEP 协议消息格式

### 6.1 control.reset

**请求**:
```json
{
  "id": "msg-010",
  "version": "aep/v1",
  "seq": 5,
  "session_id": "550e8400-e29b-41d4-a716-446655440000",
  "event": {
    "type": "control",
    "data": {
      "action": "reset",
      "reason": "user_requested"
    }
  }
}
```

**服务端响应**:
```json
{
  "id": "msg-011",
  "version": "aep/v1",
  "seq": 6,
  "session_id": "550e8400-e29b-41d4-a716-446655440000",
  "event": {
    "type": "state",
    "data": {
      "state": "running",
      "message": "context_reset"
    }
  }
}
```

### 6.2 control.gc

**请求**:
```json
{
  "id": "msg-020",
  "version": "aep/v1",
  "seq": 10,
  "session_id": "550e8400-e29b-41d4-a716-446655440000",
  "event": {
    "type": "control",
    "data": {
      "action": "gc",
      "reason": "user_idle"
    }
  }
}
```

**服务端响应**:
```json
{
  "id": "msg-021",
  "version": "aep/v1",
  "seq": 11,
  "session_id": "550e8400-e29b-41d4-a716-446655440000",
  "event": {
    "type": "state",
    "data": {
      "state": "terminated",
      "message": "session_archived"
    }
  }
}
```

---

## 7. 错误处理

### 7.1 现有错误码（已满足，无需新增）

| 错误码 | 适用场景 |
|--------|---------|
| `SESSION_NOT_FOUND` | session 不存在 |
| `SESSION_BUSY` | 向 RUNNING 状态发送消息 |
| `UNAUTHORIZED` | 所有权校验失败 |
| `INTERNAL_ERROR` | 内部错误 |
| `PROTOCOL_VIOLATION` | 未知 control action |

---

## 8. 测试用例

### 8.1 单元测试

| 测试 | 输入 | 预期 |
|------|------|------|
| `DeriveSessionKey` 确定性 | `("u1", "claude_code", "s1")` × N | 每次相同 UUID |
| `DeriveSessionKey` 差异 | 不同三元组 | 不同 UUID |
| `DeriveSessionKey` UUIDv5 格式 | 任意输入 | `/[0-9a-f]{8}-[0-9a-f]{4}-5[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}/` |
| `ClearContext` | 有 Context 的 session | Context = `{}` |
| `handleReset` | RUNNING session | State → RUNNING, Context = `{}`, Worker.ResetContext() 被调用 |
| `handleGC` | RUNNING session | State → TERMINATED, Worker.Terminate() 被调用 |
| Worker.ResetContext | CLI Worker | Terminate() + Start() |
| Worker.ResetContext | Server Worker | 发送 in-place reset 请求 |

### 8.2 集成测试

| 测试 | 场景 |
|------|------|
| `control.reset` 全流程 | init → input → control.reset → State=RUNNING → input（新 Worker）|
| `control.gc` 全流程 | init → input → control.gc → State=TERMINATED → init(resume) |
| reset 后 input | reset → input → 全新对话，无历史 |
| gc 后 resume | gc → init(resume) → 历史恢复 |
| 非法 reset | 向 TERMINATED 发 reset → `PROTOCOL_VIOLATION` |
| 所有权校验 | 非 owner 发 reset/gc → `UNAUTHORIZED` |

---

## 9. 实现计划

### 阶段一：核心变更
- [ ] `pkg/events/events.go` — 新增 `ControlActionReset` / `ControlActionGC`
- [ ] `internal/session/key.go` — 新建 `DeriveSessionKey()`（UUIDv5）
- [ ] `internal/session/manager.go` — 新增 `ClearContext()`
- [ ] `internal/gateway/conn.go` — `performInit` 调用 `DeriveSessionKey`
- [ ] `internal/gateway/handler.go` — `handleReset` + `handleGC`
- [ ] 单元测试

### 阶段二：文档
- [ ] 更新 `[[architecture/AEP-v1-Protocol]]`
- [ ] 更新 `[[architecture/WebSocket-Full-Duplex-Flow]]`

---

## 10. Changelog

| 日期 | 版本 | 变更 |
|------|------|------|
| 2026-04-06 | 1.2 | 移除向后兼容逻辑；改用 UUIDv5 算法替代 SHA-256 hex |
| 2026-04-06 | 1.1 | 交叉复核源码，精确到文件/行号；明确 minimal change set |
| 2026-04-06 | 1.0 | 初始版本 |

---

## 11. 相关文档

- [[architecture/AEP-v1-Protocol]] — AEP v1 协议规范
- [[architecture/WebSocket-Full-Duplex-Flow]] — WebSocket 全双工通信流程
- [[specs/Worker-ClaudeCode-Spec]] — Claude Code Worker 实现
- [[management/Admin-API-Design]] — Admin API 设计
