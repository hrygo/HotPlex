# AgentSpec 运行时模型 — First-Cut 设计

> **状态**: 设计（待评审）· **日期**: 2026-07-21 · **跟踪**: issue #847 · **里程碑**: v2 Wave 1 / Milestone A（依赖链链首）
> **基线**: v1.37.2 · **类型**: first-cut 设计（不含实现；遵守 Design-No-Implement）
> **关联规划**: `docs/v2/IMPLEMENTATION-ROADMAP.md`（#847 ROI 9/10 第一刀）、`docs/v2/IMPLEMENTATION-STATUS-AND-PLAN.md`

---

## 1. 目标与定位

把分散在 **config / WS init metadata / workspace 覆盖 / bot-platform 配置** 中的 agent 运行时选项，收敛为一个**只读、归一化、可审计**的 `AgentSpec` 视图与 resolver。

- **是**：现有 config/session/worker 之上的归一化视图层（normalized view + resolver）。
- **不是**：新的 agent runtime 层、agent registry/marketplace、独立策略服务。
- **战略位置**：v2 依赖链的根（#848 identity / #849 events / #851 queue / #852 context / #866 snapshot 都消费 AgentSpec 字段）。先把契约定稳，避免下游各自发明字段。

**第一刀边界（硬约束）**：
1. 只读归一化器 + resolver，**不新增持久化字段**（持久化是 #866）。
2. **不改 Worker 接口**、**不改 session key 派生**。
3. 输出继续映射到现有 `worker.SessionStartParams` / `worker.SessionInfo`。
4. 旧配置（仅设 `worker_type`）必须仍能启动 session。
5. 未知 worker type 仍在边界被拒绝。

---

## 2. 当前基线（碎片现状，已取证）

### 2.1 映射目标
- `worker.SessionStartParams`（`internal/worker/worker.go:95`）：ID/UserID/BotID/BotName/WorkerType/AllowedTools/WorkDir/Platform/PlatformKey/WorkspaceID/InjectExclude。
- `worker.SessionInfo`（`internal/worker/worker.go:312`）：富字段集——AllowedTools/DisallowedTools/AllowedModels/PermissionMode/SkipPermissions/Sandbox/ACPCommand/MaxTurns/MaxBudgetUSD/AllowedDirs/SystemPrompt/MCPConfig/ConfigEnv/ConfigBlocklist 等。

### 2.2 归一化来源（当前分散）
| 维度 | 当前来源（碎片） |
|------|------------------|
| worker_type | 5 级 fallback：per-bot → platform(YAML) → platform(env) → messaging 共享默认 → 编译默认 `claude_code`（`config_defaults.go:propagatePlatform`） |
| permission | `claude_code.permission_mode` / `codex_cli.sandbox`+`approval_mode` / `acp.auto_approve` / workspace 覆盖 / `worker.default_permission_mode`（4 层 tier：read-only\|workspace\|auto-edit\|bypass，默认 auto-edit） |
| tools | allowed/disallowed（config + init metadata） |
| sandbox | `codex_cli.sandbox`、per-bot 覆盖 |
| budget/turns | `MaxBudgetUSD` / `MaxTurns`（worker 级 + session 级） |
| models | `AllowedModels`（注：webchat WS 路径当前未设置，已知 gap，见 §7 风险） |

### 2.3 当前构造点（resolver 将统一的入口）
- **WS init**：`internal/gateway/conn.go`（4 处：~629/670/705/764）
- **REST create-session**：`internal/gateway/api.go:352`
- 其他（first-cut 可选纳入）：`internal/messaging/bridge.go:105`、`internal/cron/executor.go:71`、`internal/admin/sessions.go:44`

### 2.4 workspace 权限覆盖
`Bridge.resolveWorkspacePermissionMode`（`internal/gateway/bridge_worker.go:526`）+ `defaultPermissionMode atomic.Value`（r3 #804）。first-cut 中，**调用方先解析 workspace 覆盖，再把结果作为输入传给 resolver**（保持 resolver 纯净、可表驱动测试，不依赖 store）。

---

## 3. 设计

