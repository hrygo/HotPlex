# HOTPLEX 2.0 GitHub Milestones

本文档用于把 2.0 设计文档映射到 GitHub milestone、issue、label 和验收标准。

## 产品路线原则

HotPlex 2.0 的路线以 self-hosted Agent Runtime Gateway 为定位：

- 先做 runtime contract，不先做 marketplace。
- 先做 execution correlation，不先做复杂 workflow。
- 先做 per-session control，不先做分布式 scheduler。
- 先做 RuntimeContext facade，不先做独立 memory service。
- 先做可运营诊断，不先做通用 Agent SaaS。

## Product Waves

| Wave | 名称 | 目标 | GitHub 范围 |
| --- | --- | --- | --- |
| 0 | Roadmap Contract Hardening | 收敛定位、first cuts、暂缓清单 | 当前 2.0 文档 + #847-#852 |
| 1 | Runtime Correlation | 解释一次 execution 的身份、输入、worker、事件、trace、audit | #847, #848, #849, #850 |
| 2 | Runtime Control | 控制同 session 输入顺序和上下文恢复边界 | #851, #852 |
| 3 | Runtime Operations | Admin diagnostics、runtime health、policy decision visibility | Future 2.0.x |
| 4 | Agent Platform | capability catalog、workflow、external memory、distributed scheduling | Future 2.1+ |

## Milestone A: HOTPLEX 2.0 Runtime Contract

目标：先统一运行时契约，不改变现有用户使用方式。

Issues:

- [#847](https://github.com/hrygo/hotplex/issues/847) AgentSpec runtime model
- [#848](https://github.com/hrygo/hotplex/issues/848) Agent identity binding
- [#849](https://github.com/hrygo/hotplex/issues/849) Runtime AEP events
- [#850](https://github.com/hrygo/hotplex/issues/850) Agent runtime tracing/metrics

完成标准：

- AgentSpec 可以由现有 config/init/workspace 自动生成。
- Agent identity 能贯穿 session、event、audit、trace。
- AEP runtime events 与旧客户端兼容。
- Runtime trace 能关联一次 turn 的关键节点。

高 ROI first cuts：

- #847 先交付只读 `AgentSpec` resolver，统一 WS init 和 REST create session 的解析路径。
- #848 先把 `AgentIdentity` 放入 session `context_json` 和 runtime metadata，不急于新增 DB 列。
- #849 先标准化 runtime metadata keys，并增加最小 execution started/completed/failed events。
- #850 先补 runtime span attributes 和低基数 execution metrics。

## Milestone B: HOTPLEX 2.0 Control Plane Foundation

目标：在单机 Gateway 内建立可调度、可恢复、可观测的执行控制基础。

Issues:

- [#851](https://github.com/hrygo/hotplex/issues/851) Execution queue abstraction
- [#852](https://github.com/hrygo/hotplex/issues/852) Runtime context persistence interface

完成标准：

- 单 session FIFO execution。
- timeout/retry/crash 与现有 Bridge 行为兼容。
- RuntimeContext 从 eventstore/turns/worker history 读取事实。
- queue/context 都有 race coverage。

高 ROI first cuts：

- #851 先做 per-session input gate + `execution_id` 状态跟踪，再扩展完整 ExecutionQueue。
- #852 先做只读 `RuntimeContext.Load` facade，数据源限定为 eventstore、turns、worker_session_id 和 workspace metadata。

## Future Milestone C: HOTPLEX 2.1 Agent Platform

候选 issue 方向：

- PolicyEngine：复用 permission mode、allowed tools、workspace sandbox、audit。
- Agent registry：基于 AgentSpec 的版本化声明和 capability catalog。
- Workflow orchestration：manager/reviewer/tester 多 Agent workflow。
- External memory backend：project/skill/knowledge memory adapter。
- Admin runtime diagnostics：查询 agent、execution、context、policy decisions。

启动条件：

- Milestone A/B 完成。
- Runtime events 和 trace 已能提供足够诊断信息。
- ExecutionQueue 与 RuntimeContext 边界稳定。
- #847-#852 的 first cuts 已经在生产或长期测试环境中验证。
- 至少一个真实业务场景证明单 Agent runtime + tools/context 已不足够。

## Future Milestone D: HOTPLEX 3.0 Agent OS

候选方向：

- Distributed scheduler。
- Enterprise RBAC and compliance export。
- Kubernetes/operator deployment。
- Agent marketplace。
- Cross-cluster runtime federation。

启动条件：

- 单机 Control Plane 已被生产验证。
- 多 Agent workflow 有真实使用案例。
- 企业安全和审计模型稳定。

## Label Scheme

现有必备标签：

- `roadmap/v2`
- `runtime`
- `architecture`
- `observability`
- `agent-os`

建议新增或保持：

- `scheduler`：ExecutionQueue、task dispatch、resource scheduling。
- `memory`：RuntimeContext、future memory backend。
- `security`：policy、identity、audit、permission。

## Issue 模板

每个 2.0 issue 应包含：

```markdown
## Goal

一句话说明该 issue 对 2.0 roadmap 的贡献。

## Current baseline

列出现有代码能力，避免重复造系统。

## Scope

- 可交付项 1
- 可交付项 2
- 可交付项 3

## Non-goals

- 明确不做的内容

## Acceptance criteria

- [ ] 代码验收
- [ ] 测试验收
- [ ] 文档验收

## References

- docs/v2/ROADMAP.md
- docs/v2/ARCHITECTURE.md
- docs/v2/API-DESIGN.md
- docs/v2/IMPLEMENTATION-ROADMAP.md
```

## 当前 Issue 映射

| Issue | Milestone | Labels | 依赖 | 说明 |
| --- | --- | --- | --- | --- |
| #847 | Runtime Contract | `roadmap/v2`, `runtime`, `architecture` | 无 | 先定义 normalized AgentSpec |
| #848 | Runtime Contract | `roadmap/v2`, `runtime`, `architecture`, `security` | #847 | 绑定 user/workspace/bot/platform/worker |
| #849 | Runtime Contract | `roadmap/v2`, `runtime`, `architecture`, `observability` | #847, #848 | 增加 runtime/security/context event |
| #850 | Runtime Contract | `roadmap/v2`, `runtime`, `observability` | #849 | trace/metrics 复用现有 OTel |
| #851 | Control Plane Foundation | `roadmap/v2`, `runtime`, `scheduler`, `agent-os` | #847 | 单 session FIFO execution |
| #852 | Control Plane Foundation | `roadmap/v2`, `runtime`, `memory`, `agent-os` | #848, #849 | RuntimeContext 抽象 |

## 暂缓清单

这些方向在 #847-#852 的 first cuts 完成前不启动：

- 分布式 scheduler。
- 独立 memory service 或 vector store。
- Agent registry/marketplace。
- 复杂策略语言。
- 多 Agent workflow 编排。
