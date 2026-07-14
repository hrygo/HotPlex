---
type: spec
status: draft
---

# Spec: 停止按钮无条件打断当前 AI 任务（StopCurrentTurn）

**Issue**: 待创建
**Severity**: P1（用户高频功能失效：停止按钮对单例 worker 完全无效，对进程型 worker 延迟最长 5 秒）
**Scope**: `internal/gateway/commands.go`, `pkg/events/events.go`, `internal/worker/{base,claudecode,acp,codexcli,opencodeserver}/*.go`, `webchat/lib/adapters/hotplex-runtime-adapter.ts`, `webchat/lib/ai-sdk-transport/client/{browser-client,envelope}.ts`
**Prerequisite**: 熟悉四种 Worker 的进程模型（per-session vs singleton）、AEP control 消息分发链路、bridge `forwardEvents` 生命周期

---

## 1. Problem Statement

用户反馈：会话页面"停止"按钮无效，点击后无法打断 AI 继续回复。

**功能目标（用户明确）**：停止按钮的作用是**无条件停止当前 AI 正在执行的任务**。

当前实现下，该目标在四种 worker 上**均无法达成**，且失效方式不同：
- 进程型 worker（claudecode / acp）：能停，但走 SIGTERM 5 秒 grace，不是"立即"。
- 单例 worker（codexcli / opencodeserver）：**完全无效**——`Terminate` 不杀进程、不打断当前 turn，AI 任务在共享进程里继续执行。

---

## 2. Root Cause

### 2.1 调用链（现状）

```
[前端] handleCancel                                  hotplex-runtime-adapter.ts:1558
  ├─ client.sendControl("terminate")                 ← 发 control/terminate
  └─ setTimeout(600ms) → setIsRunning(false)          ← 盲目本地 reset，不等服务端确认
        ↓
[Gateway] handleControl                              commands.go:17
  └─ case ControlActionTerminate → TransitionWithReason(StateTerminated)   commands.go:40
        ↓ manager.go:690-692
        terminateWorkerGracefully → w.Terminate(ctx, GracefulShutdownTimeout=5s)
```

### 2.2 四种 worker 的 `Terminate()` 行为对照

| Worker | 进程模型 | `Terminate()` 实际行为 | 能停当前任务？ | 关键证据 |
|--------|---------|----------------------|--------------|---------|
| **claudecode** | per-session 子进程 | `cancel()` + SIGTERM → **5s grace** → SIGKILL | ✅ 但慢 ≤5s | `claudecode/worker.go:584-592`；`Kill()`(SIGKILL) 能力已存在但闲置 (`base/worker.go:109`) |
| **acp** | per-session 子进程 | `cancel` + conn.Close + **`client.Cancel(sessionID)`** + SIGTERM 兜底 | ✅ 最完整 | `acp/worker.go:643-702`，先发 cancel 请求再杀 |
| **codexcli** | **singleton** app-server | `shutdown()`=release 引用计数；**不杀进程、不打断 turn** | ❌ 完全无效 | `codexcli/worker.go:501-524`，注释明说 "shared AppServer singleton process is NOT killed here" |
| **opencodeserver** | **singleton** HTTP+SSE | `release()`=关 SSE + 引用计数；**不杀进程、不打断 turn** | ❌ 完全无效 | `opencodeserver/worker.go:373-385` "Does NOT kill"；协议层无 abort 端点 |

### 2.3 根本原因

Worker 接口缺少**"停止当前 turn"语义**。现有接口只有：
- `Terminate` / `Kill` —— 结束整个 worker 生命周期（对单例 worker 退化为"释放引用"，因为共享进程不能杀）。
- `ResetContext` —— 清空上下文（重于"停止当前任务"）。

停止按钮被强行绑在 `terminate`（杀 session）上，导致：
- 对进程型：走 SIGTERM 5s grace，延迟。
- 对单例型：`Terminate` 设计上不杀进程，任务继续，完全无效。

**已存在但未被接通的能力**：
- codexcli `manager.InterruptTurn(threadID, turnID)`（`codexcli/manager.go:1109`）—— 向单例发 `turn/interrupt` 通知，精确打断指定 turn。**当前仅挂在 `Clear()`（清空上下文，`worker.go:686`），停止路径未调用。**
- acp `client.Cancel(sessionID)`（`acp/worker.go:690`）—— 已在 `Terminate` 里 best-effort 调用，但绑在"杀进程"路径上。

### 2.4 加剧症状的两个前端缺陷

