---
type: spec
tags:
  - project/HotPlex
date: 2026-06-30
status: draft
progress: 0
review:
  - date: 2026-06-30
    type: adversarial (3 independent agents)
    findings: 5 substantive (content_len 误证 / Fix A goroutine 泄漏 / Fix A 缺 IDLE→RUNNING 跃迁 / Fix B 对 ACP 无效 / 遗漏 L4)
related_issues:
  - "#815"
  - Worker-Turn-Summary-Parity-Spec.md (codex thread/turn 模型背景)
  - First-Turn-History-Missing-Fix-Spec.md (history 注入机制)
  - Turns-Materialized-Table-Spec.md (turns 表查询基础)
---
# Messaging Session 跨 Turn 上下文断裂修复 Spec

> **v2 修订（2026-06-30 对抗审查后）**：删除 `content_len=13` 误证、Fix A 增加配套设计（forward 退出 + IDLE→RUNNING 跃迁）、Fix B 拆分 codex/acp、新增 L4（单例 idle drain）、Q3 给确定答案。

## 概述

**症状**：飞书（及 Slack 等 messaging 平台）session 中，用户两次提问间隔超过 30 分钟时，第二次提问 codex 完全丢失前文上下文 —— 表现为「不知道用户在指什么」。

**根因层级（四层）**：
- **L1（结构根因）**：messaging 平台 session 在 turn 完成后，缺少 `RUNNING → IDLE` 转换路径。
- **L2（判杀）**：session 永久停在 `RUNNING`，30 分钟后被 zombie `ExecutionTimeout` 判杀 → `TERMINATED`。
- **L3（codex 重建丢上下文）**：codex `CanResumeTerminated()=false` → 走 fresh start → 新建 ephemeral thread；且 `prepareWorkerInfo` 的 history 加载条件在 fresh-start 路径被 `State==Created` 跳过。
- **L4（单例进程死亡，对抗审查新增）**：codex 单例 manager 进程在 refs 归零 30 分钟后被 idle drain 杀死，所有 ephemeral thread 丢失。gateway 重启 / bridge shutdown / 所有 session 释放都会触发，此时 Fix A 的保活策略失效，必然回退到 L3。

**影响范围**：所有 messaging 平台（飞书 / Slack / 元芯）+ 所有非 native-resume worker（codex / acp / ocs）。webchat 不受 L1 影响（WS 断开转 IDLE），但仍受 L3/L4 影响。

> **范围**：本 spec 修复「turn 完成后的 session 状态机正确性」与「fresh-restart 路径的 history 兜底」。不涉及 turn summary 数据字段（见 `Worker-Turn-Summary-Parity-Spec.md`）。

---

## 1. 复现证据（生产日志）

实例：dev gateway，`configs/config-dev.yaml`，codex_cli worker，飞书平台。

| 时间 | 事件 | 关键字段 |
|------|------|---------|
| `09:10:05.218` | 用户提问 1 投递 worker | `session_id=5eb6cba8` |
| `09:11:37.931` | turn/completed（turn=3） | `thread_id=019f1602-...` |
| _（~29 分钟空闲，session 一直 RUNNING，未转 IDLE）_ | | |
| `09:40:36.470` | **zombie 触发** | `last_io=09:10:05` `timeout=30m0s` |
| `09:40:36.476` | session `running → terminated`（reason=`zombie`） | codex unsubscribe `019f1602`，refs=0 |
| `09:42:58.787` | **跳过 resume**：`worker cannot resume terminated state` | codex `CanResumeTerminated()=false` |
| `09:42:58.789` | `session: created`（fresh record） | `si.State == StateCreated` |
| `09:42:59.520` | codex subscribed **全新 thread `019f1631-...`** | ≠ `019f1602` |

> ⚠️ **`content_len=13` 不是 history 注入与否的证据**（对抗审查修正）：`content_len` 日志在 `internal/gateway/handler.go:264` 打印，位于 `:266 w.Input()` 调用**之前**；而 `injectHistoryPrefix` 在 `Input()` **内部**（`worker.go:226`）执行。因此无论 history 是否注入，日志都显示原文长度。L3 的证据是**代码路径**（fresh-start → `State==Created` → `bridge.go:844` 条件不满足），不是 content 长度。

---

## 2. 根因代码定位

### 2.1 L1 — messaging 缺少 turn 完成 → IDLE 转换

全仓库 `RUNNING → IDLE` 转换仅两处：

| 位置 | 触发条件 | 适用平台 |
|------|---------|---------|
| `internal/gateway/conn.go:146` | webchat **WS ReadPump defer**（WS 断开） | 仅 webchat |
| `internal/gateway/bridge.go:619` | `SwitchWorkDir` | 所有平台（特殊操作） |

