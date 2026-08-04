---
title: "qm 对 HotPlex 的二阶校准：从 Runtime Plan 到事实闭环"
date: 2026-08-04
status: final
references:
  - docs/research/2026-08-04-qm-hotplex-deep-research-report.md
  - docs/superpowers/specs/2026-08-04-qm-inspired-runtime-operations-design.md
  - docs/v2/ROADMAP.md
  - https://github.com/yc-software/qm
---

# qm 对 HotPlex 的二阶校准：从 Runtime Plan 到事实闭环

## 结论先行

结论为“EffectiveRuntimePlan + Gateway-owned EffectLedger + Capability Inventory”，并由下面的运行时闭环约束其实现：

```text
Authority / Scope / Capability
            ↓
Desired Runtime Plan
            ↓
Durable Execution / Effect
            ↓
Observed Provider / Worker / Delivery State
            ↓
Reconciliation / Repair / Fence / Operator Action
            └───────────────反馈到下一次 plan 与执行
```

核心校准是：

- plan hash 不是部署成功证明；
- Worker `done` 不是外部副作用成功证明；
- `idempotency` 不是跨实例 exactly-once 证明；
- capability token、audit 或 screening 不是强隔离证明；
- `unknown` 不是失败，必须成为待调和事实；
- 运行时必须能够解释“期望是什么、实际发生了什么、证据在哪里、采取何种安全动作”。

## 1. 证据范围

证据范围包括：

| 维度 | 取证 |
|---|---|
| qm 上游 | main 当前仍为 `eabe3d7`；仓库约 9.5k stars、1k forks、113 个 open issues；重点检查 #44、#46、#54、#55、#57、#125、#130、#136、#137、#138、#153、#165 |
| qm 实现 | idempotency、delivery、Postgres pool/schema、sandbox validation、config load、skill visibility、Git credential broker |
| HotPlex 实现 | AgentSpec Resolver、doctor、BuildEnv、execution runtime transitions、Cron delivery/retry、bridge Done convergence |
| 图谱关系 | `AgentSpec.Resolve` 的 Gateway 调用链；`BuildEnv` 的四类 Worker 调用链；`finishRuntimeOnDone` 到 execution/replay/event/repair 的调用链 |
| 动态验证 | `go test -race -count=1 ./internal/agentspec ./internal/execution ./internal/cron ./internal/worker/base`：431 tests passed |
| 外部设计参照 | Kubernetes controller 的 desired/current/reconciliation 模型、Temporal durable execution、OpenTelemetry semantic conventions、SPIFFE workload attestation |

README 和设计文档继续只作为意图证据；结论优先采用源码、测试、部署失败和 issue 讨论。

## 2. qm 失败样本揭示的更深层规律

### 2.1 “已校验”不等于“已应用”

qm #165/#130 暴露 renderer 派生的 `SANDBOX_BACKEND=sprites` 与 secret predicate 读取原始 config 的分裂；#137 又显示 sandbox layer、tools、secretEnv 即使被 `qm up`/`doctor` 报告为成功，也可能没有进入实际运行的 Sprites。对应实现中，`cli/src/config.ts:1242-1317` 负责静态 sandbox 配置检查，`cli/src/sandbox-layer.ts:790-903` 负责本地 layer 内容检查，而 `src/config.ts:553-855` 在运行时再次根据环境做最终选择。

这说明 EffectiveRuntimePlan 需要至少包含两类字段：

- **desired**：解析后的 backend、image/layer digest、env key 集合、policy、capability 和 source refs；
- **observed**：实际启动的 worker/backend、应用的 layer digest、实际注入的 profile、provider/channel 返回的 acknowledgement。

只有 desired 与 observed 都存在，才能计算 `reconciled`、`drifted` 或 `unknown`。HotPlex 的第一版 plan 不应只 hash config；应预留 `ObservedRuntimeRef`，否则 #946 可能把 qm 的同类问题换一种形式重演。

### 2.2 “执行完成”不等于“交付完成”

qm #44 指出恢复 delivery 先 ack、后 `chat.postMessage`，发送失败后 durable copy 已被 tombstone；#57 又指出 Postgres polling rejection 若落在 fire-and-forget promise 外，会变成 unhandled rejection。qm 的 delivery store（`src/delivery/postgres-delivery-store.ts:24-217`）已经有 unique idempotency key、claim expiry、pending/ack，但业务层的 ack 顺序仍决定事实是否可信。

