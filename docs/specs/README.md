# Specs 目录索引

> 规范文档集中管理 — 设计规格、验收标准、跟踪矩阵

## 文档索引

### 架构设计规格

| 文档 | 描述 | 状态 | 日期 | 进度 |
|------|------|------|------|------|
| [Gateway-Async-Init-Spec.md](./Gateway-Async-Init-Spec.md) | Gateway 异步初始化 — Session Start 异步化设计 | draft | 2026-04-04 | 0% |
| [ACP-Worker-Spec.md](./ACP-Worker-Spec.md) | ACP Worker 集成规格 — 通用 ACP Agent 对接，Hermes 试点 | proposed | 2026-05-29 | 0% |
| [Worker-User-Interaction-Spec.md](./Worker-User-Interaction-Spec.md) | Worker 用户交互集成 — 权限请求/问题询问/MCP Elicitation 转发与响应 | implemented | 2026-04-19 | 95% |
| [Feishu-Adapter-Improvement-Spec.md](./Feishu-Adapter-Improvement-Spec.md) | Feishu Adapter 改进规格 — 流式卡片、访问控制、多消息类型 | in-progress | 2026-04-17 | 50% |
| [GroupChat-Collaboration-Spec.md](./GroupChat-Collaboration-Spec.md) | 群聊多 Bot 协作 — 飞书/Slack 群组中多 Bot 协同讨论与任务分配 | proposed | 2026-06-03 | 0% |
| [Dual-Database-Support-Spec.md](./Dual-Database-Support-Spec.md) | 双数据库支持 — SQLite + PostgreSQL 并存方案 | proposed | 2026-05-26 | 0% |
| [Consolidate-Events-Store-Spec.md](./Consolidate-Events-Store-Spec.md) | 事件存储合并 — 统一 EventStore 架构 | proposed | - | - |
| [Delta-Optimization-Spec.md](./Delta-Optimization-Spec.md) | Delta 优化 — 增量消息压缩与合并策略 | proposed | - | - |
| [Interaction-Response-Chain-Fix-Spec.md](./Interaction-Response-Chain-Fix-Spec.md) | 交互响应链修复 — 权限/Q&A 响应路由重构 | proposed | - | - |
| [Inbound-Event-Storage-Fix-Spec.md](./Inbound-Event-Storage-Fix-Spec.md) | 入站事件存储修复 — 事件持久化一致性 | proposed | 2026-05-07 | - |
| [Hot-Reload-Spec.md](./Hot-Reload-Spec.md) | 配置热重载修复 — 配置变更即时生效 | draft | 2026-04-22 | 20% |
| [Gateway-Self-Restart-Spec.md](./Gateway-Self-Restart-Spec.md) | 飞书特权命令安全重启 Gateway — Gateway 直拦截、专用授权、原子租约与版本化生命周期回执 | proposed | 2026-08-24 | 0% |
| [Multi-Bot-Support-Spec.md](./Multi-Bot-Support-Spec.md) | 多 Bot 支持 — 多 bot 实例同时运行与隔离 | implemented | 2026-05-13 | 100% |
| [agent-config-injection-control.md](./agent-config-injection-control.md) | Agent 配置注入排除 — inject_exclude 3 级回退机制 | implemented | 2026-05-31 | 100% |
| [pr-review-webhook-driven.md](./pr-review-webhook-driven.md) | PR Review Webhook 触发 — GitHub Webhook 驱动自动化代码审查 | implemented | 2026-05-30 | 90% |
| [Turns-Materialized-Table-Spec.md](./Turns-Materialized-Table-Spec.md) | Turns 物化表 — eventstore 物化为独立表提升查询性能 | proposed | 2026-05-19 | 0% |
| [API-Documentation-Hybrid-Generation-Spec.md](./API-Documentation-Hybrid-Generation-Spec.md) | API 文档混合生成 — 手写 + 自动生成方案降低维护风险 | proposed | 2026-06-03 | 0% |
| [Observability-Spec.md](./Observability-Spec.md) | 统一可观测性体系 — OTel Native 架构，70 指标，Tracing，告警，SLO | proposed | 2026-06-04 | 0% |
| [Scope-Aware-Capability-Inventory-Spec.md](./Scope-Aware-Capability-Inventory-Spec.md) | Scope-aware capability inventory、precedence、safe materialization 与 enforcement contract | approved | 2026-08-04 | 设计已冻结 |
| [WebChat-Multitenancy-Foundation-Design-Spec.md](./WebChat-Multitenancy-Foundation-Design-Spec.md) | WebChat 多租户地基（spec ①） — 身份/workspace/隔离/配额后端地基设计 | draft | 2026-06-15 | 0% |
| [WebChat-Multitenancy-Roadmap-Spec.md](./WebChat-Multitenancy-Roadmap-Spec.md) | WebChat 多租户路线图 — 后续 spec ②-⑥ 阶段规划与依赖编排 | proposed | 2026-06-15 | 0% |
| [WebChat-Workspace-Create-WorkDir-Prefix-Spec.md](./WebChat-Workspace-Create-WorkDir-Prefix-Spec.md) | WebChat Workspace 新建入口 + work_dir 沙箱前缀约束 — 沙箱定义已由 Root-HotplexHome spec 修订 | 设计已认可 | 2026-06-24 | 待实现 |
| [WebChat-Workspace-Root-HotplexHome-Spec.md](./WebChat-Workspace-Root-HotplexHome-Spec.md) | WebChat Workspace 沙箱根跟随 HOTPLEX_HOME — 沙箱 base 收敛 + username 目录段（P1 防碰撞/P2 不可变/P3 grandfather）+ workspace_root API + 前端动态根 | draft | 2026-08-13 | 0% |

