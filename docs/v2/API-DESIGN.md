# HOTPLEX 2.0 API Design

## 设计目标

HotPlex 2.0 API 提供稳定的 Agent Runtime Contract，同时保持现有 Gateway、Session、Worker 和 AEP v1 兼容。

API 分为三层：

1. **内部 Go contract**：Session/Worker/Gateway 之间共享的数据模型和接口。
2. **AEP runtime events**：客户端、bot、WebChat 可观测的运行时事件。
3. **HTTP/Admin API 扩展**：面向管理和诊断的查询接口。

## API 稳定性策略

2.0 第一阶段优先稳定内部 contract 和 AEP metadata，不急于公开完整 Runtime HTTP API。

| 层级 | 稳定策略 | 第一阶段交付 |
| --- | --- | --- |
| Internal Go contract | 可以小步演进，但必须有兼容测试 | AgentSpec resolver、AgentIdentity、ExecutionID、RuntimeContext facade |
| AEP wire contract | 只增不改，旧客户端可忽略未知 event kind | runtime metadata keys、execution started/completed/failed |
| Admin HTTP API | 等 metadata/context 稳定后再公开 | 先保留候选设计，不作为 first cut |

高基数值如 raw `session_id`、`user_id`、`execution_id` 可以进入 trace/event/audit，但不应作为 metrics label。

## AgentSpec

AgentSpec 是现有配置的标准化视图，不是新的配置系统。

```go
type AgentSpec struct {
    Name        string
    Provider    string
    WorkerType  worker.WorkerType
    WorkspaceID string
    WorkDir     string
    Sandbox     SandboxSpec
    Policy      PolicySpec
    Budget      BudgetSpec
    Metadata    map[string]any
}
```

### YAML 形态

```yaml
agent:
  name: coding-agent

runtime:
  worker_type: codex_cli
  provider: codex

workspace:
  id: ws_123
  work_dir: /repo

sandbox:
  mode: workspace-write

policy:
  permission_mode: workspace
  allowed_tools:
    - git
  disallowed_tools:
    - shell:rm

budget:
  max_turns: 20
  max_budget_usd: 5.0
```

### 与现有配置映射

| AgentSpec 字段 | 当前来源 |
| --- | --- |
| `Name` | bot name、workspace name、init metadata，缺省为 worker type |
| `Provider` | worker-specific config 或 provider adapter 名称 |
| `WorkerType` | `worker_type`，支持 `claude_code`/`opencode_server`/`codex_cli`/`acp` |
| `WorkspaceID` | WebChat init 或 workspace API |
| `WorkDir` | init metadata、workspace work_dir、worker default_work_dir |
| `Sandbox.Mode` | Codex sandbox、workspace permission mode、path validation 结果 |
| `Policy.PermissionMode` | worker default、workspace override、bot/platform override |
| `Policy.AllowedTools` | init config、worker SessionInfo、provider-specific adapter |
| `Budget.MaxTurns` | SessionInfo/worker config |
| `Budget.MaxBudgetUSD` | Claude Code/Codex budget 参数 |

### First-Cut 实现状态（#847）

#847 交付了上述模型的**第一个落地切片**（`internal/agentspec` 包），是 v2 依赖链的根。它与本节"理想形态"的差异和边界如下：

- **结构**：first-cut 采用 `AgentSpec = WorkerSpec + PolicySpec + SandboxSpec + BudgetSpec + IdentityRefs`（只读、secret-free）。`Name`/`Provider`/`Metadata` 尚未引入；`Identity` 仅持 ID 引用（`AgentIdentity` 值对象是 #848）。
- **归一化器**：`agentspec.Resolver.Resolve(Input) (AgentSpec, error)` 为纯函数（worker_type 校验可注入，permission tier 校验为静态映射），便于表驱动测试与 WS≡REST 等价性证明。
- **worker_type**：messaging 平台走 `config.ResolveWorkerType(platform, botName)` 的 5 级 fallback（per-bot → platform YAML/env → messaging 共享默认 → 编译默认 `claude_code`）；**WebChat 为请求驱动，不走 config fallback**（body/query → workspace.WorkerPreference → default）。
- **映射**：`MapToStartParams` / `MapToSessionInfo` 幂等覆盖现有 `worker.SessionStartParams` / `worker.SessionInfo`，不改 Worker 接口。字段所有权见设计 spec §3.4.1。
- **接入范围（F8）**：first-cut 仅把 `MapToStartParams` 接入 **WebChat 入口层**（WS init + REST create），且以 **shadow 模式**运行（旁路对比旧构造、记录 diff，旧路径仍为权威，零行为变更）。`SessionInfo` 由 bridge 层 `buildWorkerInfo` 构造、服务所有会话路径，故 `MapToSessionInfo` first-cut **仅提供+单测（契约），不接入生效路径**——bridge 接入为后续 slice。
- **明确不做（first-cut）**：不注入 `AllowedModels`（F1，行为变更）；不在 agentspec 内重写 permission tier→worker 原生参数映射（F2，保留在各 adapter 内联）；不改 messaging/cron/admin 路径。
- 详见设计 spec：`docs/superpowers/specs/2026-07-21-agentspec-runtime-model-design.md`（含独立审查修订记录 F1-F8）。

