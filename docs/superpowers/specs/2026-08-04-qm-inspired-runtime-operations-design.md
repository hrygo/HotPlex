---
title: "QM-inspired Runtime Operations Design"
type: spec
status: approved
date: 2026-08-04
owners: [hotplex-runtime]
references:
  - docs/v2/ROADMAP.md
  - docs/v2/IMPLEMENTATION-ROADMAP.md
  - docs/research/2026-08-04-qm-hotplex-deep-research-report.md
  - https://github.com/yc-software/qm
---

# QM-inspired Runtime Operations Design

## 1. 目标

本 spec 将 HotPlex 已有的 AgentSpec、AgentIdentity、execution ledger、worker config、audit、observability 和 CLI doctor 定义为统一的运行期操作契约，吸收 qm 的三个高 ROI 机制：

- resolved runtime plan；
- fail-closed preflight；
- Gateway-owned effect ledger。

本 spec 不创建新的 runtime layer、event bus、identity service、memory service 或 workflow engine。

## 2. 设计原则

1. **同一解析结果**：WS init、REST create-session、worker start、doctor、admin diagnostics 和 recipe dry-run 必须使用同一个 resolver。
2. **计划与执行分离**：plan 是可审计的期望状态；execution ledger 是实际副作用状态；二者不得互相冒充。
3. **unknown 优先安全**：网络中断、进程崩溃或超时无法证明结果时，状态进入 `unknown`/fence，不自动把它当成 failed 并盲目重做。
4. **redacted by default**：plan、audit、事件和 Cockpit 不保存 prompt、metadata 值、凭证、原始 worker 错误或工具参数；只保留类型、哈希、引用和有界原因。
5. **SQLite/PostgreSQL 成对实现**：任何持久化字段、状态迁移、条件更新和测试必须同步两种方言。

## 3. EffectiveRuntimePlan

### 3.1 数据模型

```go
type EffectiveRuntimePlan struct {
    Version       int
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
    SourceRefs    []PlanSourceRef
    Warnings      []PlanWarning
}
```

字段要求：

- `EnvKeys` 只记录允许注入的 key 名，不记录值；
- `CapabilityIDs`、`SkillHash` 和 `SourceRefs` 用于解释 plan 来源与变更；
- `PlanHash` 对 canonical redacted plan 做 SHA-256；
- `Warnings` 只能是有界枚举 + redacted detail；
- 现有 session key、Worker 接口和 AEP v1 旧字段保持兼容。

### 3.2 解析输入与 precedence

解析输入按当前 HotPlex 规则合并：

```text
compiled defaults
  -> base config
  -> platform/bot config
  -> workspace override
  -> session init metadata
  -> validated runtime capability
```

任何未知 worker、冲突的显式值、无法验证的 sandbox capability 或派生 env 与 secret/profile 不一致，都必须在 preflight 失败；不能由后续 worker 启动才暴露。

## 4. Preflight / dry-run

### 4.1 能力

提供只读的：

- `hotplex doctor --runtime-plan`；
- Admin read-only runtime plan endpoint；
- recipe dry-run 的 resolved execution plan。

输出包含：worker、workspace、permission mode、env profile、注入 key 名、sandbox filesystem/network capability、budget/timeout、policy summary、plan hash 和 warnings。

不输出：prompt、metadata value、secret value、完整命令、原始 tool args、provider request 或 raw worker error。

### 4.2 fail-closed 检查

- worker type 已注册且 resolver 与实际 builder 一致；
- worker env 仅来自 allowlist、显式 `HOTPLEX_WORKER_*`、session/config override；
- backend 派生出的必需 secret/profile 已被同一 effective plan 计算；
- public URL/origin、workdir、workspace ownership、permission mode 均通过边界校验；
- capability report 明确 filesystem/network 是 enforced、partial 还是 unavailable；
- SQLite/PostgreSQL schema 与迁移版本一致。

## 5. Gateway-owned EffectLedger

### 5.1 范围

纳入：外部消息 delivery、Webhook trigger、Cron delivery、admin/control write、recipe delivery、需要重试的 connector operation。

不纳入：worker 私有进程内部的 tool call、模型 provider 内部重试、纯内存 UI 状态。

### 5.2 状态

```text
planned -> started -> succeeded
                  -> failed
                  -> unknown -> reconciled_succeeded / reconciled_failed / fenced
```

唯一键至少包含：`execution_id + effect_type + idempotency_key`。attempt 是事实字段，不是去重键。重复请求只能返回已有事实，不得重新触发外部副作用。

### 5.3 与现有 execution ledger 的关系