`bridge_forward.go` 的 done 事件处理（`:244-292`）**只做 stats 累积、retry 判定、turnText reset，不触发任何状态转换**。messaging 平台不走 webchat WS，故其 session turn 完成后**永不自然转 IDLE**。

### 2.2 L2 — zombie 判杀

`internal/session/manager.go:1310-1325` zombie 扫描基于 `LastIO()`，timeout 默认 30m（`ExecutionTimeout`，`config_defaults.go:55`）。codex worker 只在 `Input()`（`worker.go:222/262`）与 `startNewThread()`（`:369`）调用 `SetLastIO` —— **turn 执行期间完全不刷新 LastIO**，`last_io` 停在 input 时间。

### 2.3 L3 — codex fresh-restart 丢上下文

**3a. codex 无法 resume terminated**：`worker.go:165` `CanResumeTerminated() bool { return false }`。`bridge.go` StartPlatformSession 对 TERMINATED + 无 worker 的 session，检测到 `!CanResumeTerminated` 即走 `startOrResumeOnInUse` → fresh `Start()` → `startNewThread()` → 新 ephemeral thread。

**3b. fresh-restart 跳过 history 加载**：`internal/gateway/bridge.go:844`：

```go
if si.WorkerType == worker.TypeCodexCLI && si.State != events.StateCreated && b.turnsQuerier != nil {
    turns, err := b.turnsQuerier.QueryTurns(...)
```

fresh-restart 后 `si.State == StateCreated`（日志 `session: created` 实证），条件不满足 → `ConversationHistory` 为 nil → `injectHistoryPrefix` 原样返回 content。同 sessionID 在 turns 表里的历史记录未被利用。

> **ACP 平行缺口（对抗审查确认）**：`internal/worker/acp/worker.go` **完全没有** `injectHistoryPrefix` / `pendingHistory` / `ConversationHistory` 字段（grep 零命中）。ACP 靠 `WorkerSessionID` + `client.LoadSession` 恢复，terminated→fresh-restart 走 `forceNewSession()` 后 `WorkerSessionID` 对不上 → `LoadSession` 失败 → 仅发 `HISTORY_LOST` 错误。即便 Fix B 把 ACP 加入 `prepareWorkerInfo`，ACP 的 `Input()` 也不读这个字段 —— **Fix B 对 ACP 是 no-op**。

### 2.4 L4 — codex 单例 idle drain（对抗审查新增）

`internal/worker/codexcli/manager.go` `Release()` refs 归零后启动 idle drain 计时（`IdleDrainPeriod` 默认 30m，`config_defaults.go:80`），到期 `ForceKill` 单例进程。codex thread 是 ephemeral（存在进程内存），**进程死亡 = 所有 thread 丢失**。

- **正常运行**：Fix A 保活策略让 IDLE session 持有 worker → refs≥1 → 进程不死 → L4 不触发。
- **触发场景**：gateway 重启 / bridge shutdown / crash / 所有 codex session 都被 GC → refs 归零 → 30m 后进程死亡 → 所有 IDLE session 的 thread 丢失 → 下一消息必走 L3 fresh-restart。

**结论**：L4 是独立根因层，Fix A 的「真实上下文连续」承诺在重启后失效，Fix B 是 L3+L4 路径的共同兜底，**必须做**。

---

## 3. 方案设计

三个 Fix，按「治本 → 兜底 → 防误杀」。**Fix B 是必做项（L3+L4 兜底）**；Fix A 是治本但需配套；Fix C 独立低风险。

### Fix A（P1，治本）— messaging turn 完成后转 IDLE

**目标**：messaging session 在 done 后进入 IDLE，用 `idle_timeout`（60m）替代 zombie 30m 硬杀；IDLE 是「可恢复」语义。**对抗审查揭示：这不是「加一行 Transition」，需三处配套。**

#### A1. 转换触发点

`bridge_forward.go` done 事件分支（`:289-292`）后，对 messaging 平台调 `sm.Transition(sessionID, StateIdle)`。webchat 不在此转（它走 conn.go:146），靠 `IDLE→IDLE` 状态机拒绝 + 已有 Warn 日志去重。

#### A2. forwardEvents goroutine 退出（对抗审查 P0 配套）

问题：转 IDLE 后 worker 保活，`w.recvCh` 不关闭，`bridge_forward.go:156 for env := range recvCh` 永久阻塞 → 每次对话泄漏一个 forwardEvents goroutine。

方案：转 IDLE 后让 forwardEvents goroutine **主动退出**（return），worker 通过 manager Subscribe 保持存活。下次 IDLE→RUNNING 时由 bridge 重新拉起 forwardEvents goroutine。