### 3.1 包放置（解决 config↔worker import cycle）
- 已知约束：`internal/config` **不能** import `internal/worker`（memory：config 复用 `worker.ValidatePermissionMode` 会成环，被迫内联校验）。
- **方案**：新增独立包 `internal/agentspec`，import `internal/config` + `internal/worker`（+ 只读 workspace 输入类型）。gateway/messaging/cron/admin import `internal/agentspec`。
  - 不成环条件：`config`/`worker`/`session` 均不反向 import `agentspec`。
- resolver 设计为**纯函数式**（输入快照 → 输出 AgentSpec），不持有 store、不读全局，便于表驱动测试与 WS/REST 等价性验证。

### 3.2 数据结构（first-cut）
```go
package agentspec

// AgentSpec 是 agent 运行时的归一化、只读、secret-free 视图。
type AgentSpec struct {
    Worker   WorkerSpec   // provider/worker 选择与命令
    Policy   PolicySpec   // 权限/工具边界
    Sandbox  SandboxSpec  // 文件系统/网络隔离
    Budget   BudgetSpec   // 资源上限
    Identity IdentityRefs // 身份引用（仅 ID，非敏感；identity 值对象是 #848）
}

type WorkerSpec struct {
    Type        string   // claude_code | opencode_server | codex_cli | acp（归一化后）
    Command     string   // 解析后的启动命令（含 per-bot 覆盖）
    Model       string   // 允许/默认模型（codex model 等）
    AllowedModels []string
}

type PolicySpec struct {
    PermissionMode  string   // 归一化 4 层 tier：read-only|workspace|auto-edit|bypass
    SkipPermissions bool
    AllowedTools    []string // nil = 不限制
    DisallowedTools []string
}

type SandboxSpec struct {
    Mode        string   // read-only|workspace-write|danger-full-access（codex 语义为基准）
    AllowedDirs []string
}

type BudgetSpec struct {
    MaxTurns     int
    MaxBudgetUSD float64
}

type IdentityRefs struct {
    UserID      string
    WorkspaceID string
    BotName     string
    Platform    string
}
```

> **secret-free 不变量**：AgentSpec 任何字段不得包含 API key / env 值 / 凭证。`ConfigEnv`/凭证类字段**不进** AgentSpec（留在 SessionInfo 构造阶段）。AgentSpec 可安全 log/audit（验收标准之一）。

### 3.3 归一化输入与 Resolver
```go
// Input 是归一化所需的全部上游事实（调用方负责采集，含已解析的 workspace 覆盖）。
type Input struct {
    Cfg            *config.Config    // 配置快照
    InitMeta       InitMetadata      // WS init / REST create 携带的元数据
    WorkspacePerm  string            // 调用方已解析的 workspace permission 覆盖（""=无覆盖）
    BotName        string
    Platform       string
    PlatformKey    map[string]string
}

type Resolver struct{}
func (Resolver) Resolve(in Input) (AgentSpec, error)  // 未知 worker type → error（边界拒绝）
```

**归一化优先级（每个字段）**：`init metadata 显式值 > per-bot 覆盖 > workspace 覆盖（仅 permission）> platform 配置 > messaging 共享默认 > config 默认`。worker_type 复用现有 5 级 fallback（不重写，调用 `config` 既有解析）。

### 3.4 映射到现有结构（不改 Worker 接口）
```go
// MapToStartParams / MapToSessionInfo 把 AgentSpec 投射回现有结构。
func MapToStartParams(spec AgentSpec, base worker.SessionStartParams) worker.SessionStartParams
func MapToSessionInfo(spec AgentSpec, base worker.SessionInfo) worker.SessionInfo
```
- **PermissionMode tier → worker 专属语义**（复用现有 4 层统一映射，memory 43596/43710）：
  - claude_code：`PermissionMode` tier 直传
  - codex_cli：tier → `Sandbox` + `ApprovalMode` 组合
  - acp：tier → `auto_approve` 语义
  - opencode_server：tier → OCS 对应权限参数
- 映射是**幂等覆盖**：`base` 提供调用方已填字段，AgentSpec 仅覆盖其归一化拥有的字段，保留 SessionInfo 中非 AgentSpec 字段（MCPConfig/SystemPrompt/ConfigEnv 等仍由原路径填）。

