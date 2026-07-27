# IM 渠道 SESSION_BUSY 的 mid-turn 透传 + 兜底设计

**日期：** 2026-07-25
**状态：** 草案，待 review
**范围：** `internal/worker`（`MidTurnInjector` 接口 + CC/codex adapter）、`internal/gateway`（busy 决策、`PendingBuffer`、done 重投）、`internal/messaging`（三渠道文案回调）
**关联：**
- 演进自 [`2026-07-25-session-busy-pending-replay-design.md`](./2026-07-25-session-busy-pending-replay-design.md)（其兜底部分保留为本设计的 fallback）
- 不冲突 [`2026-07-16-webchat-follow-up-queue-design.md`](./2026-07-16-webchat-follow-up-queue-design.md)（webchat 前端队列 vs IM gateway 层，范围不重叠）
- 遵守 [`2026-07-14-durable-ingress-reliability-closure-design.md`](./2026-07-14-durable-ingress-reliability-closure-design.md)（active gate 单活跃约束不动）

---

## 决策摘要

前置设计 `pending-replay` 的核心前提——"SESSION_BUSY 时 gateway 只能暂存 + 合并重投"——**被实测推翻**：hotplex 的 CC worker 跑的是 `claude --print --input-format stream-json`（headless），而该模式**原生支持 mid-turn 注入**——turn 进行中向 stdin 写第二条 user 消息，CC 会把它纳入当前 running turn 消化（随下一个 tool result 体现），而非排队成独立的下一个 turn。codex 则有协议级 `turn/steer` 原语。

因此采用**混合方案**，由 gateway 在 busy 时按 worker 能力分流：

1. **worker 支持 mid-turn（CC、codex）→ 透传**：把追问直接注入当前 running turn（写 worker stdin / 调 `turn/steer`），不创建新 execution、不占 active gate 槽、不暂存。worker 自决如何在当前 turn 内处理——符合"hotplex 不做 input 控制、由 worker 自身决定"的原则。
2. **worker 不支持（ocs 待定、acp 协议层不可能）→ 兜底**：gateway 暂存 + done 后合并重投（即 `pending-replay` 设计，保留为 fallback）。

**不修改 active gate 单活跃约束**（partial unique index 不动），**不新增 AEP wire 类型**。
补充消息复用现有 `input.ack`：首次注入或暂存返回终态 `delivered`，相同
`client_message_id` 重发返回 `duplicate: true`，不会再次产生副作用。

## 背景与已证实问题

### 黑洞效应（沿用 pending-replay 背景）

PR #890 引入 active gate 后，每 session 同时只允许一个 `pending/running` execution。用户在长任务执行中追问，新消息撞 `SESSION_BUSY` 被直接丢弃，各 IM 渠道用通用兜底文案通知用户，造成黑洞效应与误判故障（飞书 `handler.go:242`、Slack `conn_events.go:86`、yuanxin 仅 `Log.Error`）。

### pending-replay 设计的局限

`pending-replay` 假设"busy 时只能 gateway 暂存合并重投"，方案是 `SESSION_BUSY → 存 PendingBuffer → done 后合并为一条 input 重投`。它把追问**延后到下一个 turn** 处理，而非让 worker 在**当前 turn** 内消化。这在 worker 原生支持 mid-turn 注入时是次优的——多了一次 turn 往返、丢失了"并入当前推理"的自然语义。

### 方向转变的依据：CC mid-turn 能力实测

在 headless `--print --input-format stream-json --output-format stream-json --verbose` 模式下实测（CC 2.1.212）：

- **t=0** 发 prompt1："用 Bash 跑 `sleep 12`，完成后只回 SLEEP_DONE"
- CC 决定调 Bash sleep（`tool_use` 1 次），turn 进入工具执行窗口
- **t=7**（sleep 仍在跑）注入 prompt2："BONUS: 7+8=? 必须在回复里包含 BONUS=15"
- CC 在 sleep 完成后生成最终回复，**同时含 `SLEEP_DONE` 与 `BONUS=15`**
- **只有 1 个 `result` 事件**，`num_turns:2`，`stop_reason:end_turn`，`duration_ms:29938`