HotPlex 当前 `internal/execution` 已把 input delivery status 与 Worker runtime status 分离；`FinishRuntime` 支持 `unknown → completed/failed` 的晚到收敛，`finishRuntimeOnDone` 在 durable write 失败时不向客户端发终态事件并进入 repairer。这是正确基础，但 Cron delivery 目前仍有明显缺口：`internal/cron/delivery.go:62-265` 使用进程内 retry queue，队列满时淘汰最旧项，关闭时把未发项记录为永久丢失。

因此 #947 的第一落点不应是抽象出一个漂亮的通用 ledger，而应先把 Cron/Webhook/消息发送的“发起、提供商接受、可查询确认、未知、重试/人工调和”做成 durable effect；先解决事实丢失，再抽象通用接口。

### 2.3 “once”不等于跨实例幂等

qm `src/idempotency/idempotency-store.ts:22-78` 的 `inflight` 是单进程 `Set`，只有 `backing` 持久化 committed record 时才具备跨重启记忆；无 backing 时只能防同一进程内的并发重复。Postgres delivery store 的 unique key 和 `FOR UPDATE SKIP LOCKED` 提供了更强的跨实例基础，但仍需要正确的 ack、外部确认和故障恢复顺序。

HotPlex 的 EffectLedger 必须将以下概念分开：

1. `claim`：哪个 Gateway 实例暂时拥有尝试权；
2. `attempt`：尝试次数与执行时间；
3. `effect fact`：外部结果已知为 succeeded/failed/unknown；
4. `idempotency key`：判断是否是同一个业务效果；
5. `reconcile evidence`：外部查询、回调、人工确认或不可验证原因。

仅增加 `idempotency_key UNIQUE` 不能满足这些语义。

### 2.4 数据库本身也是控制面的一部分

qm #46 暴露了启动时重复 DDL、非事务 DROP/ADD 主键和蓝绿实例并发写入的风险；当前 `src/persistence/pg-pool.ts:42-55` 通过 advisory lock 串行执行 DDL，但语句仍逐条执行。#136 显示连接池失效后核心服务整体 500 且只能重启恢复；#138 显示 skills 的 N+1 查询可以耗尽连接池并拖垮健康检查。

对 HotPlex 的含义：

- plan/preflight 不应每次请求都重新读取整个配置/能力目录；应使用有界、可缓存、带版本的 snapshot；
- Cockpit 查询不得把所有 shadowed skills、所有 attempts 或所有 event payload 一次性拉入内存；
- migration、connection health、repairer、reaper 都属于 Runtime Control Plane，而不是外围运维脚本；
- SQLite/PostgreSQL 的成对迁移只是最低要求，还必须验证多实例 startup、连接抖动、迁移中断和 pool 恢复。

### 2.5 Scope 必须是 canonical ownership，不是某个 surface 的目录副本

qm #153 显示 OIDC/portal principal 已进入 identity store，但没有写入 Slack 驱动的 directory store，导致 Slack-less 部署无法添加成员。这个问题不是“目录 API 小 bug”，而是同一个主体被不同 surface 各自投影，写路径没有 canonical owner。

HotPlex 的 AgentIdentity、workspace owner、bot/platform config、session owner 和 AEP `OwnerID` 应继续保持一条可追溯链：

```text
principal → workspace/bot scope → session → execution → worker run → effect/delivery
```

任何 adapter-specific member map、surface-specific permission cache 或只在 WebChat/Feishu/Slack 中存在的身份事实，都只能是投影，不能成为授权事实源。

### 2.6 “边界宣称”必须落到 parser、attestation 和负向测试

qm #55/#125 反复出现 URL origin、IPv6 authority、Git upstream path traversal、secret-drop browser auth 等边界问题。`parseBrokerPath`（`src/api/git-http-broker.ts:46-61`）只检查 NUL 和空路径，`gitUrlFor`（`:115-119`）直接将 upstream path 拼入 URL；这些问题说明“有一个 auth/policy 函数”不等于边界被正确约束。

HotPlex 的 isolation/capability report 应把 `enforced/partial/unavailable` 与负向测试绑定：

