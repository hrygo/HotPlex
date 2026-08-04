# HotPlex 2.0 Final Roadmap Documentation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将 HotPlex 2.0 路线与相关文档定稿为无调研时间线、无重复事实源、状态口径一致的发布态文档体系。

**Architecture:** `docs/v2/ROADMAP.md` 是产品定位、当前事实、目标状态、阶段和优先级的唯一主线；`IMPLEMENTATION-ROADMAP.md` 只展开交付切片与阶段闸门；`ARCHITECTURE.md` 只描述稳定组件和事实所有权；approved specs 定义冻结契约；`docs/research/` 只保留证据。所有文档通过稳定链接引用，不复制调研过程和 Issue 评论时间线。

**Tech Stack:** Markdown、HotPlex 文档构建器、Git、GitHub Issue 链接。

## Global Constraints

- 项目文档使用简体中文，保留 `EffectiveRuntimePlan`、`EffectLedger`、AEP、`unknown`、`fence` 等精确字面量。
- 当前代码和测试决定“已交付事实”；approved spec 只代表冻结契约，不得写成已实现。
- AEP、Worker、SQLite/PostgreSQL、跨平台、安全和敏感数据边界必须与项目 `AGENTS.md` 一致。
- 不修改 `internal/admin`、`internal/audit`、`cmd/hotplex/audit_cmd_test.go` 等用户现有 audit 工作区改动。
- 所有 Shell 命令使用 `rtk` 前缀；所有文档编辑使用 `apply_patch`。

---

### Task 1: 重写终版 ROADMAP 主线

**Files:**
- Modify: `docs/v2/ROADMAP.md`
- Reference: `docs/superpowers/specs/2026-08-04-final-roadmap-documentation-architecture-design.md`

**Interfaces:**
- Consumes: 当前代码/测试确认的 HotPlex 基线、approved specs、Issue #847–#852、#867、#868、#870、#877、#878、#946–#948。
- Produces: 产品定位、事实状态、四阶段路线、当前优先级、成功指标和追踪矩阵的唯一事实源。

- [ ] **Step 1: 固定 ROADMAP 章节结构**

章节按“文档定位 → 产品定位 → 当前已交付能力 → 目标运行时模型 → 设计不变量 → 四阶段路线 → 当前实施优先级 → 暂缓/非目标 → 成功指标 → 追踪矩阵 → 设计依据”排列。

- [ ] **Step 2: 将已完成项改写为当前事实**

把 AgentSpec、AgentIdentity、AEP runtime events、durable ingress、single-active gate、owner lease、ambiguity fence、Audit、OpenTelemetry、Cron 等已交付能力写入“当前已交付能力”，不保留 first cut 或调研形成过程。

- [ ] **Step 3: 将未完成项改写为冻结契约**

把 EffectiveRuntimePlan、observed bootstrap、Gateway-owned EffectLedger、ExecutionQueue、RuntimeContext、Cockpit、Capability Inventory 和 Recipes 写入对应阶段的“冻结契约”和完成定义，不使用“建议、候选、进一步校准”。

- [ ] **Step 4: 合并重复波次和闸门**

删除 `qm 研究终态校准`、`近期优先级校准`、`正交架构闸门` 等独立时间线章节，将有效内容并入目标模型、设计不变量、阶段、优先级和发布闸门。

- [ ] **Step 5: 验证 ROADMAP 事实口径**

Run: `rtk rg -n "本轮|上一轮|下一轮|调研|校准|first cut|候选方向|建议" docs/v2/ROADMAP.md`

Expected: 无匹配。

- [ ] **Step 6: 提交 ROADMAP**

Run: `rtk git add docs/v2/ROADMAP.md && rtk git commit -m 'docs(v2): 定稿 HotPlex 2.0 roadmap'`

### Task 2: 收敛 Implementation Roadmap

**Files:**
- Modify: `docs/v2/IMPLEMENTATION-ROADMAP.md`
- Reference: `docs/v2/ROADMAP.md`

**Interfaces:**
- Consumes: ROADMAP 的四阶段、当前优先级、Issue/spec 映射。
- Produces: 已完成基线、交付切片、依赖图、进入条件、退出条件、验证和回滚要求。

- [ ] **Step 1: 删除重复产品叙事和研究来源**

保留执行原则、依赖、切片和闸门；删除 qm 校准、ROI 推导、产品定位和 Architecture 已定义的组件说明。

- [ ] **Step 2: 将实施内容映射到四阶段**

每阶段固定包含“进入条件、交付物、退出条件、Issue/spec”，并明确已交付项和未完成项。

- [ ] **Step 3: 固定垂直切片顺序**

顺序固定为 EffectiveRuntimePlan/observed bootstrap → Cron/Webhook/message durable effect → bounded ExecutionQueue/RuntimeContext → Cockpit/Recipes/Capability governance。

- [ ] **Step 4: 固定验证与回滚契约**