最硬的证据是 CC 自己的 thinking 原文：

> "Now a new message came in... **In Claude Code, these mid-turn messages are genuinely from the user** and represent legitimate instructions."

**结论**：CC 客户端在工具执行期间持续读 stdin，把中途注入的 user message 纳入**当前 agentic turn**（turn1 调 sleep → turn2 收 tool_result + BONUS → 同一个 result 输出两者），**而非排队成独立的下一个 result**。hotplex 现有架构（`--print --input-format stream-json` + 常驻 stdin）物理通道已具备，仅被 gateway 的 active gate 在 execution 层挡住。

**两个诚实的 caveat**：
1. 实测后端为 glm-5.2（代理），但"mid-turn 读 stdin"是 CC 客户端行为，与后端模型无关，结论对 CC 客户端成立。
2. 实测覆盖"工具执行期间"的 mid-turn（最常见追问时机）；纯文本 streaming 期间的注入未单独测，但 control 协议（`internal/worker/claudecode/control.go` 双向 control_request/response）证明 CC 在 turn 进行中持续读 stdin，机制应一致。

## 错误识别：不引入 typed error（与 pending-replay 的关键差异）

`pending-replay` 计划新增 `pkg/events.CodedError` 让各渠道 `errors.As` 识别 `SESSION_BUSY`。**本设计不做**：混合方案下，busy 时 gateway 内部完成透传或兜底，**不再向渠道返回 `SESSION_BUSY` error**（透传/兜底均视为"已处理"，不报错）。渠道改通过 bridge→adapter 回调（见下）得知"追问已被接收"，无需 catch error。故：

- `sendErrorf`（`internal/gateway/errors.go:14`）**不改**，继续用于真正的 internal error / 真正需要拒绝的场景。
- `pkg/events` 不新增 `CodedError`，无 AEP wire contract 变动。

> 透传失败回退兜底时，错误在 gateway 内部处理（log + 走兜底），也不冒泡到渠道。

## 架构：gateway 统一 busy 决策

busy 决策集中在 `internal/gateway/handler.go` 的 `deliverToWorker`（`:536` 当前 `ErrSessionBusy` 拒绝点）。改造后：

```
deliverToWorker → acceptInputExecutionWithRetry 返回 ErrSessionBusy
  └─ w := sm.GetWorker(env.SessionID)
     ├─ w 实现 MidTurnInjector（且 worker 未 stopped）
     │    → 透传：err := w.InjectMidTurn(ctx, content, md)
     │       ├─ err == nil：
     │       │    observability.MidTurnInjected().Add(ctx, 1)
     │       │    bridge.CaptureInboundEvent(sid, seq, Input, data)  // 最低可观测
     │       │    h.notifySupplement(sid, "injected")   // hub 发 message(metadata mode)→ conn 发"正在处理"文案
     │       │    return nil（透传成功，不报错）
     │       └─ err != nil（极小竞态：done 与 inject 同时，turn 已结束）：
     │            落入兜底分支（见下）
     └─ else（acp/ocs 不实现 MidTurnInjector，或透传失败回退）
          → 兜底：bridge.BufferPending(env.SessionID, env, content)
            observability.SupplementBuffered().Add(ctx, 1)
            h.notifySupplement(sid, "buffered")   // hub 发 message(metadata mode)→ conn 发"完成后处理"文案
            return nil（已暂存，不报错）
```

三处接入点均已确认：
- busy 分支：`handler.go:536`（现 `return sendErrorf(... SESSION_BUSY)`）
- 透传蓝本：`tryInteractionResponse`（`handler.go:405`）直接 `w.Input()` 绕 execution + `CaptureInboundEvent`（`:412`）
- done 重投：`bridge_forward.go:507`（`FinishRuntime` 成功后，active gate 已释放）

