# HOTPLEX 2.0 Roadmap

> 从 AI CLI Agent Runtime 演进为 Agent Runtime Platform，再进入 Agent Operating System。

## 目标定位

HotPlex 2.0 的核心不是重写一个新的 Agent OS，而是把 v1.x 已经存在的 Gateway、Session、Worker、AEP、Audit、Observability、Cron 能力收敛成稳定的运行时契约。

一句话目标：

> 让自主 Agent 可以被声明、启动、审计、观测、限制资源、恢复上下文，并在未来被调度和编排。

## 产品定位

HotPlex 2.0 的产品定位是 **self-hosted Agent Runtime Gateway**：

- 面向需要把 Claude Code、OpenCode、Codex CLI、ACP agent 等运行在受控环境中的工程团队。
- 提供远程会话、工作区绑定、权限边界、事件流、审计、可观测性和恢复能力。
- 作为 Agent runtime 基座，而不是通用低代码 workflow 平台、模型网关、向量知识库或 Agent marketplace。

核心用户：

| 用户 | 主要诉求 | 2.0 应交付的价值 |
| --- | --- | --- |
| 开发团队 | 远程、安全、可恢复地运行 coding agents | workspace-bound session、worker provider neutral runtime、resume/context recovery |
| 平台/运维 | 管理并审计 agent 行为 | identity、policy hook、audit trail、runtime observability |
| 集成开发者 | 通过 WebChat、消息平台、API 接入 agent runtime | stable AEP events、runtime metadata、admin/runtime diagnostics |
| 企业安全 | 知道谁在何处用什么工具做了什么 | agent identity、tool audit、trace/event/audit correlation |

明确不定位：

- 不做模型聚合网关：模型路由不是 HotPlex 2.0 的核心差异。
- 不做通用 workflow SaaS：多 Agent workflow 需要建立在 runtime contract 稳定之后。
- 不做独立 memory 产品：先复用 eventstore、turns、worker history。
- 不做 Agent marketplace：先沉淀 capability contract，再谈 registry/marketplace。

## 外部架构依据

外部成熟系统给 HotPlex 2.0 的启发：

| 参考方向 | 可迁移原则 | HotPlex 2.0 取舍 |
| --- | --- | --- |
| OpenAI Agents SDK | Runtime 应管理 turns、tool execution、guardrails、handoffs、sessions，并内建 tracing | HotPlex 先拥有 session/turn/tool 生命周期和 trace，不急于做 handoffs |
| MCP | 生命周期、capability negotiation、tools/resources/prompts 应有清晰协议边界 | AEP runtime metadata 和 AgentSpec 应先成为稳定契约 |
| OpenTelemetry | traces、metrics、logs/events 需要统一语义属性，避免事后拼接 | 先定义低基数 semantic keys，再补指标和 span |
| Temporal / LangGraph | Durable execution 依赖事件历史、checkpoint、resume 和 human-in-the-loop | eventstore/turns/worker_session_id 是 RuntimeContext 的事实源 |
| Kubernetes control plane | API 描述期望状态，controller 逐步调和实际状态 | #851 先做单机 input gate，再考虑 scheduler/reconcile loop |

## 当前基线

截至 v1.37.x，HotPlex 已经具备 2.0 的运行时内核（含 #878 durable ingress reliability epic 交付的 execution 账本/owner lease/repairer/单活跃门与 `runtime.execution.*` 事件）：

| 能力 | 当前实现 | 2.0 演进方向 |
| --- | --- | --- |
| Gateway | WebSocket + HTTP session 入口，负责 AEP 分发、init 握手、用户/workspace 校验 | 暴露稳定 Runtime API，统一 session 与 agent 语义 |
| Session | `CREATED/RUNNING/IDLE/TERMINATED/DELETED` 状态机，SQLite/PostgreSQL 持久化，用户和 workspace 绑定 | 增加 Agent identity、task context、execution metadata |
| Worker | `claude_code`、`opencode_server`、`codex_cli`、`acp` 注册式适配器 | 通过 AgentSpec 统一声明 provider、权限、sandbox、预算 |
| AEP | `pkg/events` 作为唯一 wire contract，Envelope 支持 Metadata，OwnerID 内部校验 | 增加 runtime/security/audit 事件类型，不引入第二套事件总线 |
| Event Store | outbound/inbound 事件持久化、turns 聚合、崩溃/超时 synthetic turn | 为 runtime trace、context recovery 和审计查询提供事实源 |
| Execution / Durable Ingress | `internal/execution` 输入账本：owner instance + lease、有界 repairer、delivery/runtime 状态分离、单活跃门（`SESSION_BUSY`）、ambiguity fence、`runtime.execution.*` 事件（#878 epic） | 作为 #851 ExecutionQueue 与 #868 Cockpit 的持久化事实源；real-PG 多实例验证待接入 CI |
| Observability | OpenTelemetry bootstrap、Prometheus metrics、worker/session/gateway 指标 | 增加 Agent lifecycle、tool execution、policy decision spans |
| Security/Audit | API key、cookie admin fallback、workspace owner 校验、tool call/user activity audit | 收敛成策略检查接口，保持现有认证链路 |
| Cron | 定时任务、执行超时、重试、平台投递 | 作为 scheduler 的已实现经验，避免过早引入分布式调度 |
| Multitenancy | 用户、workspace、OAuth/SSO、多 bot、多 workspace 配置 | 作为 2.0 identity/ownership 的基础，不另起身份系统 |

