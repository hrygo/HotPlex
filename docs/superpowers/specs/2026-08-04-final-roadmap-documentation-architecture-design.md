---
title: "HotPlex 2.0 Final Roadmap Documentation Architecture"
type: design
status: approved
date: 2026-08-04
owners: [hotplex-runtime]
references:
  - docs/v2/ROADMAP.md
  - docs/v2/IMPLEMENTATION-ROADMAP.md
  - docs/v2/ARCHITECTURE.md
  - docs/specs/README.md
---

# HotPlex 2.0 终版路线图文档架构

## 1. 目标状态

HotPlex 2.0 文档形成一套稳定、无调研时间线、无重复事实源的发布态体系。主文档只描述已经成立的产品事实、当前能力、目标契约、实施顺序、完成定义和明确非目标；研究文档只保留证据与推导，不参与主路线的事实定义。

## 2. 权威层级

文档权威顺序固定为：

```text
ROADMAP
  ├─ 产品定位、当前基线、目标状态、阶段、优先级、完成定义
  ├─ IMPLEMENTATION-ROADMAP
  │    └─ 交付切片、依赖、进入条件、退出条件、Issue 映射
  ├─ ARCHITECTURE
  │    └─ 组件、事实所有权、数据流、状态模型、兼容边界
  └─ Approved Specs
       └─ 字段、接口、状态机、安全约束、测试契约

Research Reports
  └─ 源码、测试、Issue、外部规范和 ROI 的证据附件
```

出现冲突时，以当前代码和测试确认“已交付事实”，以 ROADMAP 确认产品与阶段边界，以 approved spec 确认尚未交付能力的冻结契约。研究报告不覆盖 ROADMAP 或 approved spec。

## 3. 文档职责

### 3.1 `docs/v2/ROADMAP.md`

ROADMAP 是 HotPlex 2.0 的产品与演进唯一事实源，固定包含：

1. 产品定位与非目标；
2. 当前已交付能力；
3. 目标运行时模型；
4. 设计不变量；
5. 四个交付阶段及其完成定义；
6. 当前实施优先级；
7. 明确暂缓项；
8. 成功指标与发布闸门；
9. Issue/spec 追踪矩阵。

ROADMAP 不包含“调研过程、上一轮校准、进一步分析、建议、候选吸收项”等时间线表达。qm、Kubernetes、Temporal、OpenTelemetry、NIST、SPIFFE 等只在“设计依据”中作为原则来源出现，不作为阶段标题。

### 3.2 `docs/v2/IMPLEMENTATION-ROADMAP.md`

Implementation Roadmap 只描述执行结构：

- 已完成基线；
- 当前交付切片；
- issue/spec 依赖图；
- 每个阶段的进入条件、交付物、退出条件；
- 迁移、兼容、验证和回滚要求。

该文档不重复产品定位、外部研究结论或完整架构解释。已完成项直接写为现状，不保留“first cut 曾经如何形成”的过程叙事。

### 3.3 `docs/v2/ARCHITECTURE.md`

Architecture 只描述稳定结构：

- Gateway、Session、Worker、AEP、Event Store、Execution、Effect、Observed State、Reconciliation、Audit、Observability 的职责；
- canonical owner 与只读投影；
- session/turn/effect 数据流；
- desired、execution、effect、observed、reconciled 状态关系；
- AEP、Worker、SQLite/PostgreSQL、跨平台兼容边界；
- 反模式。

“分阶段落地”从 Architecture 移除，由 ROADMAP 和 Implementation Roadmap 唯一负责。

### 3.4 Approved Specs

Approved spec 是尚未交付能力的冻结实现契约。Spec 使用产品或能力命名，不在标题和正文主叙事中使用 `qm-inspired`、`研究校准` 等来源性命名。外部来源只放入 references。

现有两份相关 spec 的终态名称为：

- Runtime Operations Contract；
- Scope-aware Capability Inventory Contract。

Spec 只描述范围、模型、状态机、安全边界、兼容性、完成定义与非目标，不保留“上一版、二阶校准、第一刀、后续再做”等过程语言。