## Worker 能力声明（新增可选接口）

仿现有可选接口模式（`PermissionCeilingReporter` `worker.go:166`、`ControlRequester`），新增 duck-type 接口，实现即声明支持：

```go
// internal/worker/worker.go

// MidTurnInjector is implemented by workers that can accept a user message
// mid-turn — injecting it into the currently running turn rather than starting
// a new one. Gateway probes this via type assertion at the SESSION_BUSY branch;
// workers that don't implement it fall back to pending-buffer replay.
type MidTurnInjector interface {
    // InjectMidTurn delivers a user message into the active turn of the worker.
    // It must return an error if the turn is no longer active (caller falls back).
    InjectMidTurn(ctx context.Context, content string, metadata map[string]any) error
}
```

gateway 探测：

```go
inj, ok := w.(worker.MidTurnInjector)
if ok && !w.IsStopped() {
    // 透传
}
```

| Worker | 实现 | 说明 |
|---|---|---|
| **CC** | `InjectMidTurn` = 写 stdin 同一 stream-json user 消息（复用 `writeStreamInputLocked` `input.go:43` 的内核） | 实测证明 turn 进行中写 → CC 当 mid-turn 吸收。**无需新协议、无需新方法实质逻辑**，仅包一层以满足接口 |
| **codex** | `InjectMidTurn` = `manager.SteerTurn(threadID, turnID, content)` | `SteerTurn` 已实现（`codexcli/manager.go:1093`），`threadID`（`worker.go:104`）/`turnID`（`worker.go:105`）已跟踪。照抄 `StopCurrentTurn`（`worker.go:529`）模式 |
| **ocs / acp** | 不实现 | acp `call` 阻塞到 turn 结束（`acp/client.go:279`），协议层不可能 mid-turn → 兜底。ocs 行为由 `opencode serve` 决定，本轮按不支持处理（兜底），未来可加 |

### codex 过期 turnID 竞态处理

`w.turnID` 在 turn 结束（`turn/completed`/`turn/failed`）时**不会被清除**（mapper `mapNotifTurnCompleted`/`mapTurnFailed` 不碰 turnID）。若 `InjectMidTurn` 在 turn 已结束后调用，`SteerTurn` 传旧 `expectedTurnID` 会被上游拒绝（`manager.go:1091` 注释："the request fails if it does not match"）。

**处理（最小改动，不在 worker 内清 turnID）**：
- busy 时 session 必然 active（有 running execution 才会 busy），故 turn 活跃，`InjectMidTurn` 安全。
- 极小竞态窗口（done 与 inject 同时）：`InjectMidTurn` 返回 error → gateway **自动回退兜底**（见架构流程图的 `err != nil` 分支）。
- 无需新增 mapper→worker 信号路径、无需 activeTurn bool 字段。

> codex 的 `turn/steer` 能力目前**全仓库零 caller**（预留），本设计首次接线。建议实施时补 codex steer 集成测试（CC 已实测，codex 基于代码分析 + 协议原语存在）。

## 兜底路径（保留 pending-replay，仅对不支持 worker）

`PendingBuffer` 放 **gateway/Bridge 层**（不放 messaging——done 触发点 `finishRuntimeOnDone` 在 bridge，放这避免跨层）。bridge 持有：

```go
// internal/gateway/bridge.go
type Bridge struct {
    ...
    pendingMu sync.Mutex
    pending   map[string][]pendingEntry  // sessionID → 追问条目
}

type pendingEntry struct {
    content    string
    envelope   *events.Envelope  // 原消息 envelope（含 sessionID/owner/metadata）
    receivedAt int64
}
```

方法（沿用 pending-replay 设计）：