- `execution_id`、owner lease、runtime status、ambiguity fence 继续由 `internal/execution` 管理；
- EffectLedger 只记录 execution 下的 Gateway-owned effect；
- effect 的 unknown 不得把原 execution 直接改成 completed；
- reconcile 通过外部状态查询或人工操作完成，并写入现有 audit/eventstore 引用；
- 不新增第二套通用 event bus。

## 6. Isolation capability report

每个 Worker 在 plan 中报告：

- env profile：`compat` / `allowlist`；
- filesystem：`workspace` / `partial` / `unavailable`；
- network：`enforced` / `partial` / `unavailable`；
- credential injection：显式 key 名和 broker/reference 类型；
- compatibility warning：是否仍继承 host env。

报告是事实，不是承诺。无法证明的能力必须标记 `unavailable` 或 `partial`；Strict profile 才能拒绝未知 host env，compat profile 必须带迁移告警。

## 7. 验收标准

- WS init 与 REST create-session 在等价输入下产生相同 canonical plan hash；
- doctor、worker start、admin read API、recipe dry-run 使用同一 resolver；
- unknown effect 不会自动重复发送/部署/删除；
- 同一 idempotency key 在多实例、重启、超时和 DB 短暂不可写条件下最多触发一次外部 effect；
- SQLite 与 PostgreSQL 迁移、条件更新、race/fault-injection 测试成对通过；
- four-worker contract（claude_code/codex_cli/opencode_server/acp）均输出 capability report；
- plan、audit、runtime event、Cockpit 默认不含 prompt、secret、raw worker error 或完整 tool args；
- `make check`、`make docs-build`、受影响模块 `-race -count=1` 通过。

## 8. 非目标

- 不实现跨 session 分布式 scheduler；
- 不把 worker-private tool protocol 搬进 Gateway；
- 不实现 skill marketplace、remote registry 或独立 memory backend；
- 不宣称 sandbox/allowlist 已经提供形式化 non-interference；
- 不改变现有 AEP v1 客户端可用性和旧 Worker interface。

## 9. 交付顺序

1. 先把 `AgentSpec.Resolve` 与 BuildEnv/isolation profile 接入 EffectiveRuntimePlan；
2. 再提供 doctor/admin read-only plan 和 fail-closed checks；
3. 冻结 effect state machine 与 dual-DB schema；
4. 接入 Cron/Webhook/control，再接入 recipes；
5. 最后让 Execution Cockpit 消费 plan/effect facts。

## 10. 事实闭环：Desired / Execution / Observed / Reconciled

plan 与 effect 不是完整事实层；qm #44、#57、#136、#137、#138、#153、#165 以及 HotPlex Cron/Execution 路径共同确立外部观测与调和层。

### 10.1 事实分层

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

- `DesiredRuntimePlan` 表达 HotPlex 期望启动和执行什么；
- `DurableExecution` 表达 Gateway 是否接受、占有并尝试了动作；
- `GatewayEffect` 表达外部副作用的业务唯一键、attempt 和结果事实；
- `ObservedRuntimeState` 表达 Worker/provider/channel 的外部可验证状态；
- `Reconciliation` 通过查询、回调、重试、补偿、fence 或人工确认收敛差异。

`plan_hash`、`execution_id`、`worker_run_id`、`effect_id` 和 `evidence_ref` 是关联键，但不代表彼此可以互相替代。特别是：Worker `done` 不得直接证明消息已交付，plan hash 不得直接证明 sandbox layer 已应用。

### 10.2 observed state 最小模型

ObservedRuntimeState 采用下列最小语义字段：

```go
type ObservedRuntimeState struct {
    PlanHash       string
    WorkerType     string
    Backend        string
    ArtifactDigest string
    Capability     map[string]string // declared/observed/enforced/partial/unavailable
    ProviderRef    string             // redacted external receipt/reference
    EvidenceRef    string             // event/audit/query reference
    ObservedAt     int64
    Confidence     string             // observed/verified/unknown
}
```

禁止保存 prompt、secret value、完整 provider request、完整 tool args 或原始 worker stderr。无法验证的字段必须为 `unknown`，不能根据成功返回路径推断为 `enforced`。

### 10.3 EffectLedger 的第一条垂直切片

先实现 Cron/Webhook/消息 delivery 的 durable effect，不先覆盖 worker-private tools。每个 effect 至少要区分：

```text
claim → attempt → provider accepted? → externally verified?
                                  └→ unknown → reconcile / fence
```