1. **600ms 盲目 reset**（`hotplex-runtime-adapter.ts:1566-1570`）：`setTimeout` 无条件 `setIsRunning(false)`，UI 立即显示"已停止"，但后端 worker 仍活，streaming delta 持续到达并渲染。用户看到"停止按钮已恢复，AI 却还在打字"。
2. **terminate 失败无感知**（`commands.go:33-44`）：ownership/transition 失败只 `sendErrorf`，前端 `handleCancel` fire-and-forget 不处理响应。若失败，worker 完全未受打扰。

---

## 3. Goals & Non-Goals

### Goals
- G1 停止按钮**无条件、立即**停止当前 AI turn，对四种 worker 一致有效。
- G2 停止后 **session 保持活跃**，用户可立即继续对话（不进 `StateTerminated`）。
- G3 统一 Worker 接口抽象，消除"停止当前任务"在四个 worker 上的碎片化实现。
- G4 前端 UI 与后端状态同步（服务端确认驱动 reset）。

### Non-Goals
- ❌ 不改变 `terminate`（关闭会话）的现有语义——保留给未来可能的"关闭会话"UI 入口。
- ❌ 不实现"打断后保留 partial 输出"——被打断的 turn 视为已停止，不保留 streaming 残片。
- ❌ 不在本次引入消息平台（Slack/飞书）的停止入口——仅 WebChat（后续可复用）。

---

## 4. Solution Overview

### 4.1 三条核心决策

1. **语义分离**：停止按钮 = 停止当前 turn + 保留 session；`terminate` 保留给"关闭会话"。
2. **Worker 接口新增 `StopCurrentTurn(ctx) error`**，各 worker 用最有效的手段实现。
3. **前端服务端确认驱动**：移除 600ms 盲目 reset，由 `done`(reason=stopped) 事件触发。

### 4.2 目标架构

```
[前端] handleCancel
  └─ client.sendControl("stop")              ← 新增，替代 terminate
  └─ 等 onDone(reason="stopped_by_user") → setIsRunning(false)
        ↓
[Gateway] handleControl
  └─ case ControlActionStop:                 ← 新增
       └─ w := sm.GetWorker(sid)
       └─ w.StopCurrentTurn(ctx)             ← 新接口
       └─ hub.SendToSession(done, reason="stopped_by_user")
       （不调 TransitionWithReason，session 状态不变）
        ↓
[Worker] StopCurrentTurn(ctx)
  ├─ claudecode:      Kill() → DetachWorker → session 迁 Idle
  ├─ acp:             client.Cancel(sessionID)
  ├─ codexcli:        manager.InterruptTurn(tid, turnID)
  └─ opencodeserver:  abort 端点（调研）或切断 SSE（妥协）
```

---

## 5. Detailed Design

### 5.1 Worker 接口扩展

`internal/worker/worker.go`（Worker 接口）新增：

```go
// StopCurrentTurn 停止 worker 当前正在执行的 turn，session 保持活跃。
// 区别于 Terminate（结束整个 worker 生命周期）与 ResetContext（清空上下文）。
// 实现必须做到：当前 turn 的后续 streaming 不再产生；session 可立即接受新输入。
StopCurrentTurn(ctx context.Context) error
```

契约约束：
- **幂等**：无在跑 turn 时调用返回 nil（不报错）。
- **不阻塞**：超时上限 2s（进程型 SIGKILL 是 ms 级；单例型 interrupt 是一次 RPC）。
- **不迁移 session 状态**（除非实现内部需要，见 5.2 claudecode）。

### 5.2 各 worker 实现

| Worker | 实现 | 立即性 | session / worker 影响 | 实现复用 |
|--------|------|--------|---------------------|---------|
| **claudecode** | `Kill()`(SIGKILL) → bridge `DetachWorker` → session Running→Idle | ms 级 | 进程死；session 保留；下次输入 `--session-id` resume（历史在 `.jsonl`） | `base/worker.go:109 Kill()` 已存在 |
| **acp** | `client.Cancel(sessionID)` | 立即 | worker + session 都保留，agent 自主中断 | `acp/worker.go:690` 逻辑抽出 |
| **codexcli** | `manager.InterruptTurn(tid, turnID)` | 立即 | 精确打断 thread，单例进程不动 | `codexcli/manager.go:1109` 已存在，从 `Clear()` 共享 |
| **opencodeserver** | 调研 opencode server abort 端点；无则切断 SSE + 标注限制 | 依协议 | server 侧任务可能继续（妥协） | 新增 |

