---
title: "qm × HotPlex 正交架构审查与实施闸门"
type: research-review
status: final-evidence
date: 2026-08-04
references:
  - docs/research/2026-08-04-qm-hotplex-deep-research-report.md
  - docs/research/2026-08-04-qm-hotplex-second-order-calibration.md
  - docs/superpowers/specs/2026-08-04-runtime-operations-contract.md
  - https://github.com/yc-software/qm
---

# qm × HotPlex 正交架构审查与实施闸门

> 本文是架构反例、风险、ROI 和决策闸门的证据附件，不是产品路线或实现契约。最终决策以 `docs/v2/ROADMAP.md`、`docs/v2/ARCHITECTURE.md` 和 approved specs 为准。

## 1. 终版审查结论

终版结论不是增加平台层，而是冻结 HotPlex 2.0 的架构闸门：

1. **产品边界先于能力边界**：HotPlex 仍是 self-hosted Agent Runtime Gateway，不因吸收 qm 的 scope、skill、run、delivery 机制而变成 workflow engine、marketplace、memory service 或 SaaS control plane。
2. **事实所有权必须唯一**：authority/scope、desired plan、durable execution、Gateway-owned effect、worker/provider observed state 和 reconciliation 是不同事实；每类事实只能有一个 canonical owner，其他模块只能投影。
3. **可靠性承诺必须以可证明的外部事实为准**：`accepted`、`worker done`、`provider response`、`externally verified` 不是同义词；网络断开、进程崩溃和迟到响应必须保留 `unknown`/fence 语义。
4. **安全声明必须可观测、可验证、可降级**：token、audit、screening、env allowlist、sandbox 和 workload identity 分属不同安全层，不能把其中一层的存在写成另一层的保证。
5. **协议、迁移和运维必须能演进和回退**：AEP 字段新增要兼容，SQLite/PostgreSQL 迁移成对且可恢复，repairer/reaper 具备 stop condition 和 operator escape hatch；不能以“最终一致”掩盖无法回滚的状态机。
6. **ROI 计算必须把长期运维成本计入**：优先交付能减少重复诊断、丢失副作用和错误配置的最小闭环，暂不支付通用调度器、技能市场或跨主机身份体系的维护成本。

路线确定为：统一 plan/preflight、observed bootstrap 和一条 durable effect 垂直切片优先；通用 EffectLedger、Cockpit 和更高层编排受闭环证据约束。

## 2. 证据边界与反事实方法

本审查把证据分成四层：

| 证据层 | 可以证明什么 | 不能证明什么 |
| --- | --- | --- |
| 源码/类型/状态机 | 设计了什么、状态如何迁移、谁调用谁 | 生产环境一定按设计运行 |
| 单元/竞态/故障测试 | 某种输入和故障模型下行为成立 | 未覆盖路径不存在风险 |
| 运行时观测 | 某个实例在某时刻做了什么 | 其他部署和未来版本都成立 |
| 用户可验证结果 | 外部副作用或体验是否发生 | 内部实现一定正确 |

qm 的 #165/#137（声明的 sandbox/backend 与实际运行环境不一致）、#44（delivery 先 ack 后发送失败）、#57（fire-and-forget rejection）、#136/#138（连接池与查询造成系统性故障）、#153（identity 与 directory 投影分裂）共同说明：**静态检查通过、内部函数返回成功、任务进入队列，都不足以证明用户真正得到可靠结果**。

HotPlex 当前已有 `internal/execution` 的 owner lease、runtime status、ambiguity fence 和 late convergence，也已有 Cron 的进程内 retry 路径；架构边界确定为：复用 execution state，并将其与 Gateway-owned effect、外部 evidence 和 reconcile 明确分开。受影响核心模块至少包括 `internal/agentspec`、`internal/worker/base`、`internal/execution`、`internal/cron`、`internal/audit`、`internal/session/sql` 和 `cmd/hotplex/doctor`。

## 3. 十一个正交审查维度

### D1. 产品与问题边界

**不变量**：每个新能力都必须直接改善“受控、可解释、可恢复地运行 Agent”之一，并能说明它属于 Gateway runtime，而不是独立产品。

**方向性风险**：把 qm 的 scope/skill/run/delivery 面全部搬入后，HotPlex 形成第二套 workspace、目录、调度、技能发布和 memory 体系；短期看能力变多，长期却失去 runtime gateway 的聚焦。

**闸门**：若提案需要独立 registry、独立 memory store、跨 session scheduler、复杂 workflow DSL 或 marketplace 才能成立，先拒绝或移入 Phase 4；只有真实用户场景证明单 Agent + tool/context 无法满足时，才升级路线。