## Agent Identity

Agent identity 绑定在 session 上，并传播到 event、audit、trace。

```go
type AgentIdentity struct {
    AgentID     string
    AgentName   string
    UserID      string
    WorkspaceID string
    BotID       string
    Platform    string
    Provider    string
    WorkerType  worker.WorkerType
}
```

规则：

- `UserID` 继续使用当前 auth/session owner。
- `WorkspaceID` 必须通过现有 owner 校验。
- `AgentID` 可由 `userID + workspaceID + agent name + worker type` 派生，避免引入全局 registry。
- 匿名 session 继续兼容，但 runtime events 必须标记 anonymous identity。

### First-Cut 实现状态（#848）

#848 交付了 AgentIdentity 的**值对象 + 持久化 + 三面传播**（`internal/agentspec/identity.go` + `internal/session` 接入）。它落实了上述规则，并以"不改 wire、不加迁移、零行为回退"为约束：

- **值对象**：`agentspec.AgentIdentity`（secret-free）+ `DeriveAgentID`（UUIDv5，独立子命名空间，与 session-key 命名空间隔离）+ `BuildAgentIdentity`（AgentName→BotName→WorkerType 回退、显式 `Anonymous` 标记）+ `IsAnonymousUser`。纯函数，表驱动测试覆盖确定性/离散性/匿名/无密钥不变量。
- **持久化（无迁移）**：身份折叠进**现有** `context_json TEXT` 列（SQLite + PG），reserved key `_agent_identity`。marshal 时折叠（**不改动内存 Context**，故 `/reset` 清空 Context 后身份仍存活并在下次 Upsert 重新持久化）；scan 时弹出到类型化字段，空 map 归一为 nil。遗留行（无该 key）原样可读，`Identity == nil`。
- **绑定**：`bindIdentity` 在 `CreateWithBot`（覆盖 platform/cron/messaging）与 `SetWorkspaceID`（覆盖 WebChat 创建后绑定 workspace）处（重新）派生——总从当前字段重派生，使晚到 workspace 纠正首次无 workspace 的身份。`SessionInfo.EffectiveIdentity()` 对遗留 session 从字段派生等价身份（确定性 AgentID，与新建一致）。
- **三面统一传播**（统一 key：`agent_id` / `worker_type` / `user_id` / `workspace_id` / `platform`；`agent_id` 为主关联键，恒在）：
  - **AEP metadata**：`init_ack` 经 `StampIdentityMetadata` 盖章，客户端从首帧即可按 `agent_id` 关联。
  - **audit detail_json**：`session.AuditDetailFields`（统一 key + `bot_id`，空值省略），接入 manager 的 create/delete 与 REST API delete（`api.go`）。
  - **trace attributes**：`bridge.forward_events` span（高基数键作 trace 属性，**不作 metric label**；`worker_type`/`platform` 低基数）。
- **无密钥不变量**：结构体拒绝 token/secret/credential/apikey 字段（反射断言）；序列化不含 provider token。
- **明确不做（first-cut）**：不新增专用 DB 列（直到查询需求证明，否则沿用 context_json）；不对每个转发 envelope 注入身份（init_ack 握手期携带即可，服务端关联由 trace/audit 覆盖）；不以身份 key 作 metric label（高基数）；`Provider` best-effort 留空；不接入 `AllowedModels`（属 #847 F1）。现有 owner/workspace 校验仍是权威，身份仅作观测/关联视图。

## Runtime Service Contract

内部 contract 应放在 Gateway/Session 能复用的位置，第一版可以是薄封装。

```go
type RuntimeService interface {
    ResolveSpec(ctx context.Context, req InitRequest) (*AgentSpec, error)
    BindIdentity(ctx context.Context, spec *AgentSpec, auth AuthContext) (*AgentIdentity, error)
    Start(ctx context.Context, sessionID string, spec *AgentSpec, identity *AgentIdentity) error
    Dispatch(ctx context.Context, sessionID string, input RuntimeInput) error
    Stop(ctx context.Context, sessionID string, reason string) error
}
```