**claudecode 选择 SIGKILL 而非 stdin interrupt 的理由**：目标是无条件立即停止。`ControlInterrupt` 目前只验证了 CLI→hotplex 方向（`claudecode/parser.go:409`，CLI 发来"我被打断"），hotplex→CLI 的入站 interrupt 是否被 Claude Code 支持未经验证，有失效风险。SIGKILL 确定、即时、不可协商。stdin interrupt 作为后续优化项（验证 CLI 支持后再引入，可免进程重启）。

**claudecode Kill 后的 bridge 状态协调**（P1 核心验证点）：
1. `Kill()` 杀进程组（`proc/signal_unix.go:30 ForceKill` = `kill(-pgid, SIGKILL)`）。
2. `session.DetachWorker(sid)` 解绑死进程引用（`manager.go:889`）。
3. session 从 `Running` 迁回 `Idle`（不进 `Terminated`）——worker 死了但 session 活着。
4. bridge `handleWorkerExit` 检测到 worker 退出，因 session 是 `Idle`（非 crash），**不触发 crash-recovery 重启**。
5. 用户下次发消息，bridge 检测无 worker attached → resume（`CanResumeTerminated=true`，启新进程 `--session-id` 续会话）。

### 5.3 Gateway 改造

`pkg/events/events.go` 新增 ControlAction：

```go
ControlActionStop ControlAction = "stop" // 停止当前 turn，保留 session
```

`internal/gateway/commands.go` `handleControl` 新增 case：

```go
case events.ControlActionStop:
    if err := h.sm.ValidateOwnership(ctx, env.SessionID, env.OwnerID, ""); err != nil {
        // ownership 失败处理（同 terminate）
    }
    w := h.sm.GetWorker(env.SessionID)
    if w == nil {
        return h.sendErrorf(ctx, env, events.ErrCodeInternalError, "stop: no active worker")
    }
    if err := w.StopCurrentTurn(ctx); err != nil {
        h.log.Warn("gateway: stop current turn failed", "session_id", env.SessionID, "err", err)
        return h.sendErrorf(ctx, env, events.ErrCodeInternalError, "stop failed: %v", err)
    }
    // 发 done 确认给前端，驱动 UI reset
    doneEnv := events.NewEnvelope(aep.NewID(), env.SessionID, h.hub.NextSeq(env.SessionID),
        events.Done, events.DoneData{Reason: "stopped_by_user"})
    return h.hub.SendToSession(ctx, doneEnv)
```

**不调 `TransitionWithReason`**，session 状态保持 `Running`/`Idle`。

### 5.4 前端改造

`webchat/lib/ai-sdk-transport/client/envelope.ts`：`createControlEnvelope` 已支持任意 action，无需改。

`webchat/lib/ai-sdk-transport/client/browser-client.ts:625` `sendControl` 类型签名扩展：

```ts
sendControl(action: 'terminate' | 'delete' | 'stop'): void
```

`webchat/lib/adapters/hotplex-runtime-adapter.ts:1558` `handleCancel` 重写：

```ts
const handleCancel = useCallback(async () => {
    if (stoppingRef.current) return;
    stoppingRef.current = true;
    setIsStopping(true);
    const client = clientRef.current;
    if (client?.connected) {
        client.sendControl("stop");   // terminate → stop
    }
    // 删除 setTimeout(600ms) 盲目 reset
    // reset 改由 onDone handler 驱动（reason === "stopped_by_user"）
}, []);
```

`onDone` handler（已存在，需补 reason 分支）：收到 `reason="stopped_by_user"` → `setIsRunning(false)` + `setIsStopping(false)` + `stoppingRef.current=false`。

**兜底**：若 2s 内未收到 `done`（网络异常），前端强制 reset（避免永久 stopping 态）。

---

## 6. Phased Delivery

| 阶段 | 范围 | 覆盖率 | 风险 | 依赖 |
|------|------|--------|------|------|
| **P1** | `ControlActionStop` + claudecode SIGKILL + acp `client.Cancel` + 前端 stop/done 确认 | ~90% 用户（claudecode 是默认 worker） | 低，全部基于已验证能力 | 验证 bridge 对 Kill 后状态协调（5.2） |
| **P2** | Worker.`StopCurrentTurn` 接口正式抽象 + codexcli `InterruptTurn` 接通 | codexcli 用户 | 低，能力已存在 | 无 |
| **P3** | opencodeserver（调研结果决定） | 补齐 | 中，取决于上游协议 | opencode server abort 端点调研 |

P1 可独立合并见效；P2/P3 后续推进。

---

## 7. Acceptance Criteria

### 功能验收（四种 worker 各一组）