**需核实**：bridge 是否支持「worker 存活但 forwardEvents 未跑」的中间态，以及 IDLE→RUNNING 时重启 forwardEvents 的触发点（应在 `deliverToWorker` 的 `TransitionWithInput` 成功后）。

#### A3. IDLE→RUNNING 跃迁落点（对抗审查 P0 配套）

`StartPlatformSession`（`bridge.go:412-430`）对 IDLE+存活 worker 走 `IsActive()` reuse 分支，**直接 `return nil` 不转状态**。后续 input 走 `handler.go` `deliverToWorker`，此处负责 `TransitionWithInput(IDLE→RUNNING)`。

**必须验证**：飞书 messaging 路径下，`deliverToWorker` 对 IDLE 的 `TransitionWithInput` 真实生效，且转换后复用存活 worker 的现存 `threadID`（而非走 `ResumeSession` → `startNewThread("resume")` → `cleanupOldThread` 丢 thread）。spec §5 新增对应断言。

#### 取舍 — 保活 vs 清理

| 策略 | 行为 | 上下文连续性 | 复杂度 |
|------|------|------------|--------|
| **保活（推荐）** | IDLE 时 worker 仍挂 session，下次复用同 thread | ✅ 真实上下文 | 需 A2/A3 配套 |
| 清理 | IDLE 即 detach worker，下次建新 thread | 靠 Fix B 文本兜底 | 中 |

推荐保活，但**保活只覆盖正常运行，重启场景仍靠 Fix B**。

### Fix B（P1，codex 必做 + acp 独立）— fresh-restart 注入 history

**Fix B 是 L3 + L4 的共同兜底，必须做。** 按对抗审查拆分为两条独立交付物：

#### B-codex（生效）

`bridge.go:844` 放宽条件（codex 部分）：

```go
// after (codex only)
if si.WorkerType == worker.TypeCodexCLI && b.turnsQuerier != nil {
```

去掉 `State != StateCreated`：fresh session 的 turns 表自然为空，`len(turns) > 0` 守卫跳过，无副作用。唯一开销是每次 `prepareWorkerInfo` 多一次 `QueryTurns`（sub-ms，`resolveCachedHistory` 命中后趋零）。

**边界防御（对抗审查 #4）**：`DeletePhysical`（幂等删除）删 session 记录但**不级联删 turns 表**。`DeriveSessionKey` 同 ID 重派生 → `StateCreated` + turns 残留 → 错误注入旧历史。必须加守卫：注入前检查 `si.WorkerSessionID == ""`（fresh worker 无历史 session 关联）或对比 turns 最新时间戳与 session 创建时间。

**`/reset` 不受影响**：`/reset` 走 worker 层 `ResetContext`（`worker.go:575` 显式 `ConversationHistory = nil`），Fix B 改的是 bridge 层 `prepareWorkerInfo`，两者不同层不冲突。

#### B-acp（独立设计，不并入本 Fix）

ACP 无 `injectHistoryPrefix`，Fix B 对 ACP 是 no-op。ACP 的 L3 兜底需**独立方案**：要么给 ACP `Input()` 实现 `injectHistoryPrefix` 等价物，要么用 `turnsQuerier` 文本拼成 ACP `new session` 的初始消息。**本 spec 不覆盖 B-acp，列为独立子 issue**，避免「写了覆盖 acp」误导实施者跳过验证。

### Fix C（P2，防误杀）— forwardEvents 刷新 LastIO

消除 `last_io` 停在 input 时间的隐患 —— 防单次 turn > 30m 被 zombie 误杀（与 L1 无关的独立 bug）。

**改动**：`bridge_forward.go` forwardEvents 主循环每个非 Done 事件调 `w.SetLastIO(time.Now())`（`atomic.Store`，无锁，开销可忽略）。

**注意（对抗审查）**：Fix C 只在「事件确实到达」时刷新；若 codex 卡死但订阅通道开着、无事件，Fix C 无效 —— 此场景仍需 zombie 扫描兜底。不要因 Fix C 而放宽 `ExecutionTimeout`。TurnTimeout（默认 0=disabled）不在本 spec 覆盖范围。

---

## 4. 实施顺序

1. **B-codex 先行**（L3+L4 兜底，最高 ROI，低风险）：立即修复 codex fresh-restart 丢上下文。
2. **Fix C**（独立低风险）：防长 turn 误杀。
3. **Fix A**（治本，需 A1/A2/A3 配套 + 充分测试）：完整闭环 messaging IDLE 路径。
4. **B-acp**（独立子 issue）：ACP 兜底，单独设计。

每步独立提交、独立测试。

---

## 5. 测试策略