非目标：

- 不取代 `session.Manager`。
- 不绕过 `worker.Registry`。
- 不改变 AEP init/input/done 的客户端语义。

## Runtime Events

AEP v1 继续是唯一 wire format。新增事件通过 `events.Kind` 和 typed Data 扩展。

### 建议事件

```text
agent.spec.resolved
agent.identity.bound
runtime.execution.queued
runtime.execution.started
runtime.execution.retrying
runtime.execution.timeout
runtime.execution.completed
runtime.execution.failed
security.policy.checked
context.loaded
context.saved
```

### Event Data 示例

```go
type RuntimeExecutionData struct {
    ExecutionID string         `json:"execution_id"`
    AgentID     string         `json:"agent_id,omitempty"`
    WorkerType  string         `json:"worker_type"`
    Attempt     int            `json:"attempt"`
    TimeoutMs   int64          `json:"timeout_ms,omitempty"`
    Reason      string         `json:"reason,omitempty"`
    Metadata    map[string]any `json:"metadata,omitempty"`
}
```

```go
type PolicyDecisionData struct {
    AgentID    string         `json:"agent_id,omitempty"`
    Action     string         `json:"action"`
    Resource   string         `json:"resource,omitempty"`
    Decision   string         `json:"decision"`
    Reason     string         `json:"reason,omitempty"`
    Metadata   map[string]any `json:"metadata,omitempty"`
}
```

### Metadata 要求

`events.Envelope.Metadata` 应承载：

- `trace_id`
- `span_id`
- `agent_id`
- `execution_id`
- `workspace_id`
- `worker_type`

旧客户端可以忽略这些字段。

语义：

| Key | 用途 | 约束 |
| --- | --- | --- |
| `trace_id` | 关联 OTel trace 与 AEP event | 已由 Hub 注入，继续保持 copy-on-write |
| `span_id` | 关联具体 span | 可选，只有存在有效 span 时注入 |
| `agent_id` | 关联 AgentIdentity | secret-free，不包含 provider token |
| `execution_id` | 关联单次 input dispatch | 每次 input 分配一个，贯穿 runtime events |
| `workspace_id` | 关联多租户 workspace | 可为空，平台/cron session 不强制 |
| `worker_type` | 关联 worker adapter | 低基数字段，可用于 metrics label |

## Execution Queue API

第一版是内部接口，不直接公开给用户。

```go
type ExecutionQueue interface {
    Enqueue(ctx context.Context, sessionID string, input RuntimeInput) (ExecutionID, error)
    Cancel(ctx context.Context, executionID ExecutionID, reason string) error
    Status(ctx context.Context, executionID ExecutionID) (ExecutionStatus, error)
}
```

语义：

- 单 session 内保持 FIFO。
- 一个 worker 同时只执行一个 turn。
- retry 和 timeout 记录为 execution metadata。
- queue 不直接解析 provider 私有事件。

## Runtime Context API

RuntimeContext 是 eventstore、turns、worker internal session history 的统一读取接口。

```go
type RuntimeContext interface {
    Load(ctx context.Context, sessionID string, opts ContextLoadOptions) (*ContextSnapshot, error)
    Save(ctx context.Context, sessionID string, update ContextUpdate) error
}
```

第一版来源：

- eventstore raw events。
- turns materialized table。
- worker internal session id。
- workspace agent config overrides。

未来来源：

- project memory。
- skill memory。
- external vector store。
- enterprise knowledge backend。

## HTTP/Admin API 候选

这些端点只在内部 contract 稳定后添加：

```text
GET /api/admin/runtime/agents
GET /api/admin/runtime/agents/{agent_id}
GET /api/admin/runtime/sessions/{session_id}/executions
GET /api/admin/runtime/executions/{execution_id}
GET /api/admin/runtime/context/{session_id}
```

认证：

- `/api/admin/*` 继续使用 cookie session admin。
- `/admin/*` 继续支持 Bearer admin token 和 cookie fallback。

## 兼容性要求

1. 现有 AEP v1 init/input/message/done/error/state 不改名、不删除。
2. 现有 `worker_type` 配置继续有效。
3. AgentSpec 可由旧配置自动生成。
4. 新字段进入 store 时必须允许空值或默认值。
5. 新 runtime event 必须有解析测试和 clone/deep copy 测试。

## 验收测试

每个相关 PR 至少覆盖：

- AgentSpec normalization table tests。
- Session identity persistence and ownership tests。
- AEP encode/decode/clone tests。
- Gateway init/resume compatibility tests。
- Worker start/input behavior regression tests。
- Observability noop mode tests。