| 方法 | 行为 |
|---|---|
| `BufferPending(sid, env, content)` | 容量内追加并返回成功；达到 `maxPendingPerSession`（默认 20，包含在途重放）时明确拒绝新消息，不驱逐任何已确认条目 |
| `DrainAndMerge(sid) (merged string, repr *Envelope, ok bool)` | 原子取出并清空；单条原样、多条加合并头+编号；repr 取最后一条 envelope 作重投模板 |
| `Clear(sid)` / `ClearAll()` | 清理 |

**done 时重投**（`bridge_forward.go:527` `FinishRuntime` 成功后）：

Bridge 不持有 Handler 引用（Bridge 先于 Handler 构造），故新增 narrow 接口 + late setter（仿 `SetAuditCollector:227`），在 `gateway_run.go:557` 注入 `bridge.SetPendingReplayer(handler)`：

```go
// internal/gateway/bridge.go
type PendingReplayer interface {
    DeliverReplay(ctx context.Context, env *events.Envelope) error
}
```

`finishRuntimeOnDone` success 后（注意该方法运行在**已持有的 seq lease** 下，重投必须 goroutine 异步、不重入 `BeginSeqOperation`）：

```go
if merged, repr, ok := b.pending.DrainAndMerge(sessionID); ok && b.replayer != nil {
    replayEnv := cloneForReplay(repr, merged)  // gateway 私有 helper
    go func() { _ = b.replayer.DeliverReplay(context.Background(), replayEnv) }()
}
```

`Handler.DeliverReplay` 实现走 `deliverToWorker`（重新 accept，此时 active gate 已释放、不会 busy）。

- 重投用**新 `client_message_id`**：`cloneForReplay` 调 `events.Clone`（深拷贝 Data/Metadata）+ 换 `env.ID`=`aep.NewID()` + 写 `data["client_message_id"]=aep.NewID()`，避免 `UNIQUE(session_id, client_message_id)` 去重误判。
- 重投又 busy（done 后瞬间被占，极端）：递归 `BufferPending` 回去，等下一个 done。
- `cloneForReplay` 放 **gateway 内部**（`pending_buffer.go`）：`pkg/events` 不能 import `pkg/aep`（循环依赖），故 helper 不放公共包。

### 合并格式

- **单条**：原样，不注入框架文本。
- **多条**（`DrainAndMerge` 输出）：

```
（以下是上一轮执行期间追加的 3 条消息，请一并处理）
1. 继续
2. 补充：重点关注金融领域
3. 换个角度
```

条目按收到时序编号；content 原文忠实保留，仅 `SanitizeText` 清洗。

## 文案与即时反馈机制（不改 AEP）

**基于代码勘察的简化**：当前 busy 信号已经有一条现成的 hub→conn 通道——gateway `sendErrorf` 发 Error envelope → hub `SendToSession` → 各 conn `handleError`（feishu `conn.go:363` / slack `conn_events.go:73` / yuanxin `adapter.go:606`）用 `ExtractErrorMessage` 取文案、走**通用错误兜底**（这正是误导文案的来源）。三 conn 均不区分 Error Code。

注意：gateway `Bridge` 与 messaging `Bridge` 是两层不同对象，gateway Bridge **不持有** messaging platform adapter。因此原草案的 `SupplementNotifier`/`NotifySupplementAccepted`/bridge→adapter 反向持有不可行且多余——**去掉**。

正确做法：复用 hub 广播，但用 `message` envelope + metadata 标记（非 error，避免误终端态）：

- gateway busy 透传/兜底后，发一个 `message` envelope via `h.hub.SendToSession`，`Metadata["supplement_mode"] = "injected"|"buffered"`，Content 空。
- 三渠道 conn 的 message 处理入口（feishu `WriteCtx` `conn.go:238` / slack `handleDefaultText` `conn_events.go:137` / yuanxin `WriteCtx` `adapter.go:585`）先检查 `env.Metadata["supplement_mode"]`：非空则调各渠道文案发送（feishu `sendTextMessage:257` / slack `writeWithPostMessage:956` / yuanxin `SendResponse:507`），用自己的 i18n 文案、不展示空 Content；否则正常展示 worker 输出。
- 文案在各 messaging 渠道（符合 i18n 规范）：
  - injected：`⏳ 已收到，正在当前任务中一并处理`
  - buffered：`⏳ 已收到，当前任务完成后会自动处理`