## 设计原则

1. **演进现有内核**：扩展 Session、Worker、AEP 和 config，不绕开它们。
2. **先定义契约再扩展能力**：AgentSpec、AgentIdentity、RuntimeEvent 是 2.0 的第一批契约。
3. **不引入平行系统**：不要新建独立 event bus、identity service、memory service 或 scheduler。
4. **兼容 v1.x 客户端**：AEP v1 wire format 和现有 worker config 必须继续工作。
5. **小 PR 可回滚**：每个 issue 独立验收，最终通过文档和 traceability matrix 串联。

## qm 研究终态校准（2026-08-04）

对 [yc-software/qm](https://github.com/yc-software/qm) 的源码、测试、部署文档和公开 issue 交叉确认后，2.0 路线确定三条运行时约束：

- **EffectiveRuntimePlan**：把 config、init metadata、workspace、worker、policy、sandbox 和 env profile 解析成同一份 redacted、可 hash、可审计的期望状态；WS init、REST create-session、doctor、worker start 和 recipe dry-run 不得各算一份。
- **Gateway-owned EffectLedger**：对 delivery、webhook、cron、admin/control 和 recipe 等 Gateway 自己发起的副作用记录 attempt/idempotency/outcome；`unknown` 进入 reconcile/fence，不把网络断开误当成 failed 后盲目重试。worker 私有 tool protocol 不搬进 Gateway。
- **Capability/Isolation honesty**：能力清单、skills/config hash 和 filesystem/network enforcement 以 `enforced/partial/unavailable` 报告，不能把 token、审计或 screening 误写成形式化隔离或 prompt-injection 解决方案。

这三条是对已有 AgentSpec、AgentIdentity、AEP runtime events、execution ledger、Audit 和 Cron 的收敛，不是新增平行 runtime 层。qm issue [#165](https://github.com/yc-software/qm/issues/165) 证明“renderer 派生环境”和“secret/doctor 读取原始配置”分裂会造成部署 crash-loop；HotPlex 将其转化为 #867 的 effective-plan/preflight 依赖。

上述三项统一为一条事实闭环：

```text
Authority/Scope → Desired Plan → Durable Execution/Effect → Observed State → Reconciliation/Fence
```

因此，`plan_hash` 只能证明 HotPlex 计算出了期望状态，Worker `done` 只能证明 Worker 返回了终态；两者都不能单独证明 sandbox layer 已应用、外部消息已送达或 provider effect 已成立。#946/#947 的验收必须增加 observed evidence、drift、unknown 和 reconcile 语义。

## Phase 1: Runtime Contract 2.0

目标：把现有运行时能力提升为明确的数据模型和事件契约。

跟踪 issue：

- [#847](https://github.com/hrygo/hotplex/issues/847) `feat(config): introduce AgentSpec runtime model`
- [#848](https://github.com/hrygo/hotplex/issues/848) `feat(session): bind agent identity to runtime sessions`
- [#849](https://github.com/hrygo/hotplex/issues/849) `feat(aep): extend event protocol for runtime observability`
- [#850](https://github.com/hrygo/hotplex/issues/850) `feat(obs): enhance existing tracing with agent runtime spans`

交付物：

- AgentSpec 作为现有 config/session init 的标准化视图。
- SessionInfo/Session record 增加 agent identity 与 execution metadata。
- AEP 增加 agent/runtime/security/audit 事件类型。
- OpenTelemetry trace 串联 init、worker start、turn execution、tool call、done/error。

完成标准：

- `make check` 通过。
- `make docs-build` 通过。
- 新增或更新 `docs/reference/aep-protocol.md`、`docs/reference/configuration.md`、`docs/explanation/session-lifecycle.md` 中受影响内容。
- 旧客户端无需变更即可继续连接和收发 AEP v1 事件。

优先 first cuts：

- `AgentSpec` 先作为只读 normalized view，统一 WS/REST session 创建路径，不改变持久化和 Worker 接口。
- `AgentIdentity` 先通过 session `context_json`、AEP metadata、audit detail 和 trace attributes 贯穿，不急于 schema 化。
- Runtime events 先围绕单次 input 的 `execution_id` 建立 started/completed/failed 三个最小事件。
- Observability 先补低基数 execution metrics 和 trace correlation，不引入高基数 label。

产品验收：

- 用户能回答：这个 session 由谁发起、绑定哪个 workspace、使用哪个 worker、执行了哪次 input、触发了哪些工具。
- 运维能回答：一次失败来自 init、worker start、input dispatch、tool call、done/error 的哪一段。
- 旧客户端不需要理解新 runtime events 也能继续工作。

## Phase 2: Runtime Control Plane

目标：在单机 Gateway 内形成可调度、可恢复、可限流的执行控制层。

跟踪 issue：

- [#851](https://github.com/hrygo/hotplex/issues/851) `refactor(runtime): introduce execution queue abstraction`
- [#852](https://github.com/hrygo/hotplex/issues/852) `feat(context): introduce runtime context persistence interface`

交付物：

- ExecutionQueue：位于 Session 与 Worker 之间，负责 ordered dispatch、timeout、retry 和执行元数据。
- RuntimeContext：从当前 session/eventstore/worker history 中抽象上下文接口，先支持 session-scoped context。
- Policy hook：复用现有 permission mode、allowed tools、workspace sandbox、audit collector，为后续 PolicyEngine 留接口。

完成标准：

- 不改变现有用户输入到 worker 的语义。
- turn timeout、auto retry、crash synthetic turn、event capture 仍由现有测试覆盖。
- 队列和上下文接口具备 race 测试，单模块 `-race -count=1` 通过。

优先 first cuts：

- ExecutionQueue 先做 per-session input gate，证明同 session 输入不会并发 interleave。
- RuntimeContext 先做只读 facade，复用 eventstore、turns、worker_session_id、workspace metadata。

产品验收：

- 用户连续发送多条 input 时，同一 session 内执行顺序可解释、可审计、可恢复。
- Gateway 重启或 worker 崩溃后，可以基于 RuntimeContext 解释恢复路径和丢失边界。
- Control Plane 仍然是单机内核，不承诺分布式调度。

## Phase 3: Agent Platform

目标：在 Runtime Contract 和 Control Plane 稳定后，开放平台能力。

候选方向：

- 多 Agent workflow：manager/tester/reviewer 等角色编排。
- 外部 memory backend：project memory、skill memory、knowledge memory。
- 企业策略：RBAC、审计查询、合规导出。
- 分布式调度：仅在单机 ExecutionQueue 的边界清晰后启动。

启动门槛：

- #847-#852 的 first cuts 已在生产或长期测试环境中验证。
- Runtime metadata 能稳定关联 AEP、eventstore、audit、trace。
- 至少一个真实工作流证明需要多 Agent 编排，而不是单 Agent + tool/context 即可解决。

## 实施优先级

在完整 ExecutionQueue、Execution Cockpit 和 Coding Ops Recipes 之前，先完成以下共同事实层：

1. #877 fenced execution escape hatch：消除异常三重失败导致的无界 session lockout；
2. #867 explicit worker env allowlist + isolation capability report：由 EffectiveRuntimePlan 驱动，兼容模式必须可诊断；
3. 新增 effective runtime plan / fail-closed preflight：让 doctor、worker start、admin diagnostics 和 recipe dry-run 使用同一 resolver；
4. 新增 Gateway-owned EffectLedger：再将 #851 的 queue state、#868 Cockpit、#870 recipes 接入该事实层。

#851 的剩余范围不再包含 #878 已交付的 single-active gate、owner lease、ambiguity fence 和最小 runtime events；#868 只能消费已持久化且脱敏的 plan/effect facts；#870 不能在没有 dry-run plan 和 idempotency contract 时扩展为 workflow engine。

5. 先选 Cron/Webhook/消息 delivery 完成一条 `desired → effect → observed → reconcile` 垂直闭环，再提取通用 EffectLedger；旧进程内 retry queue 不能作为重启/多实例后的最终事实源。

## 终版正交架构闸门（2026-08-04）

为避免把 qm 的局部机制误读为 HotPlex 的整体方向，路线受以下跨维度闸门约束。详细审查见 [qm × HotPlex 正交架构审查](../research/2026-08-04-qm-hotplex-orthogonal-architecture-review.md)。

### 必须同时满足的架构不变量

- HotPlex 仍是 self-hosted Agent Runtime Gateway；workflow engine、marketplace、独立 memory service、跨 session scheduler 和 SaaS control plane 不进入当前主线。
- authority/scope、desired plan、durable execution、Gateway-owned effect、worker/provider observed state 和 reconciliation 各自只有一个 canonical owner，其他模块只能投影。
- `plan_hash` 只能证明期望计划，`worker done` 只能证明 worker turn 终态，provider response 只有在业务 receipt 或可查询状态支持时才能提升为外部成功。
- 认证、授权、能力声明、sandbox/OS enforcement、审计和 workload identity 分开建模；无法证明的能力必须标记 `partial`/`unavailable`。
- 新 AEP 字段必须增量兼容；SQLite/PostgreSQL 迁移、条件更新、旧版本读写和 fault-injection 必须成对验证。
- effect/reconcile/repair 必须具备 lease/fence、attempt cap、backoff、stop condition 和 operator escape hatch；禁止以 exactly-once 掩盖 provider 事实不明。
- `execution_id`/`effect_id`/`plan_hash` 可做 trace/log/audit correlation，但不可直接成为无界高基数 metrics label；Cockpit 查询必须有分页、时间和 payload 预算。

### 实施前的否决条件

若一个提案无法同时说明 canonical owner、故障时序、外部 evidence、敏感数据边界、双数据库迁移、容量预算和回滚路径，则只能保持 `proposed`/`shadow`，不能进入 `enforced`/`completed`。任何通用抽象必须由至少两个已验证消费者驱动，不能先建平台再寻找场景。

### 重新校准的波次

```text
Wave 0  边界/事实所有权/状态词汇/隐私/容量冻结
   ↓
Wave 1  EffectiveRuntimePlan + observed worker bootstrap
   ↓
Wave 2  Cron/Webhook/消息 delivery 一条 durable effect 闭环
   ↓
Wave 3  Repair + redacted Cockpit + SLO/容量治理
   ↓
Wave 4  在真实需求证明后评估 scheduler/workflow/registry/workload identity
```

Wave 4 不再由“平台化愿景”自动触发，而必须由 Wave 1–3 的运行时 SLO、容量和迁移证据触发。

#946 不能只输出配置 hash，必须区分 declared、observed、enforced、partial、unavailable；#947 不能只增加唯一键，必须区分 claim、attempt、effect fact、external evidence 和 unknown/fence。

## Phase 4: Agent OS

目标：形成跨环境的 Agent execution kernel。

长期能力：

- Agent registry 和版本化发布。
- 多租户企业控制面。
- Kubernetes/operator 或私有云部署。
- Agent marketplace 或 capability catalog。

## 路线图边界

| 不做 | 原因 |
| --- | --- |
| 立刻重写 Agent runtime | 当前 Worker/Gateway/Session 已经是可演进内核 |
| 立刻建设分布式 scheduler | Cron 和 session pool 先沉淀单机调度边界 |
| 立刻建设独立 memory service | eventstore、turns、worker session history 已能支撑第一版 RuntimeContext |
| 另起 event bus | AEP 和 eventstore 已是运行时事实源 |
| 新建身份系统 | user/workspace/OAuth/session owner 已提供身份基础 |

## 参考资料

- OpenAI Agents SDK: https://openai.github.io/openai-agents-python/
- Model Context Protocol specification: https://modelcontextprotocol.io/specification/2025-11-25
- OpenTelemetry semantic conventions: https://opentelemetry.io/docs/concepts/semantic-conventions/
- Temporal durable execution: https://temporal.io/blog/what-is-durable-execution
- LangGraph overview: https://docs.langchain.com/oss/python/langgraph/overview
- Kubernetes controllers: https://kubernetes.io/docs/concepts/architecture/controller/

## 成功指标

2026 Runtime Contract：

- 4 类 worker 共享 AgentSpec 映射。
- 关键 runtime 操作均能通过 AEP event + trace + audit 关联。
- 关键副作用能区分 desired、attempted、observed、unknown 和 reconciled；不能以 Worker done 冒充外部成功。
- Agent identity 能贯穿 session、worker、eventstore、audit。
- 2.0 文档与 GitHub issues 可双向追踪。

2026 Runtime Control Plane：

- ExecutionQueue 支持 ordered dispatch、timeout、retry 观测。
- RuntimeContext 支持 session recovery 与 provider-specific adapter。
- 单机资源限制和 workspace 隔离可验证。
- 至少一条 Cron/Webhook delivery 路径在重启、多实例、超时和晚到确认下可收敛，且不会盲目重复外部副作用。

2027 Agent Platform：

- 多 Agent workflow 可在单 Gateway 内运行。
- 可插拔 memory backend 不破坏 v1.x session 语义。
- 企业部署可基于现有 observability/security/admin API 扩展。