### D2. 事实所有权与系统边界

**不变量**：每种事实只有一个写入者，投影可以多处存在但必须带来源和版本。

| 事实 | Canonical owner | 允许的消费者 |
| --- | --- | --- |
| user/workspace/bot ownership | 现有 identity/session/config 边界 | Gateway、adapter、audit |
| resolved desired runtime | EffectiveRuntimePlan resolver | WS、REST、doctor、worker start、admin read、recipe dry-run |
| input accepted/lease/runtime | `internal/execution` | Gateway、forwarder、repairer、runtime events |
| Gateway-owned external effect | EffectLedger（第一条垂直切片先落 Cron/delivery） | connector、retry、reconcile、Cockpit |
| worker/provider/channel observed state | 对应 adapter/provider evidence | plan status、audit、operator tooling |
| audit integrity | `internal/audit` | CLI、admin、export、verification |

**闸门**：任何新 handler 不得直接写第二份 session/execution/identity 真相；如果必须缓存，缓存键必须包含 source version/plan hash，并定义失效和降级行为。

### D3. Desired、execution、effect、observed、reconciled 语义

**不变量**：状态名称必须说明“谁观察到什么”，不能只因函数返回 nil 就进入成功态。

```text
Authority/Scope
      ↓
DesiredRuntimePlan
      ↓
DurableExecution / GatewayEffect
      ↓
ObservedRuntimeState
      ↓
Reconciliation / Repair / Fence / OperatorAction
```

**禁止的捷径**：

- `plan_hash == observed`；hash 只能证明期望计划相同。
- `worker done == external delivery succeeded`；Worker 完成只证明 worker turn 的终态。
- `HTTP 2xx == durable business effect`；只有 provider 的业务 receipt/查询或明确的 connector contract 才能提升 confidence。
- `timeout == failed`；超时时要区分 provider 未接收、已接收但响应丢失和状态可查询。

**闸门**：每个对外副作用都要有 `effect_id`、业务幂等键、attempt、provider reference、evidence/confidence、unknown 处理和 stop condition。无法提供外部确认的 connector 必须明确标为 best-effort，而不能伪装成 exactly-once。

### D4. 分布式一致性与故障模型

**不变量**：先声明交付语义，再选择 queue、lease、retry 和 fence；不以“最终一致”替代故障模型。

必须显式回答：

- DB 提交后进程崩溃，外部 effect 是否可能已经发生？
- claim owner 失联后，旧 owner 的迟到完成是否会覆盖新 attempt？
- provider 响应丢失后，能否查询状态？不能时谁负责 fence？
- 重启和多实例是否共享幂等事实？单进程 Set 只能防进程内并发，不能防跨重启重复。
- repair/reaper 何时停止，人工如何接管，如何防止无限重试？

**闸门**：所有涉及重试的状态迁移必须有 lease/fence、attempt 上限、退避、可观察 stop reason 和 fault-injection 测试；不得使用“最多一次”或“恰好一次”这种无法由 provider contract 支撑的营销式表述。

### D5. 身份、授权、能力与隔离

**不变量**：身份、授权、能力、隔离、审计分别建模，且每一项都有 declared/observed/enforced/partial/unavailable 的状态。

qm 的 capability token、scope resolution、command policy 和 sandbox layer 很值得借鉴，但它们解决的是不同层次的问题。NIST 的云原生零信任指导强调，应用和服务身份不能仅由网络位置或用户身份替代；这支持 HotPlex 将 worker/session/workspace identity 纳入策略，但不意味着 token 本身已实现隔离。

**闸门**：EffectiveRuntimePlan 必须能够解释：谁发起、在哪个 workspace、用哪个 worker、允许哪些 env/tool、文件系统和网络边界由谁执行、凭证如何注入、哪些能力只是兼容模式。若没有 OS/container/provider 证据，输出 `partial` 或 `unavailable`；Strict profile 才可 fail closed，compat profile 必须带显著迁移告警。

### D6. Worker artifact、配置和供应链

**不变量**：期望启动的 worker 必须与实际启动的 artifact、backend、env profile 和 capability evidence 可关联。

计划至少要预留：`worker_type`、命令/适配器版本标识、artifact digest（若可得）、backend、plan hash、skill/config hash、observed timestamp 和 evidence ref。不要把完整命令、secret、prompt 或原始 stderr 写入 durable facts。

