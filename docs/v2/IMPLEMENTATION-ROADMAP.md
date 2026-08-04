# HOTPLEX 2.0 Implementation Roadmap

## 总原则

HotPlex 2.0 是一次运行时契约收敛，不是大重构。

```text
现有 Gateway + Session + Worker + AEP
        |
        v
Runtime Contract 2.0
        |
        v
Runtime Control Plane
        |
        v
Agent Platform / Agent OS
```

所有实现必须遵守：

1. 扩展 Worker abstraction，不替换。
2. 扩展 Session model，不绕过。
3. 扩展 AEP events，不引入平行 event bus。
4. 复用现有 auth、workspace、audit、observability。
5. 每个 issue 独立可测、可回滚。

## 产品迭代原则

2.0 版本以 **Runtime Gateway 稳定化** 为主线：

1. 先提升“可解释性”：让每次 agent 执行能被 trace、audit、eventstore 关联。
2. 再提升“可控性”：让输入顺序、权限边界、失败恢复可预测。
3. 最后提升“可编排性”：只有当单 agent runtime 稳定后，再做 workflow、多 agent、registry。

每个阶段都必须有可观察的用户价值，不接受只有架构概念、没有运行收益的重构。

## 依赖图

```text
#847 AgentSpec
   |
   +--> #848 Agent Identity
   |        |
   |        +--> #849 Runtime AEP Events
   |        |        |
   |        |        +--> #850 Runtime Observability
   |        |
   |        +--> #852 Runtime Context
   |
   +--> #851 Execution Queue
```

推荐顺序：

1. #847：先定义标准化配置模型，避免后续 issue 各自发明字段。
2. #848：把 identity 固化到 session/runtime metadata。
3. #849：把 identity/execution/context 变成可观测 AEP 事件。
4. #850：基于事件和 metadata 建 trace/metrics。
5. #851：引入 queue，但保持输入输出语义不变。
6. #852：抽象 context 读取/保存，复用 eventstore 和 worker history。

## 高 ROI 交付切片

2.0 的第一阶段应优先做“小而可验证”的运行时契约切片，而不是直接实现 Agent OS 大组件。

| 优先级 | 切片 | 对应 issue | ROI | 说明 |
| --- | --- | --- | --- | --- |
| 1 | AgentSpec 只读归一化器 | #847 | 9/10 | 先把 WS init 和 REST create session 的 worker/workspace/work_dir 解析收敛成 normalized view，不改变现有行为 |
| 2 | Runtime correlation metadata | #849/#850 | 9/10 | 在 AEP metadata 中标准化 `trace_id`、`agent_id`、`execution_id`、`workspace_id`、`worker_type`，直接提升诊断能力 |
| 3 | AgentIdentity 先落 `context_json` | #848 | 8/10 | 先证明 identity 能贯穿 session/event/audit/trace，避免过早新增 DB 列 |
| 4 | Execution ID before ExecutionQueue | #849/#851 | 8/10 | 先给每次 input 分配 `execution_id`，贯穿事件和审计，再实现队列 |
| 5 | RuntimeContext 只读 facade | #852 | 7/10 | 从 eventstore/turns/worker_session_id 读取事实，不建设独立 memory service |
| 6 | Per-session input gate | #851 | 7/10 | 先约束同 session 输入串行，验证后再扩展完整 ExecutionQueue |

### qm 终态校准切片

| 优先级 | 切片 | 对应 issue/spec | ROI | 说明 |
| --- | --- | --- | --- | --- |
| 0 | EffectiveRuntimePlan + fail-closed preflight | 新 issue + `2026-08-04-qm-inspired-runtime-operations-design.md` | 10/10 | 复用 #847/#867/doctor；统一 WS/REST/worker/admin/recipe 的 resolved state |
| 1 | Explicit env allowlist + isolation report | #867 | 9/10 | 先建立 compat/allowlist profile 与可证明能力报告，不声称 OS isolation |
| 2 | Worker observed bootstrap | #946/#867 | 9/10 | plan 必须区分 desired/observed/enforced，不能用配置 hash 冒充 sandbox/backend 已应用 |
| 3 | Cron delivery durable effect vertical slice | #947 + Cron | 10/10 | 先解决重启、多实例、超时和晚到 ack，再提取通用 EffectLedger |
| 4 | Gateway-owned EffectLedger | #947，协同 #851/#870 | 8/10 | 只覆盖 Gateway-owned delivery/webhook/cron/control/recipe；unknown 必须 reconcile/fence |
| 5 | Capability inventory/hash/precedence | `QM-Scope-Capability-Model-Spec.md` | 7/10 | 只读优先；先解释配置/skills 投影，再讨论 promotion |