| Fix | 测试 | 断言 |
|-----|------|------|
| A1 | messaging session 收到 done | `session.State == IDLE` |
| **A2** | N 次连续对话后 | forwardEvents goroutine 计数**不增长**（无泄漏） |
| **A3** | 飞书 IDLE session 收新消息 | 走 `deliverToWorker` reuse 路径；`threadID` **不变**；不触发 `ResumeSession`/`cleanupOldThread` |
| A | webchat WS 断开（已 IDLE） | `IDLE→IDLE` 被状态机拒绝 + Warn，不报错 |
| **A-TOCTOU** | done 与新 input 并发 | input 不被 `SESSION_BUSY` 拒绝（`RUNNING→RUNNING` 非法转换防护） |
| B-codex | terminated→fresh-restart，turns 表有 3 条历史 | `ConversationHistory` 非空；首次 Input content（注入后）含 `CONVERSATION_HISTORY_` |
| B-codex | `DeletePhysical` 后同 ID 重建 | turns 残留不被错误注入（守卫生效） |
| B-codex | `/reset` 后首次 input | `ConversationHistory` 为 nil（ResetContext 清空，不被绕过） |
| B-codex | 全新 session（turns 空） | `ConversationHistory` 为 nil |
| C | 长 turn（模拟 35m 持续 delta） | `LastIO` 持续刷新；zombie 不触发 |

新增 `prepareWorkerInfo` 表驱动测试：State × WorkerType × turns 计数 × WorkerSessionID 矩阵。

---

## 6. 验收标准

**功能**：
- 飞书 session 间隔 < `idle_timeout`（默认 60m）的连续提问，codex `thread_id` **保持不变**（真实上下文连续）。
- 间隔 > `idle_timeout` 或 **gateway 重启后**（L4 触发）的提问，新 thread 首次 input content **包含 `CONVERSATION_HISTORY_` prefix**（B-codex 文本兜底）。
- 单次 turn > 30m 不被 zombie 误杀（Fix C）。
- 连续 N 次对话后 forwardEvents goroutine 计数稳定（Fix A2）。

**非功能**：
- `prepareWorkerInfo` 对 fresh session 的额外 `QueryTurns` < 1ms，缓存命中趋零。
- 状态机无非法转换警告刷屏。

**回归**：
- webchat Fast Reconnect、`/reset`、`/gc`、SwitchWorkDir、crash recovery 不回归。

---

## 7. 配置与未决问题

### Q3（对抗审查已确认答案）

`IdleTimeout` 两处默认值已查清：
- `config_defaults.go:21` `GatewayConfig.IdleTimeout = 5min` —— **dead config**，全仓库零消费者（仅 `config_types.go:306` 声明）。
- `config_defaults.go:54` `WorkerConfig.IdleTimeout = 60min` —— **实际生效**，唯一消费者 `manager.go:444`（设 `IdleExpiresAt`）。

Fix A 转入 IDLE 后实际用 `Worker.IdleTimeout`（60m）。`GatewayConfig.IdleTimeout` 是独立 dead-config bug，**顺手开独立 issue 清理**（删除或文档化"未使用"），否则 operator 误改 5min 那个值会制造运维事故。

### 仍需实施时确认

- **Q1（Fix A3）**：`deliverToWorker` 对 IDLE 的 `TransitionWithInput(IDLE→RUNNING)` 生效后，是否确实复用存活 worker 的现存 `threadID`？需追踪 handler.go 该路径。
- **Q2（B-acp）**：ACP 兜底方案选型（`injectHistoryPrefix` 等价物 vs turnsQuerier 拼初始消息），独立子 issue 设计。

---

## 附录：对抗审查发现处置表（2026-06-30）

| # | 发现 | 来源 | 处置 |
|---|------|------|------|
| 1 | `content_len=13` 是误证（日志在 Input 前） | Agent-1 | §1/§2.3 删除铁证，改代码路径推演 |
| 2 | Fix A goroutine 泄漏 | Agent-2 | §3 A2 配套：转 IDLE 后 forward 退出 |
| 3 | Fix A 缺 IDLE→RUNNING 跃迁 | Agent-2/3 | §3 A3 配套：deliverToWorker 落点 + 断言 |
| 4 | Fix B 对 ACP 无效 | Agent-2/3 | §3 拆 B-codex / B-acp，B-acp 独立子 issue |
| 5 | 遗漏 L4 单例 idle drain | Agent-3 | §2.4 新增 L4，Fix B 升级为必做 |
| 6 | `GatewayConfig.IdleTimeout` dead config | Agent-3 | §7 Q3 给答案，开独立清理 issue |
| 7 | done 与新 input TOCTOU 竞态 | Agent-3 | §5 新增 TOCTOU 测试用例 |