**闸门**：如果 renderer、doctor、worker builder 对 backend/secret/profile 各自重新计算，则视为架构错误；必须先通过同一个 resolver。跨主机 workload identity（如 SPIFFE）只有在 HotPlex 真正需要跨节点信任和轮换时再引入，不能为本地单机 runtime 提前增加控制面。

### D7. AEP、SDK 与向后兼容

**不变量**：AEP 仍是唯一 wire contract；runtime metadata 和新事件增量兼容，旧客户端可忽略未知字段/事件并继续工作。

**闸门**：任何 plan/effect/observed 字段若要进入 AEP，必须同步 Go SDK、三种示例 SDK、`docs/reference/{aep-protocol,events}.md`、双向协议测试和未知字段测试。第一版 plan/effect 优先留在 Gateway/admin/doctor 内部事实层，只有语义稳定后才扩大 wire surface。

### D8. 数据模型、迁移与隐私

**不变量**：SQLite 与 PostgreSQL 成对迁移；状态更新条件包含 owner/run/fence 等必要版本；持久化输入只保留允许的指纹和引用。

qm #46 的启动 DDL 和非事务主键变更、#136 的连接池失效，以及 HotPlex 自身多方言迁移约束说明：schema 不是实现细节，而是可用性和可回滚性的一部分。

**闸门**：每个新表/字段必须有 forward/backward 兼容计划、旧版本读取行为、失败恢复、real PostgreSQL 多实例测试和 SQLite 测试；禁止启动时无界 DDL，禁止把 prompt、metadata 值、凭证、raw provider request 或原始 worker error 写入 execution/effect/audit。

### D9. 可观测性、证据链与基数

**不变量**：trace/log/audit/event 使用一致的 correlation keys；metrics 只使用低基数维度。

OpenTelemetry semantic conventions 的价值在于跨信号使用一致属性，而不是把所有业务字段都塞进 metric labels；其一般属性要求也明确高基数属性应谨慎 opt-in。HotPlex 应使用 `execution_id`、`effect_type`、`worker_type`、`outcome` 等有限枚举做指标，`plan_hash`、provider reference 和 workspace 只作为 trace/log/audit 的关联字段或脱敏摘要。

**闸门**：每个状态必须能回答“何时由谁基于哪条 evidence 写入”，且仪表盘查询有分页、时间范围和 payload 上限；Cockpit 不得通过 N+1、无界聚合或高基数 label 变成新的连接池故障面。

### D10. 性能、成本与容量

**不变量**：每个新事实层都有有界的查询、存储、索引、保留和重试成本。

必须在 spec 中写清：plan resolver 的缓存粒度和失效、skill/config hash 计算上限、effect/audit retention、重试队列容量、并发 claim 数、数据库连接预算、Cockpit 查询预算、单 workspace/session 的配额。

**闸门**：没有容量模型和压测/故障基线，不得引入全量快照、每次 turn 的大 JSON、每个 tool call 的高维指标或无限 audit retention；优先保存 hash、枚举、外部 reference 和有界 redacted reason。

### D11. 部署、升级、治理与可逆性

**不变量**：每次启用新事实层都能灰度、观察、暂停、回退，并有 operator escape hatch。

**闸门**：新 resolver/effect ledger 先 read-only 或 shadow mode；状态机和迁移先兼容旧版本；repairer 可停、可限流、可按 effect/session fence；升级失败不应要求人工直接改库才能恢复。所有“enforced/verified/reconciled”必须留下不可抵赖的 audit 证据和操作者身份。

## 4. 反例预演（Pre-mortem）

假设 HotPlex 2.0 在 12 个月后失败，最可能不是某个 handler 有 bug，而是以下系统性失误：

| 失败情景 | 早期信号 | 预防性决策 | 触发后的处置 |
| --- | --- | --- | --- |
| 变成“半个 qm + 半个 workflow engine” | issue/模块开始出现 registry、memory、DSL、marketplace；runtime SLO 没改善 | 产品边界闸门；Phase 3/4 需要真实场景证据 | 冻结新 surface，回收至 runtime vertical slice |
| 多份 resolver/状态机逐渐分叉 | doctor 说可用、worker 启动失败；WS/REST plan hash 不同 | single resolver + canonical owner | 关闭旁路 resolver，保留兼容适配器并记录差异 |
| 把 Worker 完成误报成外部成功 | Cron/消息显示 completed，但用户未收到；unknown 比例没有指标 | effect/observed/reconcile 分层 | 将未确认结果置 unknown/fence，停止盲重试并补查询/人工流程 |
| 隔离宣传超过真实能力 | compat 模式仍继承 host env；报告写 enforced 但无 OS evidence | capability honesty + fail-closed strict profile | 降级为 partial/unavailable，阻止高风险 profile |
| 事实层拖垮数据库和观测系统 | pool wait、N+1、cardinality、audit 表无界增长 | query/storage/metrics budgets | 限流、分页、采样、保留策略和应急开关 |
| 协议/迁移不可逆 | 新客户端依赖新字段；老节点无法读取；PG/SQLite 行为不同 | additive AEP + dual DB + rollback plan | 停止 rollout，兼容读写，迁移修复前不扩面 |
| repairer 变成副作用放大器 | attempt 数持续增长、重复消息、迟到旧 owner 改写新状态 | fence、attempt cap、stop condition、operator action | 全局或按 effect 停止 repair，进入人工核查 |
| ROI 被平台维护成本吞噬 | 代码量和运维告警增加，但定位时间/丢失率无改善 | 以可观测用户结果验收，而非抽象完成度 | 砍掉低 ROI 通用化，保留已验证的垂直切片 |