明确暂缓方向：

- 分布式 scheduler。
- 独立 memory service。
- Agent registry/marketplace。
- 复杂策略语言。
- 多 Agent workflow 编排。

qm 研究确认的明确暂缓项：

- 复制 qm 的 Web app publishing、Slack-first company surface 或 private-fork 运营模型；
- worker 私有 tool protocol 搬运到 Gateway；
- skill marketplace、remote registry、独立 memory service；
- 在 EffectiveRuntimePlan 和 EffectLedger 未稳定前建设跨 session 分布式 scheduler。

### 终版正交架构闸门

上述优先级同时受以下架构闸门约束：

1. **唯一事实源**：先在设计和测试中标明 authority/scope、desired plan、execution、Gateway effect、observed state 和 reconciliation 的 canonical owner；不得由 handler、adapter、doctor 各自维护状态机。
2. **故障语义先行**：在写 retry/queue 代码前，先定义 provider 已接收但响应丢失、进程在提交前后崩溃、lease 过期、旧 owner 迟到完成和人工接管的行为；没有 `unknown`/fence/stop condition 的实现不进入生产路径。
3. **安全证据分层**：认证、授权、capability declaration、filesystem/network enforcement、credential injection 和 audit 必须分开报告；没有运行时证据的能力只能标为 `partial`/`unavailable`。
4. **兼容与可逆**：AEP 增量兼容；SQLite/PostgreSQL 迁移和条件更新成对；新事实层先 read-only/shadow，具备旧版本读写、暂停 repair、回退和 operator escape hatch。
5. **容量预算**：为 plan/effect/audit/Cockpit 写清保留期、查询分页、payload 上限、重试队列和连接池预算；`plan_hash`/workspace/provider ref 不直接进入无界 metrics label。
6. **通用化门槛**：至少一个真实垂直切片完成 `desired → effect → observed → reconcile` 闭环，并由两个消费者复用后，才提取通用 EffectLedger 或扩大到 scheduler/recipe。

## 产品迭代波次

### Wave 0: Roadmap Contract Hardening

目标：把 2.0 从“Agent OS 愿景”收敛为可执行 Runtime Gateway 路线。

交付：

- 2.0 文档明确产品定位、非定位、first cuts、暂缓清单。
- #847-#852 每个 issue 都具备 first cut 和验收标准。
- 增加 EffectiveRuntimePlan、preflight、EffectLedger 的边界、非目标和数据保密规则。
- 增加 desired/execution/observed/reconciliation 四层事实闭环和 Cron 第一条垂直切片。
- 冻结产品非目标、canonical owner、状态词汇、能力诚实、隐私边界、容量预算和回滚闸门。
- 所有新增 plan/effect projection 默认只读或 shadow，不得先改变现有 dispatch 语义。

完成标准：

- 文档构建通过。
- GitHub issue 与文档可双向追踪。
- 每个待开发切片都能回答 owner、故障时序、evidence、迁移、容量和回滚六个问题。

### Wave 1: Runtime Correlation

目标：让一次 agent execution 可以被解释。

覆盖 issue：

- #847 AgentSpec read-only resolver。
- #848 AgentIdentity in `context_json`。
- #849 execution metadata + minimal runtime events。
- #850 trace/metrics semantic keys。

用户价值：

- 管理员可以定位“谁在什么 workspace 使用什么 worker 执行了哪次 input”。
- 开发者可以从一次错误追到 input、worker、tool、done/error。

完成标准：

- AEP metadata、audit detail、trace attributes 使用一致 key。
- `execution_id` 能关联 inbound input、runtime events、terminal done/error。
- 不改变旧客户端协议行为。

### Wave 2: Runtime Control

目标：让同一 session 的执行顺序和恢复边界可控。

覆盖 issue：

- #851 per-session input gate。
- #852 read-only RuntimeContext facade。

用户价值：

- 连续输入不会并发打乱 worker 状态。
- 崩溃/重启/恢复时能解释上下文来源和缺失边界。

完成标准：

- race tests 覆盖 input gate。
- RuntimeContext 从 eventstore、turns、worker_session_id、workspace metadata 读取事实。
- 不引入分布式 scheduler 或外部 memory backend。