### Worker 与 Session

| 文档 | 描述 | 状态 | 日期 | 进度 |
|------|------|------|------|------|
| [Codex-AppServer-Worker-Spec.md](./Codex-AppServer-Worker-Spec.md) | Codex App-Server Worker — 持久化进程模式，真正流式输出 | draft | 2026-05-18 | 0% |
| [Worker-GAP-Analysis-2026-05-15.md](./Worker-GAP-Analysis-2026-05-15.md) | Worker GAP 分析 — OCS vs CC Worker 能力差距与实施计划 | active | 2026-05-15 | 0% |
| [Session-History-Persistence-Spec.md](./Session-History-Persistence-Spec.md) | Session 历史持久化 — 会话重放与历史查询 | draft | 2026-04-28 | 40% |
| [Per-Bot-Agent-Config-Spec.md](./Per-Bot-Agent-Config-Spec.md) | Per-Bot Agent 配置 — 独立 Bot 人格与上下文 | implemented | 2026-05-03 | 100% |
| [Turn-Summary-Spec.md](./Turn-Summary-Spec.md) | Turn 摘要 — 消息轮次总结与统计 | draft | 2026-05-01 | 70% |
| [Turn-Summary-WorkDir-Fix-Spec.md](./Turn-Summary-WorkDir-Fix-Spec.md) | Turn 摘要 WorkDir 修复 — 缺失工作目录字段 | implemented | 2026-05-04 | 100% |
| [OCS-Production-Readiness-Spec.md](./OCS-Production-Readiness-Spec.md) | OCS Worker 生产级完善 — 从开发到生产级可靠性提升 | approved | 2026-05-15 | 80% |
| [ACP-Worker-Enhancement-Spec.md](./ACP-Worker-Enhancement-Spec.md) | ACP Worker 全面增强 — 从可用到生产首选的改进路径 | verified | 2026-05-30 | 0% |
| [Codex-Reset-Zombie-Fix-Spec.md](./Codex-Reset-Zombie-Fix-Spec.md) | Codex Worker Reset/Zombie 修复 — reset 与 zombie bug 修复 | draft | 2026-05-30 | 0% |
| [codexcli-full-upgrade-spec.md](./codexcli-full-upgrade-spec.md) | Codex CLI 全功能升级 — 与上游 Codex CLI 100% 协议覆盖 | draft | 2026-05-31 | 0% |

### 平台适配

| 文档 | 描述 | 状态 | 日期 | 进度 |
|------|------|------|------|------|
| [Slack-CLI-Subcommand-Spec.md](./Slack-CLI-Subcommand-Spec.md) | Slack CLI 子命令规格 — 独立 CLI 操作 Slack | draft | 2026-05-03 | - |
| [Slack-Stream-Rotation-Spec.md](./Slack-Stream-Rotation-Spec.md) | Slack 流式旋转 — TTL 超时自动续流 | implemented | 2026-05-05 | 90% |
| [Feishu-Card-Header-Spec.md](./Feishu-Card-Header-Spec.md) | 飞书卡片 Header 增强 — 消息卡片 Header 区域 v2.0 设计 | draft | 2026-05-08 | 0% |
| [Feishu-Interactive-Card-Buttons-Spec.md](./Feishu-Interactive-Card-Buttons-Spec.md) | 飞书交互式卡片按钮 — 权限确认/提问/信息采集 (cardkit-v2) | draft | 2026-05-20 | 0% |
| [slack-block-kit-upgrade.md](./slack-block-kit-upgrade.md) | Slack Block Kit 升级 — slack-go v0.24.0 新 Block Kit 类型适配 | implemented | 2026-05 | 85% |

### 定时任务

| 文档 | 描述 | 状态 | 日期 | 进度 |
|------|------|------|------|------|
| [AI-Native-Cronjob-Spec.md](./AI-Native-Cronjob-Spec.md) | AI 原生定时任务 — Worker 自管理调度系统 | implemented | 2026-05-09 | 100% |
| [Cron-Fast-Path-Spec.md](./Cron-Fast-Path-Spec.md) | Cron Fast Path — 会话内回调机制 | draft | 2026-05-11 | 0% |
| [Cron-Delivery-Retry-Spec.md](./Cron-Delivery-Retry-Spec.md) | Cron 投递重试 — 执行结果消息投递重试机制 | draft | 2026-05-30 | 0% |