### 3.5 Research Reports

三份 qm 研究报告保留在 `docs/research/`，状态为 `final-evidence`。其职责是提供：

- qm 与 HotPlex 源码证据；
- 上游 issue 反例；
- 外部架构依据；
- ROI 模型；
- 决策推导。

研究报告顶部明确声明：它们是证据附件，不是产品路线或实现契约；最终决策以 ROADMAP、ARCHITECTURE 和 approved specs 为准。

## 4. 事实口径

所有终版文档采用以下状态词汇：

| 状态 | 含义 | 允许的表述 |
| --- | --- | --- |
| 已交付 | 当前代码、测试和发布记录均存在 | “已实现”“当前提供”“事实源为” |
| 已冻结 | 设计契约已批准，实现可能未完成 | “契约定义为”“完成标准为” |
| 当前实施 | 已进入明确 issue/交付切片 | “当前优先级”“实施范围” |
| 暂缓 | 不属于当前路线 | “不进入当前阶段”“启动条件为” |
| 非目标 | 明确拒绝 | “不实现”“不承担” |

禁止使用：

- “本轮、上一轮、下一轮、进一步调研、校准后新增”；
- “建议、可以考虑、可能、拟、候选”（除明确列出的长期条件式能力）；
- 用 `plan hash`、Worker `done`、HTTP 成功或 audit 记录替代外部事实；
- 把设计已冻结写成能力已实现。

## 5. 内容去重规则

1. 产品定位只在 ROADMAP 完整定义，其他文档链接引用。
2. 当前能力只在 ROADMAP 和 ARCHITECTURE 各以产品视角、组件视角描述，不复制研究推导。
3. 阶段和优先级只在 ROADMAP 定义，Implementation Roadmap 展开执行条件。
4. 状态机字段只在 approved spec 定义；ROADMAP 只引用语义。
5. 研究反例、源码行号和上游 issue 只在 research 文档完整保留。
6. Issue 状态由 GitHub 维护；文档只保留稳定映射，不复制评论时间线。

## 6. 终版主线

HotPlex 2.0 的路线固定为四个阶段：

1. **Runtime Contract**：AgentSpec、AgentIdentity、AEP runtime metadata、trace/audit correlation、EffectiveRuntimePlan 和 observed bootstrap。
2. **Runtime Reliability**：single-active execution、owner lease、ambiguity fence、Gateway-owned durable effect 和 reconciliation。
3. **Runtime Operations**：bounded ExecutionQueue、read-only RuntimeContext、Execution Cockpit、diagnostics 和 operator action。
4. **Conditional Platform Expansion**：capability governance、recipes，以及满足真实需求和运行时 SLO 后的 scheduler/workflow/workload identity。

已交付能力直接纳入对应阶段的“当前事实”；未交付能力以“冻结契约”和“完成标准”表达，不保留其研究形成过程。

## 7. 验收标准

- ROADMAP 不出现调研时间线或重复的校准章节；
- Implementation Roadmap 不重复产品定位和外部研究过程；
- Architecture 不包含阶段计划；
- approved specs 不以 qm 或研究阶段命名主契约；
- research 文档明确为非权威证据附件；
- 已交付、已冻结、当前实施、暂缓、非目标五种状态不混用；
- ROADMAP、Implementation Roadmap、Architecture、spec 和 Issue 映射无矛盾；
- 文档链接、Markdown、Swagger/docs 构建和 `git diff --check` 通过；
- 不修改用户现有 audit 代码和测试改动。

## 8. 未采用方案

### 将研究报告合并进 ROADMAP

未采用。该方案使证据、决策和执行计划混合，ROADMAP 会随每次外部研究持续膨胀。

### 保持现有增量校准章节

未采用。该方案保留跨阶段叙事和重复波次，读者无法区分当前事实、冻结设计与历史推导。

### 删除研究报告

未采用。该方案会丢失源码、Issue、外部规范和 ROI 证据链，降低后续决策的可复核性。