补充顺序：#877 fence escape hatch 与 #867 isolation/preflight 先于完整 #851 queue；queue 的事实必须能被 EffectLedger/Execution Cockpit 消费。

二阶校准：先完成 `desired → effect → observed → reconcile`，再扩展 queue ordering/attempt/timeout/retry reason；否则 queue 会把 Worker 终态误当成外部副作用终态。

### Wave 3: Runtime Operations

目标：把 2.0 能力转成可运营界面和诊断 API。

候选交付：

- Admin runtime diagnostics：按 session/execution 查询状态、事件、trace、audit ref。
- Runtime health summary：worker readiness、queue/input gate state、context source。
- Policy decision visibility：先展示现有 permission/workspace/tool decisions，不发明策略语言。
- Effective runtime plan：展示脱敏 plan hash、来源、capability/enforcement 状态和 warnings。
- Effect reconciliation：展示 Gateway-owned effect 的 attempt、unknown、fence 和 reconcile ref。
- Observed state/drift：展示实际 worker/backend/artifact/provider evidence，与 desired plan 分栏显示。

启动条件：

- Wave 1/2 的 metadata 和 context 边界稳定。
- 至少一条 Cron/Webhook delivery 在重启、多实例、provider timeout 和晚到确认下可收敛。

### Wave 4: Agent Platform

目标：在 Runtime Gateway 被验证后再做平台扩展。

候选交付：

- Agent capability catalog。
- 多 Agent workflow。
- External memory adapters。
- Distributed scheduling。

启动条件：

- 至少一个真实业务场景证明单 Agent runtime + tools/context 无法满足。
- Workflow、memory、scheduler 的需求能复用 Wave 1/2 的 contract。

## #847 AgentSpec Runtime Model

目标：把分散在 config、init metadata、workspace、bot/platform 配置中的 agent runtime 选项标准化。

第一刀：

- 新增只读 `AgentSpec` normalized view 和 resolver。
- WS init 路径和 REST create session 路径共同使用 resolver。
- Resolver 输出继续映射到现有 `worker.SessionStartParams` / `worker.SessionInfo`。
- 不新增持久化字段，不改变 session key 派生结果。

实施范围：

- 新增 AgentSpec/PolicySpec/SandboxSpec/BudgetSpec 数据结构。
- 提供 normalization 函数：config + init + workspace -> AgentSpec。
- 映射到 `worker.SessionInfo`，不改变 worker interface。
- 在文档中说明与 `worker_type`、permission mode、allowed tools、sandbox、budget 的关系。

验收标准：

- 表驱动测试覆盖 4 个 worker type。
- 旧配置不声明 AgentSpec 时仍能启动 session。
- 未知 worker type 仍在边界被拒绝。
- `docs/reference/configuration.md` 同步更新。

## #848 Agent Identity Binding

目标：让 session、worker、AEP、audit、trace 使用一致的 agent identity。

第一刀：

- 定义 `AgentIdentity`，先作为 session `context_json` 的稳定子对象保存。
- AEP metadata、audit `detail_json`、trace attributes 使用同一套 identity key。
- 暂不为 identity 新增 sessions 表独立列，除非查询需求证明需要索引。

实施范围：

- 定义 AgentIdentity。
- SessionInfo/Session store 增加可空 identity metadata。
- Gateway init/resume 绑定 user、workspace、bot、platform、worker type。
- eventstore/audit 写入 agent identity 的最小字段。

验收标准：

- workspace owner mismatch 仍被拒绝。
- anonymous session 有明确 anonymous identity。
- 已有 session 迁移后可读取。
- session lifecycle 文档同步更新。

## #849 Runtime Observability Events

目标：扩展 AEP 成为 runtime 事件契约。

第一刀：

- 标准化 AEP metadata keys：`trace_id`、`span_id`、`agent_id`、`execution_id`、`workspace_id`、`worker_type`。
- 增加最小 runtime event：`runtime.execution.started`、`runtime.execution.completed`、`runtime.execution.failed`。
- 先从 Handler input delivery 和 Bridge done/error 路径发出事件，不改变现有 message/done/error 流。

实施范围：

- 新增 runtime/security/context event kind。
- 新增 typed Data struct。
- Gateway/Bridge 在关键节点发出 runtime event。
- eventstore 捕获 runtime event，旧客户端可忽略。

验收标准：

- AEP encode/decode/clone 测试通过。
- runtime event 不破坏现有 message/done/error 流。
- `docs/reference/aep-protocol.md` 和 `docs/reference/events.md` 同步更新。