- URL 必须用类型化 parser，并拒绝 userinfo/path/query/fragment 等不符合 origin contract 的输入；
- path 必须 canonicalize 后再做 allowlist 判断；
- IPv4/IPv6、Windows path、UNC path、符号链接、NUL、编码绕过都需要拒绝测试；
- capability 只有在运行时实际观测或平台 attestation 成功后，才能从 `declared` 升级为 `enforced`；
- `audit` 记录“已检查/已拒绝/已使用”，不能记录“因此模型无法绕过”。

## 3. 五层运行时模型

### L0 Authority

输入是 actor、principal、workspace、platform/bot、session、worker identity、scope version 和 capability grant。L0 解决“谁可以对哪个对象做什么”，不承载执行结果。

### L1 Desired Runtime Plan

由 AgentSpec、config precedence、workspace/session metadata 和实际可用 Worker registry 解析出 canonical redacted plan。plan 应包含：

- `plan_hash`、`resolver_version`、source refs；
- worker/provider、permission、budget、timeout、env profile；
- workspace/sandbox desired state、layer/image digest；
- capability IDs、skill/config hash、policy summary；
- warnings、blocked reasons、compatibility mode。

plan hash 适合放在 event/audit/log correlation 中，不应直接成为 metrics label，以免制造高基数指标。

### L2 Durable Execution / Effect

L2 记录 intent、claim、attempt、lease、worker run、effect key、delivery state 和状态迁移。它回答“Gateway 是否已经拥有并尝试了这次动作”，不回答外部系统是否真正接受。

### L3 Observed State

L3 记录外部可观察事实：Worker 实际启动类型、sandbox/backend/layer digest、provider response/receipt、message delivery acknowledgement、外部查询结果、连接状态和 source timestamp。证据不足时必须是 `unknown`，而不是由执行函数自行推断 succeeded。

### L4 Reconciliation

L4 是 repairer/reaper/reconciler/operator 的闭环：根据 desired 与 observed 的差异进行重试、查询、补偿、fence、人工确认或放弃，并把新的事实写回 eventstore/audit/effect ledger。调和动作必须有 owner lease、attempt 上限和明确 stop condition。

这个模型与 Kubernetes controller 的 desired/current/reconciliation 思路一致，但 HotPlex 仍应保持 Gateway 内核边界，不直接引入 Kubernetes operator；与 Temporal 的 durable history 思路相似，但 HotPlex 只为 runtime/effect 事实提供最小持久化，不把 Worker prompt 或模型上下文变成新的通用 workflow history。

## 4. 对 HotPlex 现状的反证结果

| 假设 | 证据 | 结论 |
|---|---|---|
| AgentSpec 已经是所有运行面的统一 resolver | `internal/agentspec.Resolve` 的调用链主要集中在 Gateway session create/start；`cmd/hotplex/doctor.go:18-71` 仍独立运行 checker；`BuildEnv` 仅由各 Worker 启动链调用 | **不成立**。应把 resolver 接入 doctor/preflight/worker start，而非再加一个 plan formatter |
| execution ledger 已足够承载 EffectLedger | execution 已有 owner lease、runtime unknown、late Done convergence；Cron delivery 仍是进程内 queue，消息 delivery 与外部 ack 也有独立路径 | **部分成立**。应复用 execution identity/lease/fence，但新增 effect-level evidence 与 durable delivery 状态 |
| isolation profile 只需 allowlist | `BuildEnv` 先继承 `os.Environ()` 再套 blocklist，并支持 `HOTPLEX_WORKER_*` 注入；这能减小泄漏面，但不能证明完整隔离 | **不成立**。必须同时报告 compat/allowlist 与 filesystem/network enforcement |
| Cockpit 主要是 UI 工作 | qm #138 说明查询本身可拖垮 DB pool；HotPlex Cockpit 若无 bounded query、snapshot 和 redacted facts，会变成新的故障面 | **不成立**。Cockpit 必须消费既有 facts，并带分页、限流、快照版本和查询预算 |
| queue 可以先做，再补副作用可靠性 | qm #44 的 ack 顺序、#57 的异步异常和 HotPlex Cron 的内存 retry queue 都说明 queue 与 effect outcome 不能分开设计 | **不成立**。先定义 effect outcome/unknown/reconcile，再扩展 queue semantics |

