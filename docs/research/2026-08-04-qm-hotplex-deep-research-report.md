---
title: "qm 对 HotPlex 的深度调研、校准与 ROI 评估"
date: 2026-08-04
status: final-evidence
---

# qm 对 HotPlex 的深度调研、校准与 ROI 评估

> 本文是源码、测试、Issue、外部规范和 ROI 的证据附件，不是产品路线或实现契约。最终决策以 `docs/v2/ROADMAP.md`、`docs/v2/ARCHITECTURE.md` 和 approved specs 为准。

## 结论先行

qm 对 HotPlex 最有价值的不是“增加 Web UI、Slack、公司记忆或应用发布”，而是把 Agent Runtime 的几个隐含边界做成了可验证的运行时对象：

1. **Scope-first resolution**：每次 turn 先解析 actor、scope、workspace layer、policy、egress、approval 和 capability，再调用 harness；
2. **Resolved runtime plan**：把配置、入口参数和环境派生出的“实际期望状态”集中计算，并让 check/doctor/deploy 使用同一份结果；
3. **Durable execution + effect ledger**：run 通过 lease/heartbeat/reaper 管理，工具调用按 `run + attempt + callIndex` 做去重，未知结果不盲目重试；
4. **Governed skill projection**：技能有 scope、签名、审核、能力授权、shadowing、promotion、内容 hash 和安全 materialization；
5. **Boundary honesty**：qm 的安全文档明确哪些控制只是 defense in depth，哪些不是隔离、授权或 prompt-injection 的形式化证明。

HotPlex 已经在 AgentSpec、AgentIdentity、AEP runtime events、OpenTelemetry、durable ingress、owner lease、ambiguity fence、Audit 和 Cron 上走在同一条路线上；因此最优策略是**收敛和补强现有契约，不复制 qm 的产品表面，也不再开一个平行 runtime 层**。

## 证据基线

| 项目 | 基线 |
|---|---|
| qm 本地仓库 | `/private/tmp/qm-research-20260804`，`eabe3d7`，浅克隆自 `https://github.com/yc-software/qm` |
| qm 图谱 | codebase-memory：17,124 nodes / 58,423 edges；TypeScript 1,015 files |
| HotPlex 图谱 | codebase-memory：27,404 nodes / 125,432 edges；Go 792 files |
| 证据方法 | 以 codebase-memory 图谱、精确 `file:line` 源码、测试、配置和 issue 当前状态交叉确认 |
| 取证边界 | README 是设计意图，不等于能力已在生产验证；以源码、测试、配置和 issue 的当前状态交叉校准 |

HotPlex 既有 audit 工作区改动不属于本报告变更范围；qm 的 source-study 核心笔记与进度记录位于第二大脑 `Harness/qm-research-20260804`。

## qm 架构中真正可迁移的部分

### 1. Scope 是共同权限边界，而不是业务标签

`qm/src/types.ts:12-23` 将 personal/channel/team/org/group 统一编码为 `ScopeId`；`qm/src/types.ts:108-123` 的 `Resolution` 同时携带 layers、system prompt、egress、command policy、security policy、approval modes 和 granted handles。`qm/src/core/orchestrator.ts:391-503` 又在 turn 入口先检查 internal principal、共享 audience、roster version、rate limit、budget 和 scope resolution。

这给 HotPlex 的启发是：`workspace + session + worker + permission + config` 应继续朝一个不可变的 `EffectiveRuntimePlan` 收敛，避免每个 adapter 各自解释 `worker_type`、workdir、permission mode、allowed tools 和 budget。

HotPlex 已经实现了同方向的 `internal/agentspec/resolve.go:77-125`、`internal/agentspec/identity.go:125-168`，并将 identity 先放入 `context_json`。因此这里不是新架构，而是把现有 first cut 从“配置归一化”推进到“可展示、可校验、可审计的 resolved plan”。