- gateway 不硬编码渠道文案，不新增 AEP wire event（复用 `message` + metadata key）。
- webchat 不撞 busy（前端 follow-up queue 管控），收不到该 envelope；多 conn 场景若收到，显示空 Content（可接受，webchat 前端优化留 follow-up）。

## 各渠道接入（每渠道一处）

三渠道（`feishu`/`slack`/`yuanxin`）只需实现 `NotifySupplementAccepted` 回调，按 mode 发对应文案。**不再各自 `errors.As` 检测 `SESSION_BUSY`**（混合方案下渠道收不到 busy error）。

- busy 分支与 done 重投全在 gateway 层，渠道零决策逻辑。
- yuanxin 当前 busy 只 `Log.Error`（无用户提示），本改动补上提示，顺带消除其黑洞。

## 清理

| 时机 | 动作 |
|---|---|
| 透传 | 无状态，无需清理 |
| `ReplayPendingIfNeeded`（done 重投）后 | `DrainAndMerge` 已清空 |
| 各渠道 `conn.Close` | `Clear(sessionID)` |
| `PlatformAdapter.CloseSharedState` | `ClearAll()` |
| reset | 原地清空该 session 的 pending buffer，并使在途重放 token 失效，避免失败回补复活旧消息 |

**不主动超时**：长任务不应丢消息；生命周期由 session 兜底。

## 边界与异常

- **补充消息 ACK 与去重**：mid-turn input 不进 execution ledger（不占 active gate 槽），但 Gateway 为其维护有界的进程内 `client_message_id` 去重记录，并复用现有 `input.ack` 返回终态确认。ACK 丢失后在同一 Gateway 进程内重发不会再次注入或暂存；进程重启后记录不保留。
- **最低可观测**：透传增加 metric（`mid_turn_injected_total` / `supplement_buffered_total`）并调用 `CaptureInboundEvent`。
- **透传不污染崩溃恢复 lastInput**：CC `InjectMidTurn` 必须复用 `writeStreamInputLocked`（`input.go:43`）的 stdin 写内核，**不得调完整 `w.Input`**——否则 `SetLastInput` 会把 mid-turn 内容写入 lastInput，崩溃恢复（`bridge_worker.go` 重投 lastInput）将误重投 mid-turn 而非原 turn input。codex `InjectMidTurn` 走 `SteerTurn`、不经 `turn/start`，同样不更新 lastInput。
- **重投用新 id**：`events.CloneForReplay` 生成新 `client_message_id`，避免去重误判。
- **多 session 隔离**：buffer 按 `sessionID`，天然隔离。
- **重投又 busy**：递归 `BufferPending`，等下一个 done。
- **透传失败回退**：`InjectMidTurn` 返回 error 后 Gateway 再查 active gate；若原 turn 已结束，则按普通新 turn 投递，否则落入兜底。CC 在 Worker 内以 active-turn fence 将 `Done` 与 stdin 注入串行化，关闭检查与写入之间的 ghost-turn 窗口。
- **runtime 修复后重放**：`FinishRuntime` 直接成功或经 repairer 异步修复成功，都会触发 pending replay；repairer 成功回调只在终态已持久化后发出。
- **reset 期间**：同一 session ID 的 pending buffer 被显式清空；生命周期 token 防止更早的失败重放重新入队。
- **合并后 Worker 处理失败**：属正常 turn 错误，走各渠道现有 error 展示，不影响 pending 机制。
- **Worker 卡死永不 done**：lease 过期 → fence → fresh worker；兜底 buffer 残留由 `conn.Close`/`CloseSharedState` 兜底清理。