## 5. 修正后的演进路线

### Wave 0：事实词汇与边界测试

- 固定 `plan / execution / effect / observed / reconcile / fence` 的语义和状态迁移表；
- 为 `plan_hash`、`execution_id`、`worker_run_id`、`effect_id`、`attempt`、`evidence_ref` 建立低基数 telemetry 规范；
- 为 origin/path/env/capability 增加负向测试语料；
- 明确每个事实允许保存什么，继续禁止 prompt、metadata 值、凭证和原始 Worker 错误进入持久化。

### Wave 1：EffectiveRuntimePlan + observed bootstrap

- #946 先完成 canonical resolver、doctor/admin dry-run、plan hash、redaction；
- worker start 返回实际 worker type、env profile 和可验证的 sandbox/backend facts；
- plan 以 `desired` 和 `observed` 分栏，未经观测不宣称 `enforced`；
- #867 的 compat mode 必须保留迁移告警，并逐步切换 allowlist。

### Wave 2：以 Cron delivery 为第一条 EffectLedger 垂直切片

- #947 先接 Cron/Webhook/消息 delivery，不先接所有 worker-private tools；
- durable outbox/claim/attempt/ack/unknown/reconcile/fence 闭环；
- 重启、多实例、provider timeout、provider 5xx、成功但响应丢失、人工查询确认均有测试；
- 旧的进程内 retry queue 只能作为兼容 fallback，不能作为最终事实源。

### Wave 3：Repair Controller 与 Cockpit

- Repairer/reaper 只消费有界、可租约的 facts；
- Cockpit 展示 desired/observed/drift/unknown/evidence，不展示 prompt/secret/raw tool args；
- 查询使用 snapshot、分页、时间窗和查询预算，避免 N+1 或 pool starvation；
- #851 的 queue ordering/attempt/timeout/retry reason 接入 EffectLedger，而不是创建第二套状态机。

### Wave 4：Capability governance 与外部 workload identity

- Capability Inventory/materialization 先只读、可 hash、可审计；
- 只有当实际部署需要跨主机/跨集群身份时，才评估 SPIFFE/SPIRE 或等价 workload attestation；
- 不因为出现 capability token 就提前承诺强隔离、非干扰性或 marketplace。

## 6. ROI 二次修正

单项 ROI 评分不能独立代表交付收益；统一按垂直闭环重算：

| 垂直切片 | 单项价值 | 关键前置 | 风险 | 修正 ROI |
|---|---:|---|---:|---:|
| Plan + preflight + worker observed bootstrap | 10 | AgentSpec/BuildEnv/doctor 收敛 | 中 | **22** |
| Cron delivery durable effect + external acknowledgement | 10 | execution identity/lease/fence | 中高 | **20** |
| EffectLedger 通用抽象 | 8 | 至少一条真实 delivery 垂直闭环 | 高 | **12** |
| Capability inventory/materialization | 7 | plan hash、scope precedence、safe path | 中 | **8** |
| Cockpit facts view | 8 | bounded query/snapshot/redaction | 中 | **10** |
| 分布式 scheduler / workflow engine | 5 | 多个真实跨 session 工作流 | 极高 | **2** |

修正后的投资原则：不要先建设抽象，再寻找事实；先选择一条真实副作用路径完成 `desired → effect → observed → reconcile`，再提取跨场景接口。首条路径推荐 Cron delivery，因为 HotPlex 已有 Cron、retry、metrics 和测试，可用较小范围暴露真实边界。

## 7. 完成标准

完成标准不是新增字段或接口数量，而是证明：

1. 一次 Cron delivery 可以在 Gateway 重启、多实例竞争、provider timeout 和晚到响应后得到唯一、可解释的最终事实或明确 `unknown`；
2. `hotplex doctor`、WS/REST session start 和 worker process 使用同一份 plan hash；
3. sandbox/env capability 的 `declared`、`observed`、`enforced` 不再混用；
4. Cockpit 不会通过 N+1、无界 payload 或高基数 telemetry 放大故障；
5. 所有新状态都有 SQLite/PostgreSQL 对应迁移、repair/fence 语义、race/fault-injection 测试和脱敏检查。