旧的进程内 retry queue 可以保留作兼容 fallback，但不能作为重启和多实例后的唯一事实源。成功 ack 必须发生在外部发送成功之后；外部响应丢失时进入 `unknown`，而不是直接重做。

### 10.4 调和与可观测性边界

- Repairer/reaper 只能处理带 owner lease、attempt 上限和 stop condition 的事实；
- Cockpit 只读 redacted snapshot，必须分页、限时和限制 payload；
- `plan_hash` 可用于 trace/log/event correlation，不应直接作为高基数 metrics label；
- `desired`、`observed`、`drift`、`unknown`、`reconciled` 必须能在 audit/eventstore 中建立因果链；
- 所有跨数据库状态迁移和条件更新继续成对覆盖 SQLite/PostgreSQL，并增加重启、连接抖动和多实例 fault-injection 测试。

### 10.5 新验收条件

- plan 生成成功但 worker/backend/artifact 未被观测时，输出为 `planned` 或 `unknown`，不得输出 `enforced`；
- Cron delivery 在重启、超时、5xx、成功但响应丢失和晚到确认下不会盲目重复外部副作用；
- doctor、WS/REST start、worker start 和 recipe dry-run 使用相同 plan hash；
- Cockpit 查询不会通过 N+1 或无界读取拖垮数据库连接池；
- `unknown` 的 repair、reconcile、fence 和人工 escape hatch 均有可追溯审计记录。

## 11. 终版架构闸门

本 spec 只能在以下不变量同时成立时继续扩大范围。它们是设计约束，不是未来可选增强：

### 11.1 产品边界和事实所有权

- HotPlex 的产品边界仍是 self-hosted Agent Runtime Gateway；不因引入 scope、skill、run 或 effect 事实而新建 workflow engine、marketplace、独立 memory service 或跨 session scheduler。
- authority/scope、desired plan、durable execution、Gateway-owned effect、worker/provider observed state 和 reconciliation 必须各有唯一 canonical owner。
- Cockpit、admin、AEP、adapter 和 recipe 只能读取或投影 canonical facts；任何缓存必须带 source version/plan hash，并定义失效和降级。

### 11.2 故障模型和可靠性承诺

- 交付语义必须先于实现：明确 at-most-once、at-least-once 或 best-effort，禁止无 provider contract 时宣称 exactly-once。
- `worker done` 不得直接转换为外部 delivery succeeded；provider response 丢失、超时或连接中断必须进入 `unknown`/fence 或可查询的 pending 状态。
- 所有 retry/reconcile/reaper 必须有 lease/fence、attempt 上限、backoff、stop condition、可观察 stop reason 和 operator escape hatch。

### 11.3 安全、隔离和供应链证据

- authentication、authorization、capability declaration、filesystem/network enforcement、credential injection、audit 和 workload identity 分开建模。
- EffectiveRuntimePlan 关联 worker/backend/env profile、config/skill hash 和 artifact/evidence reference；不能以 token、screening、audit 或配置 hash 冒充 OS isolation。
- 无法观察或验证的能力必须输出 `partial`/`unavailable`；Strict profile 才能 fail closed，compat profile 必须带迁移告警。

### 11.4 协议、数据和运维可逆性

- AEP 新字段增量兼容，并同步 Go SDK、三种示例 SDK、协议文档和双向测试；第一版 plan/effect 优先留在内部/管理面，避免过早扩大 wire contract。
- SQLite/PostgreSQL schema、条件状态迁移、重启、连接抖动、多实例和旧版本读写必须成对验证；不得将 prompt、metadata value、secret、raw provider request 或 raw worker error 写入 durable facts。
- 新事实层先以 read-only/shadow mode 发布，升级必须可暂停、可回退，repair 可按 effect/session fence；不得依赖人工直接改生产库恢复。

### 11.5 观测和容量预算

- 使用有限枚举作为 metrics 维度；`plan_hash`、provider reference、workspace 和详细原因只能进入 trace/log/audit 的脱敏关联字段。
- 每个 plan/effect/audit/snapshot 都要有存储保留、查询分页、payload 大小、并发 claim、重试队列和数据库连接预算。
- 完成定义必须包含用户可见结果和 SLO：plan divergence、unknown age、reconcile success、delivery loss/duplicate、定位耗时和 DB pool saturation，而不只是“类型和测试已通过”。

### 11.6 阶段否决规则

若提案无法同时给出 canonical owner、状态时序、外部 evidence、隐私边界、双数据库迁移、容量预算和回滚路径，则维持 `proposed`/`shadow`。通用抽象必须由至少两个已验证消费者驱动；单一垂直切片应先完成 observed/reconciled 闭环，再提取公共接口。