### CLI 与 Onboard

| 文档 | 描述 | 状态 | 日期 | 进度 |
|------|------|------|------|------|
| [CLI-Self-Service-Spec.md](./CLI-Self-Service-Spec.md) | CLI 自服务规格 — doctor/onboard/status 子命令 | implemented | 2026-04-22 | 95% |
| [Onboard-UX-Improvement-Spec.md](./Onboard-UX-Improvement-Spec.md) | Onboard UX 改进 — 向导式引导流程 | draft | 2026-05-06 | 10% |
| [Onboard-Go-Embed-AST.md](./Onboard-Go-Embed-AST.md) | Onboard Go:Embed AST 重构 — 模板嵌入优化 | proposed | 2026-04-25 | 0% |

### 前端与平台

| 文档 | 描述 | 状态 | 日期 | 进度 |
|------|------|------|------|------|
| [WebChat-v2-Revamp-Spec.md](./WebChat-v2-Revamp-Spec.md) | WebChat v2 改版 — 产品愿景与技术路线 | proposed | 2026-04-20 | 0% |
| [WebChat-i18n-Bilingual-Spec.md](./WebChat-i18n-Bilingual-Spec.md) | WebChat 中英双语切换 — 客户端运行时 i18n 方案（i18next + react-i18next） | proposed | 2026-06-30 | 0% |
| [TTS-Engine-Spec.md](./TTS-Engine-Spec.md) | TTS 引擎规格 — Edge-TTS + Kokoro 语音合成 | draft | 2026-05-07 | 15% |
| [2026-04-29-windows-support.md](./2026-04-29-windows-support.md) | Windows 平台支持 — 跨平台兼容规格 | proposed | 2026-04-29 | 30% |

### 验收标准与跟踪

| 文档 | 描述 | 状态 |
|------|------|------|
| [Acceptance-Criteria.md](./Acceptance-Criteria.md) | 157 条验收标准完整定义 | draft |
| [AC-Tracking-Matrix.md](./AC-Tracking-Matrix.md) | 验收标准跟踪矩阵（Markdown） | active |
| [AC-Tracking-Matrix.csv](./AC-Tracking-Matrix.csv) | 验收状态跟踪矩阵（CSV） | active |
| [TRACEABILITY-MATRIX.md](./TRACEABILITY-MATRIX.md) | 功能实现与代码溯源矩阵 | active |

---

## HotPlex 2.0 Approved Contracts

| 文档 | 权威职责 | 状态 |
| --- | --- | --- |
| [Scope-Aware-Capability-Inventory-Spec.md](./Scope-Aware-Capability-Inventory-Spec.md) | Capability inventory、precedence、materialization 和 enforcement | approved |
| [Runtime Operations Contract](../superpowers/specs/2026-08-04-runtime-operations-contract.md) | EffectiveRuntimePlan、EffectLedger、observed state 和 reconciliation | approved |
| [HotPlex 2.0 Roadmap](../v2/ROADMAP.md) | 产品定位、当前事实、阶段、优先级和完成定义 | final |
| [HotPlex 2.0 Architecture](../v2/ARCHITECTURE.md) | 组件、事实所有权、数据流和兼容边界 | final |

---

## 关联规范文档

### 架构设计

- [`../architecture/AEP-v1-Protocol.md`](../architecture/AEP-v1-Protocol.md), [`../architecture/AEP-v1-Appendix.md`](../architecture/AEP-v1-Appendix.md)
- [`../architecture/Worker-Gateway-Design.md`](../architecture/Worker-Gateway-Design.md), [`../architecture/Message-Persistence.md`](../architecture/Message-Persistence.md)

### 安全设计

- [`../security/Security-Authentication.md`](../security/Security-Authentication.md)
- [`../security/SSRF-Protection.md`](../security/SSRF-Protection.md)
- [`../security/Env-Whitelist-Strategy.md`](../security/Env-Whitelist-Strategy.md)
- [`../security/AI-Tool-Policy.md`](../security/AI-Tool-Policy.md)
- [`../security/Security-InputValidation.md`](../security/Security-InputValidation.md)

### 管理设计

- [`../management/Admin-API-Design.md`](../management/Admin-API-Design.md)
- [`../management/Config-Management.md`](../management/Config-Management.md)
- [`../management/Observability-Design.md`](../management/Observability-Design.md)
- [`../management/Resource-Management.md`](../management/Resource-Management.md)

### 测试策略

- [`../testing/Testing-Strategy.md`](../testing/Testing-Strategy.md)

---

## 已归档

12 个已实现 spec + 4 个过时文件已移至 [`../archive/specs/`](../archive/specs/)。详见 [`../archive/README.md`](../archive/README.md)。