写入 SQLite/PostgreSQL 成对迁移、四 Worker、AEP 兼容、race/fault-injection、redaction、容量预算、shadow/read-only rollout 和 operator escape hatch。

- [ ] **Step 5: 验证去重**

Run: `rtk rg -n "qm|调研|校准|上一|本轮|下一轮|候选交付|first cut" docs/v2/IMPLEMENTATION-ROADMAP.md`

Expected: 无匹配。

- [ ] **Step 6: 提交实施路线**

Run: `rtk git add docs/v2/IMPLEMENTATION-ROADMAP.md && rtk git commit -m 'docs(v2): 定稿实施路线与交付闸门'`

### Task 3: 将 Architecture 收敛为稳定结构

**Files:**
- Modify: `docs/v2/ARCHITECTURE.md`
- Reference: `docs/v2/ROADMAP.md`
- Reference: `docs/superpowers/specs/2026-08-04-runtime-operations-contract.md`

**Interfaces:**
- Consumes: ROADMAP 的产品边界和 approved specs 的状态模型。
- Produces: 组件职责、canonical owner、数据流、状态关系、兼容边界和反模式。

- [ ] **Step 1: 更新组件事实**

保留 Gateway、Session、Worker、AEP、Event Store、Execution、Audit、Observability；加入 Desired Plan、Gateway Effect、Observed State、Reconciliation 的稳定职责。

- [ ] **Step 2: 固定 canonical owner 表**

明确 authority/scope、desired plan、input execution、Gateway-owned effect、observed state、audit integrity 的唯一写入者和只读消费者。

- [ ] **Step 3: 更新数据流**

Session 初始化和 Turn 执行加入 plan/effect/observed/reconcile 关联，不把 Worker `done` 写成外部成功。

- [ ] **Step 4: 移除阶段计划**

删除 `分阶段落地`，以链接指向 ROADMAP 和 Implementation Roadmap。

- [ ] **Step 5: 验证架构边界**

Run: `rtk rg -n "分阶段|Phase|Wave|调研|校准|建议" docs/v2/ARCHITECTURE.md`

Expected: 无匹配。

- [ ] **Step 6: 提交架构文档**

Run: `rtk git add docs/v2/ARCHITECTURE.md && rtk git commit -m 'docs(v2): 收敛终版运行时架构'`

### Task 4: 将 approved specs 去来源化并冻结契约

**Files:**
- Create: `docs/superpowers/specs/2026-08-04-runtime-operations-contract.md`
- Delete: `docs/superpowers/specs/2026-08-04-qm-inspired-runtime-operations-design.md`
- Create: `docs/specs/Scope-Aware-Capability-Inventory-Spec.md`
- Delete: `docs/specs/QM-Scope-Capability-Model-Spec.md`
- Modify: `docs/specs/README.md`

**Interfaces:**
- Consumes: 现有两个 approved specs 的已冻结语义。
- Produces: 与外部研究来源无关的 Runtime Operations Contract 和 Scope-aware Capability Inventory Contract。

- [ ] **Step 1: 重命名并重写 Runtime Operations spec**

保留 EffectiveRuntimePlan、preflight、EffectLedger、Isolation Capability、Observed State、Reconciliation、架构闸门和完成定义；删除 `QM-inspired`、二阶校准、上一版和研究 issue 叙事。

- [ ] **Step 2: 重命名并重写 Capability spec**

保留 scope precedence、inventory、hash、materialization、redaction、admin-gated promotion、兼容和非目标；qm 只保留在 references。

- [ ] **Step 3: 更新 Specs 索引**

把两份 spec 列入 approved/架构设计分类，删除 `2026-08-04 新增提案` 的过程标题，更新全部路径和状态统计。

- [ ] **Step 4: 验证 spec 终态语言**

Run: `rtk rg -n "QM-inspired|二阶校准|上一版|本轮|下一轮|first cut|新增提案" docs/superpowers/specs/2026-08-04-runtime-operations-contract.md docs/specs/Scope-Aware-Capability-Inventory-Spec.md docs/specs/README.md`

Expected: 无匹配。

- [ ] **Step 5: 提交 approved specs**

Run: `rtk git add docs/superpowers/specs/2026-08-04-runtime-operations-contract.md docs/superpowers/specs/2026-08-04-qm-inspired-runtime-operations-design.md docs/specs/Scope-Aware-Capability-Inventory-Spec.md docs/specs/QM-Scope-Capability-Model-Spec.md docs/specs/README.md && rtk git commit -m 'docs(spec): 冻结运行时与能力契约'`

### Task 5: 将研究报告降为证据附件

**Files:**
- Modify: `docs/research/2026-08-04-qm-hotplex-deep-research-report.md`
- Modify: `docs/research/2026-08-04-qm-hotplex-second-order-calibration.md`
- Modify: `docs/research/2026-08-04-qm-hotplex-orthogonal-architecture-review.md`