- **AC1 (claudecode)**：AI streaming 中点停止 → ≤200ms 内停止生成（进程 SIGKILL）→ session 仍活跃 → 用户立即发新消息可正常 resume 续会话。
- **AC2 (acp)**：AI streaming 中点停止 → agent 收到 `session/cancel` → 立即停止 → worker 进程仍活 → 立即发新消息无需重启 worker。
- **AC3 (codexcli)**：AI turn 中点停止 → `turn/interrupt` 发往单例 → 当前 thread turn 停止 → **其他 session 的 turn 不受影响**。
- **AC4 (opencodeserver)**：依 P3 调研结果定义（若协议支持 abort → 同 AC2；若妥协 → 前端停止渲染，标注 server 侧可能继续）。

### 通用验收

- **AC5**：停止后 session 状态**不**是 `Terminated`（保持 `Running`/`Idle`），历史完整。
- **AC6**：前端 UI reset 由服务端 `done` 事件驱动，无 600ms 盲目 reset；停止按钮无"假恢复"。
- **AC7**：`stop` 幂等——无在跑 turn 时调用不报错、不影响 session。
- **AC8**：停止操作被 admin audit 记录（actor=uid，action=`session.stop`）。

### 负向验收

- **AC9 (回归保护)**：现有 `terminate`（关闭会话）、`reset`（清上下文）、`gc` 行为不变。

---

## 8. Risks & Open Questions

### R1 [高] opencodeserver 无 abort 能力
opencode server 协议是否暴露 abort/cancel 端点未知。若无，P3 只能交付"切断 SSE streaming"的妥协——server 侧任务继续、浪费 token，不满足"无条件停止"。
**缓解**：P1 先交付不依赖 opencodeserver；P3 前先完成协议调研；若上游不支持，向 opencode 提 feature request，并在文档明确标注限制。

### R2 [中] claudecode Kill 后 bridge 状态协调
`Kill` 后 bridge `handleWorkerExit` 必须把 session 正确迁回 `Idle` 且**不**触发 crash-recovery。若误判为 crash，会自动重启 worker（违背"停止"语义）。
**缓解**：P1 实施前先写一个集成测试验证 Kill→Detach→Idle 链路；必要时给 bridge 增加一个"用户主动停止"标记区分于 crash。

### R3 [低] partial turn 的前端展示
SIGKILL 留下未完成的 assistant message。前端需把它标记为 `status: "stopped"`（而非 `complete`），避免误导用户。
**缓解**：`onDone(reason="stopped_by_user")` 时把当前 streaming message 标记为 stopped。

### R4 [低] 单例 worker stop 后的配额/订阅一致性
codexcli `InterruptTurn` 不杀进程、不 release 引用——需确认 stop 后订阅、配额、turnID 状态一致（下次 turn 正常）。
**缓解**：AC3 集成测试覆盖"stop → 立即新输入 → 正常响应"。

### 开放问题
- Q1：Claude Code CLI 是否支持入站 interrupt control request？若支持，claudecode 可免 Kill（保留进程，体验更佳）——作为 P1 后优化项验证。
- Q2：消息平台（Slack/飞书）是否需要停止入口？本期 Non-Goal，但 `ControlActionStop` 已为其预留。

---

## 9. Test Strategy

- **单元测试**：每个 worker 的 `StopCurrentTurn`（mock 进程/单例），覆盖：正常停止、无在跑 turn（幂等）、超时。
- **集成测试**：
  - claudecode：真实 spawn → streaming 中 StopCurrentTurn → 断言进程 ≤200ms 死、session=Idle、可 resume。
  - codexcli：双 session 共享单例 → session A stop → 断言 session B turn 不受影响。
- **回归测试**：terminate / reset / gc 行为不变（AC9）。
- **前端 E2E**：停止按钮 → 断言无 600ms 假恢复、done 事件驱动 reset、停止后可立即发新消息。
- **负向测试**：ownership 失败、无 worker、worker 已死等边界。

---

## 10. References

- 根因调查：本 worktree 会话 `debug-stop-button`（四种 worker Terminate 实现全链路追踪）
- 相关代码：
  - 停止链路：`internal/gateway/commands.go:17-78`、`internal/session/manager.go:600-694`
  - Worker Terminate 实现：`claudecode/worker.go:584`、`acp/worker.go:643`、`codexcli/worker.go:501`、`opencodeserver/worker.go:375`
  - 已存在的 interrupt 能力：`codexcli/manager.go:1109 InterruptTurn`、`acp/worker.go:690 client.Cancel`
  - 前端：`hotplex-runtime-adapter.ts:1558 handleCancel`、`browser-client.ts:625 sendControl`
- 相关 spec：`docs/specs/CodexCLI-Wait-Block-Fix-Spec.md`（singleton worker 生命周期参考）