## 5. 借鉴决策矩阵

| 机制/外部参照 | 借鉴 | 不直接复制 | 当前决策 |
| --- | --- | --- | --- |
| qm scope/resolution | 层级作用域、显式 precedence、redacted resolution | qm 的完整 company surface、目录和 skill marketplace | 只做 capability inventory/read-only projection |
| qm run/lease/reaper | claim、heartbeat、lease expiry、reap、cancel | 把单进程幂等 Set 当成跨重启保证 | 复用 HotPlex execution，补 effect/observed 边界 |
| qm delivery store | durable idempotency key、claim expiry、ACK 状态 | ack-before-provider-effect 的业务顺序 | 以 Cron/delivery 做第一条垂直切片 |
| qm sandbox/config | layer/hash/materialize、配置派生统一 | 把静态 doctor 通过等同运行时 enforcement | 接入 EffectiveRuntimePlan + observed bootstrap |
| Kubernetes controller | desired/current state、controller/reconcile、status | 立即引入分布式 operator/control plane | 先在单机 Gateway 做有限 repair/reconcile |
| Temporal/durable execution | 事件历史、checkpoint、resume、durable failure boundary | 宣称 provider effect exactly-once | 只吸收事实和恢复语义，不替换现有 runtime |
| OpenTelemetry | 统一 semantic keys、跨信号关联 | 将 execution/plan/workspace 作为无限 metric labels | 低基数 metrics + trace/log/audit evidence |
| NIST Zero Trust | service/workload identity、policy per request | 用 token 或网络位置代替 enforcement | 明确身份/授权/隔离三者边界 |
| SPIFFE | 跨主机 workload attestation 的候选路径 | 在单机 HotPlex 里提前引入新身份控制面 | Deferred；只有跨节点信任成为硬需求才启动 |

## 6. ROI 重算与投资闸门

ROI 不按“可复用抽象数量”计分，而按四个可验证收益计算：减少错误配置导致的启动失败、减少重复外部副作用、缩短定位时间、降低未来迁移成本。初始估算仍采用 1–10 的收益/成本综合分，并要求每个切片在 1 个 release cycle 内产生观测证据。

| 切片 | 直接收益 | 新增复杂度 | 校准 ROI | 投资闸门 |
| --- | --- | --- | --- | --- |
| EffectiveRuntimePlan + preflight | 消除 WS/REST/doctor/worker 分叉，提前发现 fail-closed 配置错误 | resolver、canonicalization、redaction、兼容 profile | 10 | 等价输入 plan hash 一致；至少覆盖 4 worker |
| Worker observed bootstrap | 区分“计划声明”和“实际启动/能力” | artifact/evidence capture、adapter 适配 | 9 | 能在无 evidence 时输出 unknown，不得推断 enforced |
| Cron/Webhook/delivery durable effect | 降低重启、超时、迟到 ack 的丢失/重复 | dual DB、claim、reconcile、provider contract | 10 | 重启/多实例/响应丢失 fault-injection 通过 |
| 通用 Gateway EffectLedger | 统一外部副作用语义，减少后续重复实现 | 状态机、迁移、repair/operator tooling | 8 | 先完成一条垂直切片且无重复状态源 |
| Capability inventory + isolation report | 降低安全误宣称，提升诊断和治理 | precedence、hash、capability vocabulary | 7 | declared/observed/enforced 三者可区分 |
| Cockpit redacted snapshot | 缩短运营定位时间 | 查询预算、分页、权限、快照 | 7 | 不能产生 N+1/pool/cardinality 回归 |
| 跨 session 分布式 scheduler | 未来调度能力 | leader/partition/backpressure/migration/ops | 2 | 至少两个真实场景且单机边界已稳定 |
| Skill registry/marketplace/memory service | 产品扩展 | supply chain、权限、存储、运营 | 1–3 | 当前路线拒绝，另立产品决策审查 |