## 测试要点

- **CC 透传**：busy → `InjectMidTurn` 写 stdin → CC 在当前 turn 消化（mock CC 进程或集成实测）。
- **codex 透传**：busy → `SteerTurn` 调用，`threadID`/`turnID` 正确；`SteerTurn` 失败（过期 turnID）→ 回退兜底。
- **透传失败回退**：`InjectMidTurn` 返回 error → 落入 `BufferPending`。
- **兜底完整链路**：busy → buffer → done → `DrainAndMerge` → 合并重投（基座单测，mock worker 不实现 `MidTurnInjector`）。
- **合并顺序与编号格式**；单条原样（无合并头）。
- **上限 20 拒绝**：前 20 条保留，第 21 条返回明确失败且不发送 delivered ACK；即使内容相同，只要 client message ID 不同也分别保留。
- **重投新 id**；`seq` 由 Handle 重分配。
- **worker 能力探测**：实现 `MidTurnInjector` 走透传；不实现走兜底。
- **并发**：`BufferPending` 与 `DrainAndMerge` 并发安全（`-race`）；透传 stdin 写与 control 写的锁复用。
- **ACK 去重**：首次补充返回 `input.ack(delivered)`；相同 ID 重发返回 `duplicate: true`，注入/暂存只发生一次。
- **终态竞态**：CC `Done` 先于注入时拒绝 mid-turn 写并改走普通投递，不产生 ghost turn。
- **修复闭环**：runtime terminal repair 成功后触发 pending replay；失败重放与 `/reset` 并发时不得重新入队。
- **三渠道文案**：injected / buffered 两 mode 各发对应文案；yuanxin 不再黑洞。
- **清理**：reset / `conn.Close` / `CloseSharedState`。

## 改动文件清单

| 文件 | 改动 |
|---|---|
| `internal/worker/worker.go` | 新增 `MidTurnInjector` 接口 |
| `internal/worker/claudecode/worker.go` | 实现 `InjectMidTurn`（复用 `writeStreamInputLocked`） |
| `internal/worker/codexcli/worker.go` | 实现 `InjectMidTurn`（调 `manager.SteerTurn`） |
| `internal/gateway/handler.go` | `:536` busy 分支改造：透传/兜底决策；`notifySupplement`（hub 发 message+metadata）；`DeliverReplay`（实现 `PendingReplayer`） |
| `internal/gateway/pending_buffer.go`（新） | `pendingEntry` + `PendingBuffer`：`BufferPending`/`DrainAndMerge`/`Clear`/`ClearAll`（按 ID 幂等、payload 冲突检测、容量20）；私有 `cloneForReplay`（`events.Clone`+`aep.NewID`，写新 `client_message_id`） |
| `internal/gateway/bridge.go` | `pending *PendingBuffer` 字段；`PendingReplayer` 接口 + `SetPendingReplayer` late setter（仿 `SetAuditCollector:227`）；`Clear`/`ClearAll` 代理 |
| `internal/gateway/bridge_forward.go` | `:527` done 成功后 `DrainAndMerge` + goroutine 异步 `replayer.DeliverReplay`（避开 seq lease） |
| `cmd/hotplex/gateway_run.go` | `:557` 注入 `bridge.SetPendingReplayer(handler)` |
| `internal/messaging/{feishu,slack,yuanxin}/conn*.go` | message 处理入口识别 `Metadata["supplement_mode"]`，调各自文案方法（feishu `sendTextMessage:257` / slack `writeWithPostMessage:956` / yuanxin `SendResponse:507`） |
| 对应 `*_test.go` | 透传单测、兜底单测、codex steer、竞态回退、三渠道 mode 文案识别、`-race` |

## 关键决策与理由