**Interfaces:**
- Consumes: 原始源码、测试、Issue、外部规范和 ROI 证据。
- Produces: `final-evidence` 状态的非权威证据附件。

- [ ] **Step 1: 增加统一证据声明**

三份文档 frontmatter 改为 `status: final-evidence`，开头声明最终产品路线和契约以 ROADMAP、ARCHITECTURE、approved specs 为准。

- [ ] **Step 2: 修正旧路径和权威措辞**

所有旧 spec 路径更新为终态路径；研究报告中的“Roadmap 调整、新 spec、issue 处理结果”等发布结果改为“决策映射”，避免其成为第二路线源。

- [ ] **Step 3: 保留证据完整性**

保留 qm commit、源码行号、Issue 反例、动态测试、外部架构依据和 ROI 数据，不把证据附件压缩为结论摘要。

- [ ] **Step 4: 提交研究附件**

Run: `rtk git add docs/research && rtk git commit -m 'docs(research): 固化 qm 证据附件边界'`

### Task 6: 全局交叉引用与终版验证

**Files:**
- Modify: `docs/v2/ROADMAP.md`
- Modify: `docs/v2/IMPLEMENTATION-ROADMAP.md`
- Modify: `docs/v2/ARCHITECTURE.md`
- Modify: `docs/specs/README.md`
- Modify: any documentation file containing an obsolete spec path

**Interfaces:**
- Consumes: Tasks 1–5 的终版路径和事实口径。
- Produces: 无断链、无旧路径、无过程叙事的文档集合。

- [ ] **Step 1: 扫描旧路径和过程词**

Run: `rtk rg -n "2026-08-04-qm-inspired-runtime-operations-design|QM-Scope-Capability-Model-Spec|qm 研究终态校准|二阶校准|上一轮|本轮|下一轮|新增提案" docs`

Expected: 旧路径无匹配；过程词只允许出现在 research 证据正文和本实施计划的禁止词检查说明中。

- [ ] **Step 2: 验证 Markdown 与生成文档**

Run: `rtk git diff --check`

Expected: exit 0。

Run: `rtk make docs-build`

Expected: `Documentation built successfully!` 或缓存命中成功。

- [ ] **Step 3: 验证状态和追踪矩阵**

逐项核对 ROADMAP 中“已交付/已冻结/当前实施/暂缓/非目标”与当前代码、approved specs 和 GitHub Issue open/closed 状态一致。

- [ ] **Step 4: 检查工作树隔离**

Run: `rtk git status --short`

Expected: 用户既有 audit 文件保持原状；本计划只产生文档变更。

- [ ] **Step 5: 提交终版交叉引用**

Run: `rtk git add docs && rtk git commit -m 'docs(v2): 发布终版路线图文档体系'`

### Task 7: 同步相关 GitHub Issues

**Files:**
- Read: `docs/v2/ROADMAP.md`
- Read: `docs/v2/IMPLEMENTATION-ROADMAP.md`
- Read: `docs/superpowers/specs/2026-08-04-runtime-operations-contract.md`
- Read: `docs/specs/Scope-Aware-Capability-Inventory-Spec.md`

**Interfaces:**
- Consumes: 终版路径、四阶段路线、冻结契约、完成标准和 Issue 追踪矩阵。
- Produces: 与终版文档一致的 GitHub Issue 正文和终态评论。

- [ ] **Step 1: 读取相关 Issue 当前状态**

通过 GitHub connector 读取 #851、#867、#868、#870、#946、#947、#948 的 title、state、body、labels 和最新评论；已关闭的 #847–#850、#852、#877、#878 只核对状态，不覆盖已交付记录。

- [ ] **Step 2: 更新冻结契约 Issue**

更新 #946、#947、#948 的权威文档路径、四阶段归属、范围、完成标准和非目标；保持 `open`，明确“设计已冻结、实现尚未完成”。

- [ ] **Step 3: 更新承接 Issue**

更新 #851、#867、#868、#870 的依赖和终态评论，使其分别承接 EffectLedger、EffectiveRuntimePlan/isolation、canonical facts/Cockpit、Recipes/effect contract，不保留 qm 调研时间线。

- [ ] **Step 4: 远端复核**

重新读取 7 个 Issue 和 4 条终态评论，确认路径、状态、依赖、非目标和本地 ROADMAP 一致；不关闭任何实现未完成的 Issue。

- [ ] **Step 5: 记录同步结果**

在最终交付中列出 7 个 Issue 链接、state 和设计状态；外部写入完成以重新读取结果为准，不以 connector 调用返回成功替代远端事实。

- [ ] **Step 6: 推送分支**

Run: `rtk git push origin fix/949-audit-chain-delete-protection`

Expected: pre-push 验证通过；若被用户现有未格式化 audit 文件阻断，保留提交并报告准确阻断文件，不修改无关文件。
