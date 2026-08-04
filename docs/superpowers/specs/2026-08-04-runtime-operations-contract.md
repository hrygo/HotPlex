---
title: "HotPlex Runtime Operations Contract"
type: spec
status: approved
date: 2026-08-04
owners: [hotplex-runtime]
references:
  - docs/v2/ROADMAP.md
  - docs/v2/IMPLEMENTATION-ROADMAP.md
  - docs/v2/ARCHITECTURE.md
  - docs/research/2026-08-04-qm-hotplex-deep-research-report.md
  - docs/research/2026-08-04-qm-hotplex-second-order-calibration.md
  - docs/research/2026-08-04-qm-hotplex-orthogonal-architecture-review.md
---

# HotPlex Runtime Operations Contract

## 1. 定位

本 contract 冻结 HotPlex 的运行期操作契约：

- `EffectiveRuntimePlan` 和 fail-closed preflight；
- desired、execution、effect、observed、reconciled 事实分层；
- Gateway-owned `EffectLedger`；
- isolation capability report；
- repair、reconcile、fence 和 operator action；
- redaction、兼容、迁移、容量和完成定义。

该 contract 扩展现有 AgentSpec、AgentIdentity、`internal/execution`、Worker config、AEP、eventstore、audit、observability、Cron 和 doctor，不创建平行 runtime layer、event bus、identity service、memory service 或 workflow engine。

## 2. 设计不变量

1. WS init、REST session creation、worker start、doctor、admin diagnostics 和 recipe dry-run 使用同一 plan resolver。
2. Plan、input execution、Gateway-owned effect、observed state 和 reconciliation 互不替代。
3. `unknown` 表示现有 evidence 无法证明成功或失败，不自动转换为 failed 或 retry。
4. Plan、effect、audit、event 和 Cockpit 默认脱敏。
5. SQLite/PostgreSQL migration、条件更新和故障测试成对实现。
6. AEP 新 Kind/Data/Metadata 增量兼容并同步 SDK、示例和协议测试。
7. Repair/reconcile 具备 lease、attempt cap、backoff、stop condition 和 operator escape hatch。
8. 通用抽象由已验证的垂直切片提取，不先建立平台再寻找消费者。

## 3. 事实模型

```text
Authority / Scope / Capability
            ↓
DesiredRuntimePlan
            ↓
DurableExecution / GatewayEffect
            ↓
ObservedRuntimeState
            ↓
Reconciliation / Repair / Fence / OperatorAction
```

| 事实 | 含义 | Canonical owner |
| --- | --- | --- |
| Authority / Scope | 谁能对哪个 workspace/session/capability 执行动作 | identity/config/session |
| DesiredRuntimePlan | HotPlex 期望如何启动和执行 | plan resolver |
| DurableExecution | input 是否 accepted、owned、delivered、running 或 terminal | `internal/execution` |
| GatewayEffect | Gateway-owned external effect 的 claim、attempt 和 outcome | EffectLedger |
| ObservedRuntimeState | Worker/provider/channel 的外部可验证状态 | adapter evidence |
| Reconciliation | desired 与 observed 的收敛动作和证据 | reconciler/operator |

`plan_hash`、`execution_id`、`worker_run_id`、`effect_id` 和 `evidence_ref` 是关联键，不代表彼此可以替代。

## 4. EffectiveRuntimePlan

### 4.1 数据模型

```go
type EffectiveRuntimePlan struct {
    Version       int
    Resolver      string
    PlanHash      string
    AgentSpec     agentspec.AgentSpec
    Identity      agentspec.AgentIdentity
    WorkerType    string
    WorkspaceID   string
    Permission    PermissionSummary
    Sandbox       SandboxCapabilitySummary
    EnvProfile    string
    EnvKeys       []string
    CapabilityIDs []string
    SkillHash     string
    ConfigHash    string
    SourceRefs    []PlanSourceRef
    Warnings      []PlanWarning
    Blocked       []PlanBlockReason
}
```

字段约束：

- `PlanHash` 对 canonical redacted plan 做 SHA-256；
- `EnvKeys` 只记录允许注入的 key name；
- `CapabilityIDs`、`SkillHash`、`ConfigHash` 和 `SourceRefs` 解释 plan 来源和变更；
- `Warnings`、`Blocked` 使用有界枚举和 redacted detail；
- 不保存 prompt、metadata value、secret、credential、完整命令、raw provider request、完整 tool args 或 raw worker error。

### 4.2 Precedence

```text
compiled defaults
  -> base config
  -> platform/bot config
  -> workspace override
  -> session init metadata
  -> validated runtime capability
```

等价输入产生相同 canonical plan hash。未知 Worker、冲突显式值、派生 env/secret/profile 不一致、ownership/permission/workdir/origin 无效或 strict capability 不可验证时，preflight fail closed。

### 4.3 共同消费者

- WebSocket init；
- REST session creation；
- Worker start；
- `hotplex doctor --runtime-plan`；
- Admin read-only runtime plan API；
- Recipe dry-run；
- Cockpit redacted plan projection。

共同消费者不得重新计算 precedence、backend、env profile 或 isolation conclusion。

## 5. ObservedRuntimeState

```go
type ObservedRuntimeState struct {
    PlanHash       string
    WorkerType     string
    Backend        string
    ArtifactDigest string
    Capability     map[string]string
    ProviderRef    string
    EvidenceRef    string
    ObservedAt     int64
    Confidence     string
}
```

约束：

- Capability value 只允许 `declared`、`observed`、`enforced`、`partial`、`unavailable`、`unknown`；
- `ProviderRef` 和 `EvidenceRef` 为 redacted external/event/audit/query reference；
- plan 生成成功但 worker/backend/artifact 未被观测时，状态为 `planned` 或 `unknown`；
- 配置 hash、token、audit 或成功返回路径不把 capability 提升为 `enforced`。

