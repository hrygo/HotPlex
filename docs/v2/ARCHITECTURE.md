# HOTPLEX 2.0 Architecture

## 架构目标

HotPlex 2.0 的目标架构是一个渐进式 Runtime Platform：

```text
Client / Bot / WebChat / Cron
        |
        v
Gateway API + AEP Router
        |
        v
Session Runtime Kernel
        |
        +--> AgentSpec Resolver
        +--> Agent Identity Binder
        +--> Execution Queue
        +--> Policy Hook
        +--> Runtime Context
        |
        v
Worker Adapter Registry
        |
        +--> claude_code
        +--> opencode_server
        +--> codex_cli
        +--> acp
        |
        v
Process / App Server / JSON-RPC / Provider Runtime
```

这不是新建一套 control plane 取代 Gateway，而是在 Gateway + Session + Worker 之间补齐 2.0 所需的标准契约。

## 架构定位

2.0 的控制面是 **single-node runtime control plane**，不是分布式集群控制面。

它负责：

- 解析 AgentSpec。
- 绑定 AgentIdentity。
- 标准化 runtime metadata。
- 控制同 session 的 input dispatch。
- 暴露 runtime context 和诊断事实。

它暂不负责：

- 跨机器调度。
- Agent registry/marketplace。
- Workflow DAG 编排。
- 外部 memory backend。
- 自定义策略语言。

这个边界参考成熟平台的演进经验：先用稳定 API 和状态模型约束本地运行时，再在真实调度压力出现后引入分布式 controller。

## 当前架构事实

| 模块 | 现状 | 约束 |
| --- | --- | --- |
| `internal/gateway` | AEP 分发、WebSocket 连接、worker bridge、history recovery、LLM retry、audit emission | 2.0 API 必须复用 Handler/Bridge 生命周期 |
| `internal/session` | 状态机、store、pool、workspace quota、user ownership | Agent identity 只能扩展 Session metadata，不能旁路状态机 |
| `internal/worker` | 注册式 Worker 接口和 SessionInfo，适配 Claude Code/OpenCode/Codex/ACP | AgentSpec 应映射到 SessionInfo 和 worker config |
| `pkg/events` | AEP v1 事件、Envelope.Metadata、内部 OwnerID | Runtime event 必须仍是 AEP Kind/Data 扩展 |
| `internal/eventstore` | 事件捕获、turns 聚合、synthetic turn | RuntimeContext 先从这里读取事实，不新建 memory DB |
| `internal/observability` | OTel 初始化、Prometheus handler、39 类指标工具函数 | Agent tracing 复用现有 meter/tracer lifecycle |
| `internal/audit` | user activity 与 tool call audit | Policy/security events 复用 audit collector |
| `internal/cron` | 本地 scheduler、timeout、retry、delivery | ExecutionQueue 可借鉴 cron 的 timeout/retry 模型 |

## 目标组件

### AgentSpec Resolver

职责：

- 将 YAML/config/env/init metadata 解析为统一 `AgentSpec`。
- 保持现有 `worker_type`、allowed tools、permission mode、sandbox、budget 配置兼容。
- 输出可审计的 normalized spec，供 Session 和 Worker 使用。

边界：

- 不替代 `internal/config`。
- 不直接启动 worker。
- 不读取业务消息内容。
- 第一版是只读 normalized view，不新增持久化字段。

### Agent Identity Binder

职责：

- 将 user、workspace、bot、platform、agent name、runtime provider 绑定到 session。
- 为 AEP metadata、audit、eventstore、trace 提供一致 identity。
- 支持匿名用户和历史 session 的兼容迁移。

边界：

- 不新建独立 identity service。
- 不绕过现有 workspace owner 校验。

### Execution Queue

职责：

- 在 Session 与 Worker Input 之间提供 ordered task dispatch。
- 记录 task id、attempt、timeout、retry reason、worker execution metadata。
- 暴露队列状态给 observability 和 admin diagnostics。

边界：

- 第一版只做单 session / 单 worker 顺序队列。
- 第一刀先实现 per-session input gate。
- 不做跨节点调度。
- 不改变现有输入事件和 worker stream 输出语义。

### Policy Hook

职责：

- 对工具、文件系统、网络、预算、permission mode 做统一检查入口。
- 将 allow/deny/escalate 结果写入 audit 和 runtime events。

边界：

- 第一版复用现有 permission mode、allowed/disallowed tools、workspace path validation。
- 不引入复杂策略语言。

### Runtime Context

职责：

- 将 session history、turns、worker internal session id、workspace context 抽象为上下文接口。
- 为 resume、fork、summary recovery、future memory backend 提供统一读取路径。

边界：

- 第一版不建设独立 Memory Service。
- 第一刀只提供 `Load` facade。
- eventstore 和 worker provider history 仍是事实源。

### Runtime Observability

职责：

- 将 init、session create、worker start、input dispatch、tool call、done/error 串成 trace。
- 为 runtime events 增加 trace id/span id metadata。
- 暴露 agent/runtime 维度 metrics。

边界：

- 复用 `internal/observability.Init` 的 shutdown 和 noop fallback。
- 不从应用层直接 import Prometheus。

## 数据流

### Session 初始化

```text
client init
  -> gateway validates auth/workspace
  -> AgentSpec Resolver normalizes config
  -> Agent Identity Binder attaches identity
  -> session.Manager creates or resumes session
  -> worker.Registry validates worker type
  -> bridge starts worker
  -> AEP init_ack + runtime events
```

### Turn 执行

```text
input event
  -> AEP validation
  -> Policy Hook
  -> Execution Queue enqueue
  -> Worker.Input
  -> bridge.forwardEvents
  -> eventstore capture
  -> observability spans/metrics
  -> audit for user/tool/security decisions
```

### Context 恢复

```text
resume request
  -> session lookup
  -> RuntimeContext reads eventstore/turns/provider session id
  -> worker-specific adapter reconstructs history
  -> session transitions to RUNNING or IDLE
```

## 兼容策略

| 兼容面 | 策略 |
| --- | --- |
| AEP v1 | 新事件只增不改，旧客户端可忽略未知 event kind |
| Config | `worker_type` 和现有 worker config 保留，AgentSpec 是 normalized view |
| Session store | 新字段使用可空字段或 JSON metadata，迁移必须兼容旧记录 |
| Worker | 通过 SessionInfo 扩展，不修改 Worker 接口的核心输入输出语义 |
| Observability | tracing disabled 时保持 noop，不影响 gateway 启动 |

## 反模式

- 在 `internal/runtime` 新建一套独立 session/worker 生命周期。
- 为 runtime events 引入 Kafka/NATS 等外部 event bus。
- 将 AgentSpec 做成只有 WebChat 使用的前端模型。
- 在 ExecutionQueue 内直接理解 Claude/OpenCode/Codex 的私有协议。
- 为 Memory Service 新建数据事实源，导致 eventstore 和 turns 失去权威性。

## 分阶段落地

| 阶段 | Issue | 架构增量 |
| --- | --- | --- |
| Runtime Contract | #847 | AgentSpec Resolver |
| Runtime Contract | #848 | Agent Identity Binder |
| Runtime Contract | #849 | Runtime AEP events |
| Runtime Contract | #850 | Runtime tracing/metrics |
| Control Plane | #851 | Execution Queue |
| Control Plane | #852 | Runtime Context |