## #850 Runtime Tracing And Metrics

目标：将现有 OTel/Prometheus 能力扩展到 Agent runtime 维度。

第一刀：

- 在现有 trace 基础上统一 runtime span attributes。
- 修正已实现但文档/注释未收敛的 trace metadata 说明。
- 先新增 `execution` 维度指标，不引入高基数 label。

实施范围：

- trace：init、session create/resume、worker start、execution dispatch、tool call、done/error。
- metrics：agent executions、queue latency、policy decisions、context load/save。
- metadata：向 AEP envelope 注入 trace id/span id。
- noop mode 保持无副作用。

验收标准：

- observability disabled 时 gateway 行为不变。
- metric 创建失败只 warn，不 panic。
- trace id 能从 runtime event/audit 中关联。
- `docs/reference/metrics.md` 同步更新。

## #851 Execution Queue Abstraction

目标：在 Session 和 Worker 之间增加第一版执行队列，为 scheduler 打基础。

第一刀：

- 先实现 per-session input gate，保证同一 session 内不会并发调用 `Worker.Input`。
- 使用 #849 的 `execution_id` 记录 input attempt 和完成状态。
- 不做跨 session 调度，不改变 worker 输出流。

实施范围：

- 定义 ExecutionQueue、RuntimeInput、ExecutionStatus。
- 单 session FIFO dispatch。
- 与 turn timeout、LLM retry、worker crash cleanup 对齐。
- queue state 暴露给 metrics/admin diagnostics。

验收标准：

- 同 session 并发输入不会造成 worker interleaving。
- retry/timeout/crash 保持现有行为。
- race 测试覆盖 enqueue/cancel/worker done。
- 不引入跨节点调度。

## #852 Runtime Context Persistence

目标：为 session recovery、summary、future memory backend 建立统一上下文接口。

第一刀：

- 先做只读 `RuntimeContext.Load` facade。
- 数据来源限定为 eventstore events、turns、sessions.worker_session_id、workspace metadata。
- 不新增 memory backend，不改变 turns 聚合语义。

实施范围：

- 定义 RuntimeContext、ContextSnapshot、ContextUpdate。
- 读取 eventstore events、turns、worker internal session id。
- 支持 provider-specific adapter，但接口不泄漏 provider 私有类型。
- 记录 context.loaded/context.saved runtime event。

验收标准：

- resume/fork/session history recovery 回归测试通过。
- context load/save 不改变现有 turn 聚合语义。
- 后续 memory backend 可以通过接口接入。

## 跨 issue Traceability

| 需求 | Issue | 代码区域 | 文档区域 |
| --- | --- | --- | --- |
| AgentSpec normalized contract | #847 | `internal/config`, `internal/gateway`, `internal/worker` | `reference/configuration.md`, `v2/API-DESIGN.md` |
| Agent identity | #848 | `internal/session`, `internal/gateway`, `internal/audit` | `explanation/session-lifecycle.md`, `guides/enterprise/multi-tenant.md` |
| Runtime events | #849 | `pkg/events`, `internal/gateway`, `internal/eventstore` | `reference/aep-protocol.md`, `reference/events.md` |
| Runtime tracing | #850 | `internal/observability`, `internal/gateway`, `internal/session` | `reference/metrics.md`, `guides/enterprise/observability.md` |
| Execution queue | #851 | `internal/gateway`, `internal/session`, `internal/worker` | `explanation/session-lifecycle.md`, `guides/enterprise/resource-limits.md` |
| Runtime context | #852 | `internal/eventstore`, `internal/session`, worker adapters | `guides/developer/session-management.md`, future context reference |

## 验证门禁

每个 PR：

```bash
go test ./<touched-packages> -count=1
go test ./<risk-packages> -race -count=1
make docs-build
```

里程碑合并前：

```bash
make check
```

## 文档同步规则

- 修改 AEP event：同步 `docs/reference/aep-protocol.md` 和 `docs/reference/events.md`。
- 修改配置字段：同步 `docs/reference/configuration.md`。
- 修改 session 状态或恢复语义：同步 `docs/explanation/session-lifecycle.md`。
- 修改 metrics：同步 `docs/reference/metrics.md`。
- 修改 admin/runtime API：同步 `docs/reference/admin-api.md`。

## 明确非目标

- 不在 #847-#852 内实现分布式 scheduler。
- 不新建独立 memory service。
- 不建设 agent marketplace。
- 不改变 WebChat UI 主流程。
- 不把 2.0 绑定到单一 provider。