## 6. Gateway-owned EffectLedger

### 6.1 范围

纳入：

- external message delivery；
- Webhook trigger；
- Cron delivery；
- admin/control write；
- recipe delivery；
- 需要 durable retry/reconcile 的 connector operation。

不纳入：

- worker-private tool call；
- model-provider 内部 retry；
- 纯内存 UI state；
- 只在 Worker 私有协议内可见的动作。

### 6.2 状态

```text
planned -> started -> succeeded
                  -> failed
                  -> unknown -> reconciled_succeeded
                             -> reconciled_failed
                             -> fenced
```

每个 effect 记录：

- `effect_id`、`execution_id`、`worker_run_id`；
- `effect_type`、business idempotency key；
- claim owner、lease token/expiry；
- attempt、started/finished/observed timestamps；
- provider reference、evidence/confidence；
- redacted outcome 和 unknown reason。

attempt 是事实字段，不是去重键。重复请求返回已有 effect fact，不盲目触发第二次外部动作。

### 6.3 与 Input Execution 的关系

- input accept、delivery、owner lease、runtime status 和 ambiguity fence 继续由 `internal/execution` 管理；
- EffectLedger 只记录 execution 下的 Gateway-owned external effect；
- effect `unknown` 不把 input execution 直接改为 completed；
- Worker `done` 不把 effect 改为 succeeded；
- reconcile 结果写入 effect fact，并关联 audit/eventstore evidence。

### 6.4 External Ack

- provider/connector contract 明确确认成功后写 succeeded/ack；
- response 丢失、timeout 或连接中断进入 unknown 或可查询 pending；
- 只有 provider contract 和当前 evidence 证明安全时才 retry；
- 无法查询或安全 retry 的 effect 进入 fence 或 operator action。

## 7. Reconciliation Contract

Reconciler 输入是 desired plan、open execution/effect、owner lease、observed evidence 和 retry policy。每次动作必须满足：

- 当前 owner/lease 有效；
- attempt 未超过上限；
- backoff 到期；
- stop condition 未满足；
- effect idempotency/provider contract 支持该动作；
- 写入 audit actor/action/reason/evidence ref；
- late completion 只能收敛同一 worker run/effect。

Operator action 必须经过 workspace/session/admin authorization，支持按 session/effect fence、resolve 或 abandon，并保留审计记录。

## 8. Isolation Capability Report

每个 Worker 报告：

| 能力 | 允许值 |
| --- | --- |
| env profile | `compat` / `strict` |
| filesystem | `workspace` / `partial` / `unavailable` |
| network | `enforced` / `partial` / `unavailable` |
| credential injection | explicit key names + broker/reference type |
| host env inheritance | true/false + compatibility warning |
| artifact/backend | observed digest/type 或 `unknown` |

Strict profile 拒绝未知 host env 和不可验证的必需能力。Compat profile 保持现有兼容行为并输出迁移告警，不报告强隔离。

## 9. Observability 与容量

- trace/log/audit/event 使用一致的 agent/session/execution/effect keys；
- metrics 只使用有限枚举；plan hash、workspace、provider/evidence ref 不作为无界 label；
- plan resolver 使用 versioned、bounded snapshot；
- Cockpit/API 查询具有 pagination、time window、payload limit、authorization 和 connection budget；
- effect/audit/snapshot 具有 retention 和 cleanup policy；
- repair queue 具有 capacity、concurrency 和 rate limit。

## 10. 兼容与迁移

- AEP 新字段增量兼容；同步 Go SDK、TypeScript/Python/Java 示例 SDK 和双向协议测试；
- Worker contract 变化同步四 adapter、mocks、noop 和 registry tests；
- SQLite/PostgreSQL migration、conditional update、multi-instance 和 interruption tests 成对；
- Linux/macOS/Windows env/path/process/injection 行为分别验证；
- 新 plan/effect projection 先 read-only 或 shadow；
- rollout 可暂停、可回退，repair 可停止，旧版本记录可读。

## 11. 完成定义

- WS/REST/doctor/worker/admin/recipe 共享 resolver 和 canonical plan hash；
- 四类 Worker 输出 capability report 和 observed bootstrap；
- Cron/Webhook/message delivery 在重启、多实例、DB failure、timeout、5xx、response loss 和 late confirmation 下保持唯一、可解释的 effect fact；
- `unknown` 不会触发不安全的发送、部署、删除或 control action；
- SQLite/PostgreSQL migration、race/fault-injection 和 real-PG multi-instance 测试通过；
- Cockpit 不产生 N+1、无界读取或 high-cardinality metrics；
- plan、effect、audit、event 和 Cockpit 默认不含敏感值或 raw payload；
- `make check`、`make docs-build` 和受影响模块 `-race -count=1` 通过。

## 12. 依赖约束

```text
AgentSpec / AgentIdentity / Durable Input Execution
                 ↓
EffectiveRuntimePlan + Observed Bootstrap
                 ↓
Cron/Webhook/Message Durable Effect
                 ↓
Bounded ExecutionQueue + Cockpit
                 ↓
Recipes / Capability Governance
```

第一条 external effect 垂直闭环完成前，不提取跨场景通用 scheduler 或 workflow abstraction。

## 13. 非目标

- 跨 session distributed scheduler；
- worker-private tool protocol 搬入 Gateway；
- skill marketplace、remote registry 或独立 memory backend；
- general workflow/DAG engine；
- 用 capability token、audit、screening、allowlist 或 plan hash 宣称形式化 non-interference；
- 改变现有 AEP v1 client availability 和 Worker core input/output semantics。
