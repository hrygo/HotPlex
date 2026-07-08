# HotPlex 2.0 代码审计

## 审计定位

本审计用于把当前代码实现映射到 HotPlex 2.0 产品路线。2.0 的定位不是重写 Agent OS，而是把现有 Gateway、Session、Worker、AEP、Audit、Observability、EventStore 能力收敛成稳定的 **self-hosted Agent Runtime Gateway**。

2.0 详细文档集中在 [docs/v2](v2/README.md)：

- [产品路线](v2/ROADMAP.md)
- [架构设计](v2/ARCHITECTURE.md)
- [API 设计](v2/API-DESIGN.md)
- [实施路线](v2/IMPLEMENTATION-ROADMAP.md)
- [GitHub 里程碑](v2/GITHUB-MILESTONES.md)

## 当前结论

HotPlex 不是从一个简单 CLI wrapper 起步。当前代码库已经具备 Agent Runtime Gateway 的关键地基：

| 能力 | 当前基础 | 2.0 演进重点 |
| --- | --- | --- |
| Gateway | WebSocket/HTTP session 入口、AEP 分发、worker bridge | 统一 runtime metadata 和 execution correlation |
| Session | 状态机、SQLite/PostgreSQL 持久化、workspace/user 绑定、pool quota | AgentIdentity、execution metadata、context facade |
| Worker | `claude_code`、`opencode_server`、`codex_cli`、`acp` 注册式适配器 | AgentSpec normalized view，不替换 Worker interface |
| AEP | `pkg/events` 作为唯一 wire contract，支持 metadata | 增加 runtime execution events，旧客户端可忽略 |
| EventStore | inbound/outbound events、turns 聚合、synthetic turns | RuntimeContext 的事实源 |
| Audit | message/tool/session/admin 行为审计、hash chain、EventRef | 与 agent/execution/trace 关联 |
| Observability | OTel bootstrap、Prometheus metrics、gateway/session/worker 指标 | 低基数 runtime metrics 和 trace semantic keys |
| Cron | 本地调度、timeout、retry、delivery | 作为未来 scheduler 经验，而非 2.0 先行重写目标 |

核心判断：

> 2.0 的机会不是新建一个平行 Agent OS，而是把现有运行时内核产品化、契约化、可诊断化。

## 架构基线

```text
Client / WebChat / Slack / Feishu / Cron / SDK
        |
        v
Gateway API + AEP Router
        |
        v
Session Runtime Kernel
        |
        +-- Workspace / User / Bot ownership
        +-- Worker lifecycle
        +-- EventStore / Turns
        +-- Audit / Observability
        |
        v
Worker Adapter Registry
        |
        +-- Claude Code
        +-- OpenCode Server
        +-- Codex CLI
        +-- ACP-compatible agents
```

2.0 应在这个架构上补齐：

- AgentSpec resolver。
- AgentIdentity binding。
- Runtime execution metadata。
- RuntimeContext facade。
- Per-session input control。
- Admin runtime diagnostics。

## 缺口分析

| 缺口 | 当前状态 | 高 ROI 做法 | 暂缓项 |
| --- | --- | --- | --- |
| AgentSpec | worker/workspace/work_dir 解析分散在 WS 和 REST 路径 | 先做只读 normalized view，映射到现有 `SessionStartParams` | 不做 registry/marketplace |
| AgentIdentity | session 已有 user/owner/workspace/bot/worker 字段 | 先存入 session `context_json` 并传播到 AEP/audit/trace | 不新建 identity service |
| Runtime events | AEP 已有 event kind 和 metadata | 先加 execution started/completed/failed 和 metadata keys | 不建第二套 event bus |
| Observability | 已有 trace/metrics 基础 | 先补低基数 execution metrics 和 semantic attributes | 不用 raw session/user/execution 做 metrics label |
| Execution control | Handler 直接向 Worker.Input 投递 | 先做 per-session input gate | 不做分布式 scheduler |
| RuntimeContext | eventstore/turns/worker_session_id 已存在 | 先做只读 `Load` facade | 不做独立 memory service |

## 推荐产品迭代波次

### Wave 0: Roadmap Contract Hardening

目标：收敛定位、first cuts、暂缓清单。

状态：文档和 #847-#852 已完成第一轮整理。

### Wave 1: Runtime Correlation

目标：让一次 execution 可解释。

覆盖：

- #847 AgentSpec read-only resolver。
- #848 AgentIdentity in `context_json`。
- #849 execution metadata + runtime events。
- #850 trace/metrics semantic keys。

验收问题：

- 谁发起了这次 session？
- 绑定哪个 workspace？
- 使用哪个 worker？
- 哪次 input 触发了哪些工具？
- 失败发生在 init、worker start、input dispatch、tool call 还是 done/error？

### Wave 2: Runtime Control

目标：让同一 session 的执行顺序和恢复边界可控。

覆盖：

- #851 per-session input gate。
- #852 read-only RuntimeContext facade。

验收问题：

- 同一 session 连续 input 是否可解释、可审计、不会并发 interleave？
- Gateway 重启、worker 崩溃或 resume 时，context 来源和丢失边界是否可说明？

### Wave 3: Runtime Operations

目标：把 2.0 能力转成可运营诊断。

候选：

- Admin runtime diagnostics。
- Runtime health summary。
- Policy decision visibility。

启动条件：Wave 1/2 的 metadata 和 context 边界稳定。

### Wave 4: Agent Platform

目标：在 Runtime Gateway 被验证后再平台化。

候选：

- capability catalog。
- 多 Agent workflow。
- external memory adapters。
- distributed scheduling。

启动条件：真实业务场景证明单 Agent runtime + tools/context 不足够。

## 审计结论

HotPlex 当前已经具备约 60-70% 的 Agent Runtime Gateway 基础。2.0 研发应坚持：

1. 先 runtime contract，再 platform feature。
2. 先 execution correlation，再 workflow orchestration。
3. 先 per-session control，再 distributed scheduler。
4. 先 RuntimeContext facade，再 memory service。
5. 先可运营诊断，再 Agent marketplace。

这个路线务实、稳健，并且最大化复用当前代码资产。