### 3.5 集成（first-cut 仅两处，保证 WS≡REST）
- `internal/gateway/conn.go`（WS init）与 `internal/gateway/api.go:352`（REST create-session）改为：采集 `Input` → `Resolve` → `MapToStartParams/MapToSessionInfo` → 现有 `StartSession`。
- **等价性不变量**：相同输入下 WS init 与 REST create 产出**等价 AgentSpec**（验收标准）。
- messaging/cron/admin 路径 first-cut **不改**（保留现状），后续按需纳入（避免一次性大改）。

---

## 4. Worker 类型归一化矩阵（first-cut 表驱动覆盖）

| worker_type | permission 来源 | sandbox 语义 | model | 备注 |
|-------------|-----------------|--------------|-------|------|
| claude_code | `claude_code.permission_mode` / workspace | —（用 PermissionMode tier） | AllowedModels | 默认 bypass（无覆盖时） |
| codex_cli | `codex_cli.sandbox`+`approval_mode` | `Sandbox` 字段直用 | `codex_cli.model` | tier→sandbox+approval 映射 |
| opencode_server | OCS 权限参数 | tier 映射 | AllowedModels | 单例进程 |
| acp | `acp.auto_approve` / `acp.command` 覆盖 | tier→auto_approve | — | ACPCommand per-session 覆盖 |

每行至少一个表驱动用例：仅设 worker_type 的旧配置 → 正确默认；显式覆盖 → 覆盖生效；未知 type → error。

---

## 5. 验收标准（对齐 #847）

- [ ] 表驱动测试覆盖 4 个 worker type 的归一化与映射。
- [ ] 仅设 `worker_type` 的旧配置仍能启动 session（向后兼容）。
- [ ] 未知 worker type 在 `Resolve` 边界被拒绝（error）。
- [ ] AgentSpec 可 log/audit 且**不含 secret**（断言无 key/env 值字段）。
- [ ] WS init 与 REST create-session 对等价输入产出等价 AgentSpec（等价性测试）。
- [ ] `docs/reference/configuration.md` 与 `docs/v2/API-DESIGN.md` 更新。
- [ ] `make check` 与 `make docs-build` 通过。

## 6. Non-goals（first-cut）
- 不新增持久化字段/DB 列（#866）。
- 不改 Worker 接口、session key 派生、AEP wire format。
- 不引入 agent registry/marketplace、独立策略服务、策略语言。
- 不改 messaging/cron/admin 构造路径（后续增量）。
- 不实现 #848 identity 值对象（AgentSpec 仅持 IdentityRefs ID 引用）。

## 7. 风险与待决

| 风险/待决 | 说明 | 处理 |
|-----------|------|------|
| config↔worker import cycle | resolver 需同时读 config、写 worker 结构 | 独立 `internal/agentspec` 包；保持 config/worker/session 不反向依赖 |
| AllowedModels webchat gap | 已知 webchat WS 路径未设 AllowedModels（memory 41279） | AgentSpec 归一化时统一注入，顺手修复并在测试中断言 |
| 映射幂等性 | MapTo* 不能清掉 SessionInfo 非 AgentSpec 字段 | "仅覆盖拥有字段"语义 + 单测覆盖 MCPConfig/SystemPrompt 保留 |
| permission tier 映射分歧 | 4 worker 的 tier→原生语义已有实现，勿重写 | 复用现有统一映射函数，AgentSpec 只产 tier，映射委托现有逻辑 |
| WS≡REST 等价性 | 两路径输入采集可能漂移 | 共用同一 `buildInput()` 辅助 + 等价性测试 |
| workspace 覆盖输入耦合 | resolver 不应依赖 store | workspace permission 由调用方解析后作为 Input 字段传入 |

## 8. 测试计划
- `internal/agentspec/resolver_test.go`：表驱动，4 worker × {默认/显式覆盖/未知 type}；secret-free 断言；WS≡REST 等价性。
- `internal/agentspec/mapper_test.go`：MapTo* 幂等覆盖 + 非 AgentSpec 字段保留。
- 回归：现有 gateway/conn/api/session 测试不破（`make check`）。

## 9. 后续衔接
- #848 在 AgentSpec.IdentityRefs 之上定义 `AgentIdentity` 值对象并落 `context_json`。
- #849/#850 从 AgentSpec 派生 runtime metadata keys（agent_id/worker_type/workspace_id）。
- #866 持久化 `EffectiveAgentSpecSnapshot`（secret-free 序列化）。