**投资规则**：任何新切片若无法同时写出 canonical owner、failure semantics、evidence、rollback 和容量预算，则暂不开发；任何“通用化”必须由至少两个已验证消费者驱动，而不是预先抽象。

## 7. 修订后的实施波次与完成定义

### Wave 0：架构事实和边界冻结

- 固化产品非目标、canonical owner、状态词汇和 capability honesty。
- 给 #946/#947 和现有 #847–#852 增加正交闸门：single resolver、observed evidence、unknown/fence、双数据库、AEP compatibility、privacy、capacity。
- 只允许 read-only/shadow mode 的新 plan/effect projection。

### Wave 1：Plan + observed bootstrap

- `AgentSpec.Resolve` 成为 WS/REST/doctor/worker start 的共同入口。
- BuildEnv/isolation report 输出 declared/observed/enforced/partial/unavailable。
- 计划事实不写 prompt/secret/raw error；plan hash 只做关联键。
- 四种 worker adapter 均给出最小 capability evidence 或明确 unavailable。

### Wave 2：一条外部副作用的闭环

- 选择 Cron/Webhook/消息 delivery 之一，定义 provider contract、effect id、idempotency、claim、ack、unknown 和 reconcile。
- 完成 SQLite/PostgreSQL 成对迁移、重启/多实例/连接抖动/响应丢失 fault-injection。
- 只有 observed/reconciled 证据稳定后，才提取通用 EffectLedger。

### Wave 3：Repair、Cockpit 与运维治理

- repair/reaper 带 lease、attempt cap、backoff、stop condition 和 operator escape hatch。
- Cockpit 只读 redacted snapshot，具备查询预算和低基数指标。
- 以 SLO 验收：plan divergence、unknown age、reconcile success、delivery loss/duplicate、定位耗时和 DB pool saturation。

### Wave 4：条件式平台化

- 只有 Wave 1–3 的运行时 SLO、容量和迁移证据稳定，才评估跨 session scheduler、workload identity、workflow 或 registry。
- 每个候选方向必须说明为何现有 Gateway/Session/Execution/AEP/事件历史不足，并给出独立产品 ownership。

## 8. 最终不可犯错清单

在代码合并、schema 发布或扩大 rollout 前，必须能对以下问题给出证据，而不是口头保证：

- 这还是 HotPlex Runtime Gateway 吗？如果不是，是否已另立产品决策？
- 哪个模块拥有这条事实？其他模块如何投影、失效和追溯？
- 这是 desired、attempted、observed、verified 还是 reconciled？状态迁移谁授权？
- 如果进程在 DB 提交前后、provider 接收前后、响应丢失时崩溃，系统分别怎么做？
- 是否可能重复外部副作用？不能避免时如何标记 unknown/fence 并让人工接管？
- 安全能力是 declared、observed 还是 enforced？证据来自哪里？
- AEP、SDK、SQLite、PostgreSQL、旧版本实例和 Windows/macOS/Linux 是否兼容？
- 事件、审计、指标是否会泄露敏感信息或制造高基数/无界存储？
- 失败时能否暂停 repair、回滚 rollout、恢复旧读写路径，而不直接手工改生产库？
- 本切片在一个 release cycle 内减少了什么用户可见损失，新增了多少长期维护成本？

只要其中任一项没有明确答案，状态应为 `proposed`/`shadow`，而不是 `enforced`/`completed`。

## 9. 参考资料与本地证据

- qm 源码与 issues：https://github.com/yc-software/qm
- HotPlex 当前运行时与路线图：`docs/v2/ROADMAP.md`、`docs/v2/IMPLEMENTATION-ROADMAP.md`
- HotPlex 事实闭环 spec：`docs/superpowers/specs/2026-08-04-runtime-operations-contract.md`
- Kubernetes controller：https://kubernetes.io/docs/concepts/architecture/controller/
- Temporal durable execution：https://docs.temporal.io/
- OpenTelemetry semantic conventions：https://opentelemetry.io/docs/specs/semconv/
- OpenTelemetry 属性基数要求：https://opentelemetry.io/docs/specs/semconv/general/attribute-requirement-level/
- NIST Zero Trust Architecture：https://csrc.nist.gov/pubs/sp/800/207/a/final
- SPIFFE Workload API：https://spiffe.io/docs/latest/spiffe-specs/spiffe_workload_api/