### 2. Resolved plan 解决的是配置漂移与可运营性

qm 的 `src/wiring.ts:364-480` 将持久化、skills、sandbox、run store、harness 和审计统一装配；README 的部署路径强调 `qm check`、`doctor`、`up` 共用 deployment contract。上游 [issue #165](https://github.com/yc-software/qm/issues/165) 暴露了一个关键失败模式：Fly renderer 派生出 `SANDBOX_BACKEND=sprites`，secret predicate 却只读取原始配置，导致 `check/doctor` 通过而部署 crash-loop。

可迁移的不是具体 CLI，而是原则：**计算 effective environment 的函数必须是 secret collection、doctor、render、deploy 的唯一事实源**。HotPlex 当前 `internal/config` 与 `internal/worker/base/BuildEnv` 仍有 inherit-all-plus-blocklist 的边界；[issue #867](https://github.com/hrygo/hotplex/issues/867) 已明确要改为 explicit allowlist + isolation profile，本报告建议将 effective plan/preflight 作为它的上游契约。

### 3. Tool context 把策略、沙箱和副作用放在一条路径

`qm/src/tools/primitives.ts:416-424` 以 `runId + attempt + callIndex` 访问 `ToolLedger`；`479-526` 在执行前统一做 command policy、deny/approval、sandbox provision、timeout ceiling 和执行审计；`578-651` 对文件写入、持久化、artifact、ACL grant 和 audit 做同一条闭环。

HotPlex 不应把 worker 内部所有 tool call 重新搬到 Gateway。应迁移边界限定为 Gateway-owned effects：Cron delivery、Webhook trigger、admin/control write、recipe delivery、外部消息发送和需要 retry 的 connector operation。每个 effect 记录 `execution_id`、attempt、operation key、idempotency key、outcome；结果为 `unknown` 时 fence 后续冲突动作，直到查询或人工确认。

这会补上 #851 完整 queue 与 #870 recipes 之间的可靠性缺口，同时复用 #878 已落地的 execution ledger/fence，不创建第二套 event bus。

### 4. Skills 是受治理的 capability projection

`qm/src/skills/skill-store.ts:114-195` 对技能做安全命名、HMAC manifest、review、required capability 授权和 publish gate；`221-278` 按 ordered scopes 解析同名技能，支持 shadowing、promotion 和 move 限制。`qm/src/skills/materialize.ts:210-269`、`272-379` 通过 hash marker、safe path、增量清理、bundle 保护和 keyed/advisory lock 将技能投影进 sandbox。

HotPlex 当前已有 agent-config B/C 双通道、注入排除边界、worker-specific skills 和 AEP `skills_list`，但还没有 qm 这种“capability inventory + scope precedence + safe projection”统一对象。建议先建立只读 capability inventory 和 effective skill/config hash，不立即建设 marketplace、远程 skill registry 或独立 memory service。

### 5. Durable run 与未知结果语义值得直接吸收

`qm/src/runs/run-store.ts:19-100` 把 run 的 status、attempt、dedup key、lease token、lease expiry、worker id、delivery state 和 terminal wait 统一进 store；`qm/src/runs/worker.ts:22-82` 通过 heartbeat 丢失连续计数取消 in-process turn，`101-186` 负责 claim、stop drain 和 lease handback；`qm/src/runs/reaper.ts:40-53` 通过 leader lease 运行 reaper。

HotPlex 的 `internal/execution` 已实现更严格的 accepted/delivered/unknown/failed、owner lease、single-active gate、late convergence 和 ambiguity fence；这意味着 qm 的 run model 不应被照搬为新表。可以吸收的部分是：

- 把 `effect attempt` 与 `execution attempt` 关联，而非只靠日志；
- 为 queue/recipe/control effect 提供 `unknown` 后的 bounded repair/reconcile；
- 把 reaper、fence 和 operator escape hatch 纳入同一个 lifecycle explanation。

### 6. Capability token 与边界诚实

`qm/src/auth/capability-token.ts:21-47` 将 actor、scope、audience、credentials、egress 和 expiry 放入签名 token，`60-85` 做结构与时效验证；但 `SECURITY.md:70-143` 明确 command policy 可被绕过、screening 是 heuristic、credential materialization 期间仍是明文、egress 约束依赖 backend、admin 可读取敏感内容。

HotPlex 的安全文档、Admin、worker isolation 和 audit 也应采用同样的 claim discipline：报告“策略已检查/操作已审计/能力已声明”，不报告“模型无法越权/worker 已隔离/审计阻止了副作用”。

## 校准：哪些应进入 HotPlex，哪些不应进入

| qm 机制 | HotPlex 现状 | 校准结论 | 处理 |
|---|---|---|---|
| Scope-owned runtime resolution | workspace/session/worker/AgentSpec 已存在 | 直接收敛为 EffectiveRuntimePlan | Runtime Operations Contract |
| Agent identity object | #848 已完成，identity 在 context_json | 继续作为跨 AEP/audit/trace 的共享键 | 保留现状，补验证与文档 |
| Run lease/reaper | #878 已完成 owner lease/repair/fence | 不复制 qm run store；补 Gateway-owned effect ledger | Runtime Operations Contract、#851/#870 |
| Tool ledger | HotPlex 有 execution ledger，但没有 effect-level exactly-once 边界 | 只覆盖 Gateway-owned side effects；worker-private tools 不搬运 | 新 issue，非 #851 全量队列的一部分 |
| Skills review/publish/materialize | HotPlex 有 agent config/skills 注入，能力治理较分散 | 先 capability inventory/hash/precedence，后做 promotion | 新 issue，低于 isolation/preflight |
| Deployment check/doctor | HotPlex 有 CLI doctor/checkers | 用同一个 resolved plan 做 preflight、render、diagnose | 新 issue，优先级高 |
| Per-scope sandbox | HotPlex worker/workspace/permission 已有边界 | 抽取 capability report，不宣称 OS isolation | 加入 #867 |
| Web apps / Slack-first company surface | HotPlex 是 Gateway + WebChat + Slack/Feishu adapters | 非核心差异，暂不引入 | 明确 non-goal |
| Org memory service / knowledge product | HotPlex roadmap 已拒绝独立 memory service | 只做 RuntimeContext facade，复用 eventstore/turns | 保持 #852 边界 |
| Private-fork deployment model | HotPlex 发布/部署模型不同 | 只吸收“core vs deployment-specific config”思路 | 不复制仓库运营模式 |

## ROI 评估

评分采用 `价值 × 采用概率 × 现有复用度 ÷ 实施成本` 的相对模型，10 为最高。实施成本含跨平台测试、AEP/SDK/文档和迁移风险。

| 投资项 | 价值 | 复用度 | 成本 | 相对 ROI | 建议 |
|---|---:|---:|---:|---:|---|
| EffectiveRuntimePlan + dry-run preflight | 9 | 9 | 3 | **27.0** | 立即做；连接 #847/#867/doctor |
| Explicit worker env allowlist + isolation report | 10 | 8 | 5 | **16.0** | 与 preflight 并行，安全优先 |
| Gateway-owned effect ledger + unknown/fence | 9 | 8 | 5 | **14.4** | 作为 #851/#870 的共同底座 |
| Execution Cockpit | 8 | 8 | 6 | 10.7 | 等 effect/runtime facts 稳定后做 |
| Capability inventory + skill hash/precedence | 7 | 7 | 6 | 8.2 | 先只读，避免 marketplace 过早 |
| Full cross-session scheduler | 5 | 3 | 9 | 1.7 | 暂缓 |
| External memory backend / memory product | 4 | 3 | 8 | 1.5 | 暂缓 |
| Web app publishing surface | 3 | 2 | 8 | 0.8 | 不纳入 HotPlex 2.0 |

### 终态投资决策

当前路线只投入三条主线：

1. **Preflight / resolved plan**：最快提升“为什么这样运行”的可解释性，并能提前拦截配置-渲染漂移；
2. **Worker isolation contract**：直接降低未知环境变量、凭据泄漏和错误安全宣称的风险；
3. **Effect ledger**：让 Cron/Webhook/recipe/admin side effects 可以在 retry、crash、网络断开后收敛。

Execution Cockpit、完整 queue 和 capability promotion 都依赖这三条线的事实，顺序不能倒置。

## 决策映射

### ROADMAP 映射

- Phase 1 的交付物包含 `EffectiveRuntimePlan`、preflight、redacted resolved config 和 plan hash，并与 AgentSpec/Identity/Event/Trace 共享关联键。
- Phase 2 的依赖顺序为 #877 fence escape hatch、#867 isolation profile/preflight、完整 #851 queue、#870 recipes。
- #851 的范围是持久化排序/attempt/timeout/retry reason、effect lifecycle 和 queue state，不重复 #878 已交付的 single-active gate。
- #868 Cockpit 消费 execution/effect facts，默认展示 redacted plan、policy decision、attempt、unknown/fence 和 trace/audit refs。
- Skills/capabilities 以 Phase 3 只读 inventory 形式纳入，不包含 registry/marketplace。

### Approved contract 映射

`docs/superpowers/specs/2026-08-04-runtime-operations-contract.md` 定义 EffectiveRuntimePlan、preflight、Gateway-owned EffectLedger、isolation capability report、unknown/fence 和验收边界。

`docs/specs/Scope-Aware-Capability-Inventory-Spec.md` 限定 HotPlex 的 scope-aware capability inventory 与安全 materialization，不引入独立 memory service 或 marketplace。

## Issue 映射

### 既有 Issue 的冻结范围

- [#851](https://github.com/hrygo/hotplex/issues/851)：full queue 不重复 #878 first cut，并消费 EffectLedger 的 queue/effect 事实。
- [#867](https://github.com/hrygo/hotplex/issues/867)：explicit env allowlist/isolation profile 依赖 EffectiveRuntimePlan/preflight，并处理 qm #165 的 derived-env drift 反例。
- [#868](https://github.com/hrygo/hotplex/issues/868)：Cockpit 只展示 redacted plan/capability/isolation/effect facts。
- [#870](https://github.com/hrygo/hotplex/issues/870)：recipe 必须使用 EffectLedger 和 dry-run resolved plan，不扩展为 workflow/DAG engine。

### 新增 Issue 的冻结范围

- `feat(ops): expose effective runtime plan and fail-closed preflight diagnostics`：配置、init、workspace、worker、env profile、policy 和 sandbox capability 由一份可审计、可 dry-run 的 plan 表达。
- `feat(runtime): add Gateway-owned effect ledger for retry-safe side effects`：EffectLedger 覆盖 delivery/webhook/cron/control/recipe effect，不覆盖 worker-private tool internals。
- `feat(agent): add scope-aware capability inventory and safe materialization contract`：Capability/skill inventory、precedence、hash 和 materialization report 只读优先，promotion 需要显式授权。

## 风险与停止条件

- 不把 qm 的 Node/TypeScript 组织方式、Slack-first identity 或 web-app product surface 引入 HotPlex。
- 不因为“有 capability token”就宣称强隔离；必须同时报告 backend 的实际 enforcement。
- 不把 effect ledger 做成第二套 event store；所有事实仍落在 execution/eventstore/audit 现有边界内。
- ROI 结论不等同于生产保证；生产保证还需要真实副作用调用样本、unknown 重现、SQLite/PostgreSQL 双方言测试和跨 worker 回归。
- 如果 EffectiveRuntimePlan 只能在各 adapter 内各自计算，说明切片失败，应先收敛 resolver 而不是扩展 UI。