| 决策 | 选择 | 理由 |
|---|---|---|
| 总体策略 | 混合：透传优先 + 不支持兜底 | CC/codex 原生支持 mid-turn，透传最自然；acp 不可能，兜底保底 |
| 透传位置 | gateway 层统一决策 | busy 检测、worker 接口、active gate 都在 gateway；messaging 只管文案 |
| Worker 能力声明 | 可选接口 `MidTurnInjector`（duck-type） | 符合现有 `PermissionCeilingReporter` 模式；不实现者自动兜底，零特判 |
| CC 透传实现 | 复用 stdin stream-json 写 | 实测证明 turn 进行中写 = mid-turn 吸收，无需新协议 |
| codex 透传实现 | 调已有 `SteerTurn` | 协议级 mid-turn 原语已存在，仅需接线（首次 caller） |
| codex 过期 turnID | 不清，靠 busy 必 active + 失败回退 | 最小改动；竞态由兜底覆盖 |
| 兜底位置 | gateway/Bridge 层 | done 触发点在 bridge，避免跨层（pending-replay 放 messaging 的改进） |
| typed error（CodedError） | **不做** | 混合方案 busy 不返回渠道，无消费者；简化 |
| 文案机制 | hub 广播 `message` + `Metadata["supplement_mode"]`，三 conn 识别发文案 | 复用现有 hub→conn 通道，不改 AEP，文案留 messaging 层；gateway Bridge 无需反向持有 messaging adapter |
| done 重投回桥 | `PendingReplayer` narrow 接口 + late setter（仿 `SetAuditCollector`） | Bridge 先于 Handler 构造、无 handler 引用；用接口避免循环耦合 |
| 透传即时反馈 | 发提示 | IM 渠道 worker 回复可能很久，用户需即时确认追问没丢 |
| 透传 ledger | 不进（仅 metric） | 不占 active gate；mid-turn 状态由 worker agentic loop 持有 |
| 超时 | 不主动超时 | 随 session 清理；长任务不应丢消息 |
| 单 session 上限 | 20 条（兜底，包含在途重放） | 防滥用与内存；满时拒绝新补充，绝不丢弃已返回 delivered ACK 的条目 |

## 非目标

- 不改 active gate 单活跃约束与 partial unique index。
- 不引入 gateway 层 `ExecutionQueue`（issue #868）。
- 不做交互式卡片（pending-replay 方案 C，未来可选）。
- 不自动重投 delivery `unknown` 的输入。
- 不持久化兜底 pending（仅内存；gateway 重启时 in-flight 暂存丢失，可接受——用户会重发）。
- 不为 ocs 本轮实现 mid-turn（本轮按兜底；未来 `opencode serve` 若支持可加）。
- WebChat 不涉及：前端已有 follow-up queue（`2026-07-16-webchat-follow-up-queue-design.md`，stop-and-send + 可编辑 FIFO 队列），范围不重叠。

## 与现有设计的关系

- **演进 `2026-07-25-session-busy-pending-replay-design.md`**：其兜底（PendingBuffer / 合并重投）保留为本设计对不支持 worker 的 fallback；新增 mid-turn 透传为主路径。合并格式继续复用，但容量策略改为“满时明确拒绝”，幂等改为按 client message ID + payload hash 判定，避免确认后丢失。
- **不冲突 `2026-07-16-webchat-follow-up-queue-design.md`**：该设计是 webchat **前端**页面级 FIFO 队列（stop-and-send），明确边界"不修改 Slack/飞书/Cron 入站行为"；本设计是 **gateway/messaging 层** mid-turn，聚焦 IM 渠道。该设计 :98 "不引入 Worker 原生 mid-turn steering" 是其前端设计的边界选择（彼时未验证 CC 支持），不约束 IM 层——本设计以 CC 实测为据，在 IM 层引入 mid-turn。
- **遵守 `2026-07-14-durable-ingress-reliability-closure-design.md`**：active gate 单活跃不变；透传注入当前 turn（不开第二个 execution），不触发"避免重复副作用"（该不变量针对重投，非注入）。
