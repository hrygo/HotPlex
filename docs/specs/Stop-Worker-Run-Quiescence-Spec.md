---
type: spec
status: approved
date: 2026-08-25
issue: 971
---

# Spec: WebChat 停止当前 Worker Run 并等待事件静默

**Issue**: [#971](https://github.com/hrygo/hotplex/issues/971)
**Severity**: P2（停止反馈失真，旧任务输出可污染下一轮会话）
**Scope**: `internal/gateway/{commands,handler,bridge,bridge_worker,bridge_forward,dispatch_acceptance}.go`、`internal/worker/{worker,native_commands}.go`、ACP/OCS/Codex 适配器、停止契约测试与四类 Worker 生命周期回归测试
**Related**: `docs/specs/Stop-Current-Turn-Spec.md`、Issue #880

本 Spec 是 Issue #880 和 `Stop-Current-Turn-Spec.md` 的可靠性补强。旧 Spec 已建立 `control.stop`、`StopCurrentTurn` 和 `done(reason="stopped_by_user")` 的基础语义；本 Spec 取代其中“`StopCurrentTurn` 返回后立即发送 done”的停止确认时序。其余 `control.stop` wire contract 和前端确认协议保持不变。

---

## 1. 问题陈述

### 1.1 用户症状

WebChat 中点击停止后，当前界面会退出思考状态，但下一次发送消息时仍可能继续思考或回应刚刚停止的问题。用户看到的“已停止”不是后端执行链的真实终态。

### 1.2 当前链路

`internal/gateway/commands.go` 当前执行：

```text
control.stop
→ stopFence.Claim
→ cancelRetryIfNeeded
→ Worker.StopCurrentTurn
→ finishRuntimeOnStop
→ 立即发送 done(reason="stopped_by_user")
```

`done` 只证明 Worker 的取消 API 已返回，不证明以下条件已经成立：

- Worker 进程或当前 session wrapper 已释放；
- 冻结在旧 run 上的 `SessionConn` 已关闭；
- 旧 `forwardEvents` goroutine 已退出；
- 已缓冲或并发到达的旧 run 事件已被隔离；
- 下一次输入不会再次绑定到旧 run。

### 1.3 根因

1. `processForwardedEvent` 只在 `done/error` 上检查 `Worker.IsStopped()`。旧 run 晚到的 `message.delta`、`message`、`reasoning`、tool、permission 和 `state` 事件仍可转发并写入 event store。
2. synthetic `done` 由 Handler 发送，不参与旧 forwarder 的 `terminalSent` 状态机，因此不能关闭旧 forwarder 的非终态事件入口。
3. 下一次 primary input 会调用 `stopFence.BeginTurn` 和 Worker `BeginTurn`，清除上一轮的 stopped 标记；旧 run 若尚未退出，晚到事件可能重新通过现有检查。
4. stop 与 input accept/dispatch 没有共同的 session 级临界区。并发输入可能在停止清理过程中取得旧 binding，或让 stop 读取到尚未投递的新 execution。
5. 关闭 channel 只会阻止新增事件。已经进入缓冲区或正在 `processForwardedEvent` 中执行的事件，需要 run 级屏障才能形成确定的静默边界。

---

## 2. 目标与非目标

### 2.1 目标

- **G1 — 真实停止确认**：只有旧 Worker run、旧连接和旧 forwarder 完成静默后，才发送 `stopped_by_user`。
- **G2 — 全事件隔离**：停止成功后，旧 run 的任何事件都不能到达 Hub、event store、runtime repair 或 retry 链路。
- **G3 — 输入隔离**：stop 与 input accept/dispatch 按 session 串行；新输入只能投递给新的 Worker run。
- **G4 — 会话连续性**：停止后逻辑 Session 保留并回到 `Idle`；下一次输入创建新的本地 Worker run，并恢复原 provider 会话历史。
- **G5 — 单例安全**：Codex CLI 和 OpenCode Server 只释放当前 session wrapper、订阅和连接，不终止共享单例服务，不影响其他 session。
- **G6 — 向后兼容**：不修改 AEP Kind/Data、JSON tag、WebChat control/done 数据形状或 Worker 公共接口。

### 2.2 非目标

- 不删除或回滚停止屏障建立前已经展示、转发或持久化的 partial output。
- 不清空会话历史，不调用 `ResetContext`，不创建 provider-fresh session。
- 不改变 `terminate`、`delete`、`reset`、GC 和 crash recovery 的既有语义。
- 不新增数据库字段、migration、公共指标或前端状态协议。
- 不为消息平台新增停止入口；已存在的 AEP `control.stop` 可被后续平台复用。

---

## 3. 核心不变量

### I1：Done 表示静默完成

`done(reason="stopped_by_user")` 的发送必须晚于旧 forwarder 的退出屏障。客户端收到该 terminal 后，旧 run 不可能再产生 seq、Hub 消息或 event-store 记录。

### I2：每轮只有一个停止终态

同一 `(session_id, worker_run_id, execution_id)` 最多执行一次有效停止、一次 runtime finish 和一次 synthetic done。重复 stop 静默返回，不调用第二次 Worker stop，也不发送第二个 terminal。

### I3：停止失败不伪装成功

若 `StopCurrentTurn` 返回失败，Gateway 必须恢复事件通行、回滚 stop fence、保留当前 binding，不发送 synthetic done。

### I4：停止成功后旧事件永久隔离

一旦 `StopCurrentTurn` 成功，旧 run 的 `stopping` 状态不可回退。后续 Worker teardown 即使报错或超时，该 run 的事件也不能重新进入用户或持久化链路。

### I5：新输入不复用旧本地 Run

停止成功后旧 Worker 必须 CAS detach，旧 `workerRunBinding` 必须清除。下一次输入使用 `ResumeSession` 创建新的 Worker 对象和 run ID；可恢复相同 provider session，但不能复用旧本地 wrapper、Conn 或 forwarder。

### I6：共享 Worker 隔离

对 Codex CLI / OpenCode Server 调用 `Terminate` 或 `Kill` 时，只能执行当前 wrapper 的 `release`、unsubscribe、SSE/Conn close 和 singleton reference release。不得终止共享 app-server / serve 进程。

---

## 4. 方案概览

```text
WebChat control.stop
        │
        ▼
session dispatch gate（阻止同 session 新 input accept/dispatch）
        │
        ▼
stopFence.Claim(session, run, execution)
        │
        ▼
run event write barrier
  ├─ Worker.StopCurrentTurn 失败 → 解锁、Rollback、返回 error
  └─ 成功 → lifecycle.stopping=true
        │
        ▼
Worker.Terminate
  └─ 失败 → Worker.Kill fallback
        │
        ▼
关闭 frozen SessionConn
        │
        ▼
等待 lifecycle.done（旧 forwarder 已退出）
        │
        ▼
确认 DetachWorkerIf + clearWorkerRun + Session Idle 已完成
        │
        ▼
finishRuntimeOnStop
        │
        ▼
done(reason="stopped_by_user")
        │
        ▼
释放 session dispatch gate
```

本方案销毁的是“当前 session 的 Worker run”，不是无条件杀死底层共享服务。精确取消负责停止 provider 当前任务；Worker teardown 负责释放本地生命周期；forwarder barrier 负责证明事件链已经静默。三者缺一不可。

---

## 5. 详细设计

### 5.1 Session 级 Dispatch Gate

在 Handler 内新增固定分片的 session gate，避免无界 map：

```go
type sessionDispatchGate struct {
	stripes [64]sync.Mutex
}

func (g *sessionDispatchGate) Lock(sessionID string) func()
```

锁定范围：

- `deliverToWorkerWithBusyHandling`：从读取 Session、`acceptInputExecutionWithRetry` 开始，覆盖 binding 选择、`MarkRunning`、`stopFence.BeginTurn`，直到 Worker 明确确认请求已被 provider 接受。
- `control.stop`：从解析当前 binding/execution 开始，覆盖 stop、teardown、forwarder 等待、runtime finish 和 synthetic done。

不持锁等待整个模型 turn。对于返回即代表投递完成的 Worker，成功的 `Worker.Input` 返回是 acceptance point；对于 ACP 等 `Input` 会阻塞至整轮结束的适配器，新增可选 `InputDispatchAcknowledger`，在请求不可再被 stop 超越时回调确认，Gateway 随即释放 gate、继续在锁外等待 `Input` 结果。OpenCode 普通输入改用 `POST /session/{id}/prompt_async`，以 204/2xx acknowledgement 作为 acceptance point，后续输出仍由 SSE 提供。不同 stripe 上的 session 不互相影响；哈希碰撞只会造成短暂串行，不改变语义。

该能力是可选接口，不修改 `worker.Worker` 主接口：

```go
type InputDispatchAcknowledger interface {
    InputWithDispatchAccepted(ctx context.Context, content string, metadata map[string]any, accepted func()) error
}
```

ACP 在完整 `session/prompt` JSON-RPC frame 写入并释放 stdin write lock 后确认，确保随后发出的 `session/cancel` 不会越过 prompt。OCS 原生 `/command` 没有异步端点，因此在该请求仍等待响应时，以同 session SSE 的 `session.status(busy)` / `state(running)` 作为 provider acceptance signal；session gate 保证前一轮 terminal 已处理且同一 Worker 只有一个待确认 command，callback 又在发出 HTTP 请求前即时注册。LLM auto-retry 复用相同协议：run event read admission 只持有到 acceptance point，避免 retry 已投递后仍以整轮阻塞 stop 的 event write barrier。

`reset`、`terminate` 和 `delete` 本期保持现状。后续若需要统一所有 lifecycle command，可复用同一 gate，但不得在本 Issue 顺手扩大范围。

### 5.2 Worker Run Lifecycle

扩展私有 `workerRunBinding`，不改变 `worker.Worker` 公共接口：

```go
type workerRunLifecycle struct {
	eventMu  sync.RWMutex
	stopping atomic.Bool
	done     chan struct{}
	conn     worker.SessionConn
}

type workerRunBinding struct {
	worker    worker.Worker
	id        string
	lifecycle *workerRunLifecycle
}
```

约束：

- lifecycle 在 `createAndLaunchWorker` 成功启动 Worker、冻结 Conn 时创建；binding 与 forwarder 共享同一个指针。
- `conn` 必须是 forwarder 启动时冻结的 Conn，停止路径不得重新读取可能已被 reset 替换的 `Worker.Conn()`。
- forwarder goroutine 最外层注册 `defer close(lifecycle.done)`，并通过 defer 注册顺序保证 run binding 清理先执行；close 发生在 `handleWorkerExit` 返回和 `clearWorkerRun` 完成之后。
- `done` 只关闭一次；生命周期对象不复用于 replacement run。

### 5.3 事件隔离屏障

`processForwardedEvent` 在任何 LastIO、seq、runtime、retry、Hub 或 event-store 副作用之前执行：

```go
lifecycle.eventMu.RLock()
defer lifecycle.eventMu.RUnlock()
if lifecycle.stopping.Load() {
	return
}
```

stop 路径执行：

```go
lifecycle.eventMu.Lock()
err := w.StopCurrentTurn(ctx)
if err != nil {
	lifecycle.eventMu.Unlock()
	return err
}
lifecycle.stopping.Store(true)
lifecycle.eventMu.Unlock()
```

该顺序提供两个性质：

- stop 失败时，没有事件被永久丢弃；等待 event write lock 的事件会在解锁后继续处理。
- stop 成功时，所有已经进入 `processForwardedEvent` 的事件先完成；`stopping=true` 建立后，新旧缓冲事件全部被拒绝，因此屏障后不存在并发转发。

下列不经过 `processForwardedEvent` 的出口也必须检查 `lifecycle.stopping`：

- forwarder panic synthetic error；
- turn-timeout timer synthetic error 及其 Worker terminate；
- recv channel 关闭后的 pending error flush；
- LLM retry 调度和 replay；
- `handleWorkerExit` 的 crash fallback、crash error 和 synthetic crash turn。

现有 `Worker.IsStopped()` 仍用于区分 user stop 与 crash，但不再承担全部事件隔离职责。

### 5.4 Stop 编排接口

Bridge 新增内部接口：

```go
func (b *Bridge) StopAndDisposeCurrentRun(
	ctx context.Context,
	sessionID string,
	expectedRunID string,
) error
```

执行规则：

1. 读取完整 private binding，并校验 session 当前 Worker 和 `expectedRunID` 同时匹配；不匹配返回 run-changed error，禁止操作 replacement Worker。
2. 按 5.3 建立事件屏障并调用 `StopCurrentTurn`。
3. stop 成功后调用 `Terminate`。teardown 使用总计 8 秒的 server-side deadline；底层 `proc.Manager.Terminate` 在 context 到期时强制 Kill。
4. `Terminate` 返回错误时调用 `Kill`。对 per-session Worker 这是进程级强杀；对 Codex/OCS 是幂等 wrapper release。
5. best-effort 关闭 lifecycle 保存的 frozen Conn，确保旧 `Recv()` range 可以结束。
6. 等待 `lifecycle.done`。8 秒 teardown 预算耗尽后，Conn close 仍允许最多 1 秒本地 forwarder 收敛；整个服务端停止上限为 9 秒，低于 WebChat 当前 10 秒 stop settle timeout。
7. `lifecycle.done` 返回成功，证明 `handleWorkerExit` 已完成 CAS detach 和 `Idle` transition；Bridge 再验证旧 binding 不再是 current binding。

正常情况下 `handleWorkerExit` 继续作为 Worker detach、配额释放和 `Idle` transition 的唯一主路径，避免 Handler 与 forwarder 双重释放。超时异常路径执行 CAS detach 和 `clearWorkerRun` 作为 fail-closed 收敛；旧 lifecycle 保持 `stopping=true`，即使孤立 goroutine 稍后退出，也不能污染 replacement run。

锁顺序固定为 session dispatch gate → run event barrier。event write lock 只覆盖 `StopCurrentTurn` 和 `stopping` 状态提交；调用 `Terminate`、`Kill`、Conn close、SessionManager 或等待 `done` 前必须释放 event lock，避免把 Worker/Session 生命周期操作纳入转发锁。

### 5.5 Handler 停止流程

`commands.go` 的 `control.stop` 保留 ownership、stop fence、retry cancel 和 execution runtime 语义，但调整顺序：

1. 获取 session dispatch gate。
2. 从原子 Bridge binding 获取 Worker 和 run ID；若 binding 已被首次 stop 清除，则使用 latest execution 的 `WorkerRunID` 判断是否为同一轮重复 stop。
3. 获取 latest execution ID 并执行 `stopFence.Claim`。重复 claim 静默返回。
4. `cancelRetryIfNeeded`。
5. 调用 `StopAndDisposeCurrentRun`。
6. 仅在 run 静默成功后调用 `finishRuntimeOnStop`。
7. 最后发送一个 `done(reason="stopped_by_user")`；`Seq=0` 继续由 Hub 在 publish-order lock 下分配。

错误语义：

- `StopCurrentTurn` 失败：`stopFence.Rollback`，返回 `INTERNAL_ERROR`，不 finish runtime、不发 done，当前 run 可继续并可重试 stop。
- stop 成功但 teardown/forwarder 未在预算内静默：不回滚 stop fence；旧 run 保持隔离并被 CAS detach，execution 收敛为 stopped/failed；向客户端返回 `INTERNAL_ERROR`，不发送代表完整静默的 done。
- binding 在执行前变化：返回 `SESSION_BUSY` 或内部 run-changed error，不触碰新 Worker。

不得将原始 Worker 错误写入 execution 持久化数据；日志只记录包装后的 phase、session ID、run ID、execution ID 和错误类别。

### 5.6 下一轮恢复

停止成功后 Session 为 `Idle` 且无 Worker binding。现有 input 路径会调用 `ResumeSession`：

- WorkerFactory 创建新的 Worker 对象；
- attach 分配新的 run ID 和 lifecycle；
- provider resume identity 保留，因此对话历史继续；
- 旧 run 的 Worker、Conn、forwarder 和停止标记不被复用。

本流程不调用 `StartFreshWorker`。后者会清除 `WorkerSessionID` 和 provider resume identity，适用于 execution ambiguity fence，不适用于用户主动停止后保留上下文的场景。

### 5.7 四类 Worker 语义

| Worker | 精确取消 | Teardown | 共享服务影响 | 下一轮 |
|---|---|---|---|---|
| Claude Code | `StopCurrentTurn` 取消 goroutine并强杀当前进程 | `Terminate` 幂等清理；`Kill` 兜底 | 无共享进程 | 新进程 resume 原 session |
| ACP | `session/cancel`；不支持时进程级 kill | 关闭 conn/client/pipe，SIGTERM→SIGKILL | 无共享进程 | 新进程 resume 原 session |
| Codex CLI | `turn/interrupt(threadID, turnID)` | unsubscribe thread、关闭 wrapper conn、release manager ref | 不杀 app-server，不影响其他 thread | 新 wrapper resume 原 thread |
| OpenCode Server | session abort API | 取消 SSE、unsubscribe session、关闭 wrapper conn、release singleton ref | 不杀 serve，不影响其他 session | 新 wrapper resume 原 session |

### 5.8 前端与协议

前端当前已通过 `stopCurrentTurn()` 等待服务端 `done`，并有 10 秒 timeout；本期无需修改 WebChat 源码。停止耗时从“取消 API 返回”改为“旧 run 完全静默”，UI 保持 stopping 状态是预期行为。

以下 wire contract 不变：

```json
{"event":{"type":"control","data":{"action":"stop"}}}
{"event":{"type":"done","data":{"reason":"stopped_by_user"}}}
```

因此无需更新 AEP Kind/Data、SDK 或双向协议测试矩阵。

---

## 6. 并发与顺序证明

### 6.1 Stop 与旧事件

`processForwardedEvent` 持 event read lock；stop 持 event write lock。stop 只有在所有已进入转发函数的事件完成后才能将 `stopping=true`，此后所有晚到事件在任何副作用前返回。synthetic done 又晚于 `forwarder.done`，因此 done 后不存在旧事件。

### 6.2 Stop 与新 Input

stop 和 input accept/dispatch 使用同一个 session dispatch gate：

- input 先取得 gate：输入完成 execution 绑定并到达 provider acceptance point 后释放 gate，stop 随后解析并停止该轮；adapter 可继续在 gate 外等待整轮 RPC 结果；
- stop 先取得 gate：旧 run 静默并 detach 后，input 才能 accept/resolve binding，因而只能 resume 新 run。

不存在“新 execution 已 accept，但 stop 仍按旧 binding finish 新 execution”的中间状态。

### 6.3 Stop 与 Worker Replacement

`expectedRunID`、binding Worker 指针和 `DetachWorkerIf` 共同形成 CAS fence。旧 stop/forwarder 无权清理或终止 replacement Worker；旧 lifecycle 只控制自身 frozen Conn 和事件流。

### 6.4 Double Stop

session gate 使两个 stop 串行，`stopFence` 保持每轮 single-effect。首次 stop 清除 binding 后，第二次 stop 使用 latest execution 保存的 run ID 命中原 claim并静默返回，保持 C04/C05 合同。

---

## 7. 可观测性

使用结构化 slog 记录以下 phase：

- `stop_requested`
- `provider_cancelled`
- `worker_terminating`
- `worker_kill_fallback`
- `forwarder_quiesced`
- `stop_completed`
- `stop_failed`

字段统一使用 snake_case：`session_id`、`worker_type`、`worker_run_id`、`execution_id`、`stop_phase`、`duration_ms`、`error_kind`。不得记录 prompt、metadata 值、凭据或原始 Worker 错误。

本期不新增公共 metrics；因此无需修改 `docs/reference/metrics.md`。后续如增加 counter/histogram，必须使用 `sync.Once` 注册并同步文档。

---

## 8. 测试策略

### 8.1 Gateway Contract

扩展 `internal/gateway/contracttest/worker_probe.go`，支持：

- `StopCurrentTurn` 成功后继续注入 `message.delta`、`reasoning`、tool、permission、`state`、`done` 和 `error`；
- 延迟关闭 frozen Conn；
- 控制 `Terminate`、`Kill` 和 Conn close 的成功/失败；
- 暴露 forwarder 已退出信号。

在 `internal/gateway/stop_contract_test.go` 增加：

- **C06 late-event quarantine**：屏障后的所有事件不可见、不可持久化，只出现一个 `stopped_by_user`。
- **C07 done-after-quiescence**：释放 forwarder exit gate 前不得出现 stopped done；释放后必须出现。
- **C08 stop-input race**：并发 stop 和 input，新 input 的 execution/run ID 只能指向 replacement Worker。
- **C09 teardown fallback**：Terminate 失败触发 Kill 和 frozen Conn close；成功静默后仍只发一个 done。
- **C10 quiescence timeout**：旧 run 被隔离并 CAS detach，但客户端收到 error 而非假 done。
- **C11 blocking-input stop**：Worker 已接受输入但 `Input` 仍等待整轮结果时，stop 必须进入 `StopCurrentTurn`，不得被 dispatch gate 阻塞；adapter 随后返回的 cancellation error 必须等待 stop 决策，成功 stop 不得把它误报为 input failure。
- **C12 stale-stop-claim**：旧 turn 的 retained claim 不得让新 execution 在 binding 丢失后静默返回成功；已配置 execution ledger 时仅精确 `(session, run, execution)` 匹配可判定重复 stop，ledger-disabled 模式才允许 session-only fallback。
- **C13 accepted-input stop failure**：已接受 RPC 的 adapter error 必须等待并发 stop 决策；stop 成功时吸收取消错误，stop 失败并 rollback 时保留真实 input error。

### 8.2 Stop 失败回归

保留并增强现有 stop-failure-retry：

- 取消失败期间到达的事件在 event lock 解开后正常转发；
- stop fence 被 rollback；
- 第二次 stop 可以成功；
- 第一次失败不产生 runtime stopped 或 synthetic done。

### 8.3 Worker Matrix

- Claude Code / ACP：验证 teardown 后子进程退出、Conn 关闭、Wait 返回，随后 resume 创建新 Worker。
- Codex CLI：双 session 共享 manager；停止 A 后 A wrapper unsubscribe，B 的 turn 和 manager 进程保持正常。
- OpenCode Server：双 session 共享 singleton；停止 A 后 A SSE/subscription 释放，B 不受影响。
- 四类 Worker 均验证 stop → resume 后 Worker 指针和 run ID 已变化，provider session identity 保留。

### 8.4 前端回归

不新增前端行为测试，只运行现有 browser client 测试和完整 WebChat unit suite，确认：

- double click 仍只发送一次 stop；
- 10 秒 timeout 仍可解除 stopping；
- `stopped_by_user` 仍触发当前轮次和 follow-up queue 收敛。

### 8.5 验证命令

```bash
go test ./internal/gateway -run 'Stop|C0[4-9]|C10' -count=1 -race -shuffle=on
go test ./internal/worker/claudecode ./internal/worker/acp ./internal/worker/codexcli ./internal/worker/opencodeserver -run 'Stop|Terminate|Kill|Release|Resume' -count=1 -race -shuffle=on
go test ./internal/gateway/... ./internal/worker/... -count=1 -race -shuffle=on
cd webchat && pnpm test
```

---

## 9. 验收标准

- **AC1**：stop 成功后，旧 run 晚到的全部事件类型均不可到达客户端或 event store。
- **AC2**：`stopped_by_user` 只在旧 forwarder 退出后发送，每轮最多一个 terminal。
- **AC3**：stop 与新 input 并发时，新 input 只投递至不同 Worker 指针和不同 run ID。
- **AC4**：stop 失败不发送 done、不 finish runtime，并允许同一轮重试 stop。
- **AC5**：Terminate 失败时执行 Kill 和 frozen Conn close；若仍无法静默，返回 error 而不是成功 done。
- **AC6**：停止成功后 Session 为 `Idle`、历史保留；下一条消息通过新 Worker run 正常响应，旧问题不再输出。
- **AC7**：Codex/OCS 停止 session A 不终止共享服务，也不影响 session B。
- **AC8**：现有 `terminate`、`reset`、`delete`、GC、crash fallback、execution owner lease 和输入幂等合同不变。
- **AC9**：AEP 和 WebChat wire contract 无变化，现有前端 stop suite 全部通过。
- **AC10**：目标 Gateway/Worker 测试在 `-race -count=1 -shuffle=on` 下通过，单模块不超过 5 秒；显式超时测试使用缩短的可注入 duration。
- **AC11**：ACP/OCS 普通输入、ACP/OCS 原生命令及 LLM retry 在 provider acceptance 后不再持有 stop 所需的 dispatch/event admission；Codex interrupt 与 unsubscribe 使用调用方 teardown context。
- **AC12**：detached-run 重复 stop 只在已知 run/execution 精确命中 retained claim 时幂等成功；旧 claim 不掩盖新 turn 的 binding 丢失。

---

## 10. 实施范围

| 文件 | 责任 |
|---|---|
| `internal/gateway/handler.go`、`dispatch_acceptance.go` | session dispatch gate；两阶段 input accept/dispatch 临界区 |
| `internal/gateway/commands.go` | stop 编排、重复 stop、错误与 done 时序 |
| `internal/gateway/bridge.go` | run lifecycle 类型、完整 binding 查询、stop/dispose API |
| `internal/gateway/bridge_worker.go` | lifecycle 创建、frozen Conn、forwarder done 屏障 |
| `internal/gateway/bridge_forward.go` | 全事件隔离和非主事件出口收敛 |
| `internal/gateway/stop_contract_test.go` | C06-C13 Gateway 契约 |
| `internal/gateway/contracttest/worker_probe.go` | 晚到事件、teardown 和 forwarder 可控探针 |
| `internal/worker/{worker,native_commands}.go` | 可选 dispatch-acceptance 能力；`Worker` 主接口保持不变 |
| ACP / OCS / Codex Worker 与测试 | ACP 精确写入确认、OCS async prompt、Codex deadline 传播与共享单例隔离回归 |

除上述文件及必要的同目录测试辅助代码外，不修改前端、AEP、SDK、数据库 migration、配置、版本或 changelog。

---

## 11. 替代方案与取舍

### A. 只在 forwarder 中丢弃 `IsStopped()` 事件

拒绝。下一轮 `BeginTurn` 会清除 stopped 标记；该标记属于 Worker 的 per-turn 状态，不是不可回退的 run 生命周期 fence，也无法证明 forwarder 已退出。

### B. 只调用 `Kill()`，不等待 forwarder

拒绝。进程终止和 wrapper release 不能撤销已经缓冲或正在转发的事件；synthetic done 之后仍可能出现旧 seq。

### C. 为所有 AEP content 事件增加 execution/run ID，让前端过滤

拒绝。本问题可在 Gateway 内部闭合。扩大 AEP wire contract 会要求 SDK、示例和协议测试同步变化，且不能阻止旧事件进入 event store、retry 和 runtime side effects。

### D. stop 后调用 `StartFreshWorker`

拒绝。它会清除 provider resume identity，适合 ambiguity fence，不适合用户主动停止；会不必要地丢失 provider 原生会话上下文。

### E. stop 后立即启动 replacement Worker

拒绝。停止动作无需承担 Worker startup 延迟；现有下一次输入 auto-resume 已能按需创建新 run。dispatch gate 和旧 run detach 足以隔离竞态。

### F. 直接杀 Codex/OCS 共享服务

拒绝。会中断同一 singleton 上的其他 session，违反多会话隔离。只能 interrupt/abort 当前 provider turn并释放当前 wrapper。

---

## 12. 回退策略

该变更不涉及数据迁移或 wire contract。若上线后出现新生命周期回归，可整体回退实现提交，恢复 #880 的 soft-stop 行为；本 Spec 和 Issue 保留用于记录未解决风险。不得只回退事件屏障而保留“等待静默后 done”的部分实现，否则可能制造永久 stopping。

---

## 13. 参考代码

- `internal/gateway/commands.go`：当前 stop handler、stop fence 和 synthetic done。
- `internal/gateway/handler.go`：input accept、binding 选择、`BeginTurn` 和 auto-resume。
- `internal/gateway/bridge_worker.go`：Worker attach、run binding 和 forwarder goroutine。
- `internal/gateway/bridge_forward.go`：事件转发、terminal fence、retry、exit cleanup。
- `internal/session/manager.go`：`DetachWorkerIf` CAS 与配额释放。
- `internal/worker/worker.go`：现有 `StopCurrentTurn`、`Terminate`、`Kill`、`Wait`、`Conn` 接口。
- `internal/worker/{claudecode,acp,codexcli,opencodeserver}/worker.go`：四类 Worker 的取消和 teardown 语义。
- `webchat/lib/ai-sdk-transport/client/browser-client.ts`：10 秒 stop settle waiter。
- `docs/specs/Stop-Current-Turn-Spec.md`：Issue #880 的初始停止语义。
