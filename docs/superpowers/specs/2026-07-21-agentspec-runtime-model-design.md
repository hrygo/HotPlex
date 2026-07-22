# AgentSpec 运行时模型 — First-Cut 设计

> **状态**: 设计（已纳入独立审查 F1-F7 修订，待二次评审）· **日期**: 2026-07-21（F1-F7 修订 2026-07-22）· **跟踪**: issue #847 · **里程碑**: v2 Wave 1 / Milestone A（依赖链链首）
> **基线**: v1.37.2 · **类型**: first-cut 设计（不含实现；遵守 Design-No-Implement）
> **关联规划**: `docs/v2/IMPLEMENTATION-ROADMAP.md`（#847 ROI 9/10 第一刀）、`docs/v2/IMPLEMENTATION-STATUS-AND-PLAN.md`
>
> **修订记录（F1-F7）**：独立审查对 5 条 spec 断言做了代码取证，发现 7 项需修订——F1/F2 移除两个"顺手/委托现有逻辑"的隐性行为变更与不成立假设；F3/F4 修正 worker_type 与 WS≡REST 两处过度断言；F5/F6/F7 收紧包放置论证、补字段所有权表、定义 InitMetadata 与灰度回滚。逐项处理见 §10。

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
| models | `AllowedModels`（注：webchat WS 路径当前未设置，为**已存在的碎片**；first-cut **不**顺手注入，见 §6 Non-goals 与 §10 F1） |

### 2.3 当前构造点（resolver 将统一的入口）
- **WS init**：`internal/gateway/conn.go`（4 处：~629/670/705/764）
- **REST create-session**：`internal/gateway/api.go:352`
- 其他（first-cut 可选纳入）：`internal/messaging/bridge.go:105`、`internal/cron/executor.go:71`、`internal/admin/sessions.go:44`

### 2.4 workspace 权限覆盖
`Bridge.resolveWorkspacePermissionMode`（`internal/gateway/bridge_worker.go:526`）+ `defaultPermissionMode atomic.Value`（r3 #804）。first-cut 中，**调用方先解析 workspace 覆盖，再把结果作为输入传给 resolver**（保持 resolver 纯净、可表驱动测试，不依赖 store）。

---

## 3. 设计

### 3.1 包放置（推荐独立包，非强制）
- 取证结论（修订 F5）：当前依赖是**单向**的——`internal/worker` import `internal/config`，`internal/config` **不** import `internal/worker`（memory：config 复用 `worker.ValidatePermissionMode` 会成环，故被迫内联校验）；`internal/gateway` 同时 import config 与 worker。因此 resolver **并非被迫**放新包——理论上也可放在 gateway 内。
- **选择**：仍新增独立包 `internal/agentspec`，import `internal/config` + `internal/worker`（+ 只读 workspace 输入类型）。gateway/messaging/cron/admin import `internal/agentspec`。
  - 理由（偏好，非约束）：① 归一化逻辑与 gateway 的 WS/HTTP 关注点隔离，避免把纯配置归一化耦合进连接生命周期代码；② 纯函数 + 独立包最利于表驱动测试与 WS≡REST 等价性验证；③ 为 #848/#849/#852 等下游消费者提供稳定 import 目标。
  - 不成环条件（硬约束）：`config`/`worker`/`session` 均不反向 import `agentspec`。
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

**InitMetadata 定义（修订 F7）**：`Input.InitMeta` 是 WS init / REST create 请求携带的、由用户/客户端**显式声明**的运行时意图，对应 AEP init 帧与 REST create-session body 中已存在的字段子集（非派生、非默认）：
```go
// InitMetadata 仅承载请求显式携带的覆盖意图；零值/nil 表示"未声明，走 fallback"。
type InitMetadata struct {
    WorkerType      string   // 请求显式指定的 worker type（""=未声明）
    PermissionMode  string   // 请求显式指定的权限 tier（""=未声明）
    AllowedTools    []string // 显式 allowed tools（nil=未声明）
    DisallowedTools []string // 显式 disallowed tools（nil=未声明）
    Model           string   // 显式模型（""=未声明）
    // 仅纳入 first-cut 需归一化的字段；其余 init 字段（如 resume/MCPConfig）不进 InitMetadata
}
```
> 显式 vs 未声明的区分是优先级链的第一环：resolver 只对"已声明"的 InitMetadata 字段应用最高优先级，未声明者 fallthrough 到后续层级。

**归一化优先级（每个字段）**：`init metadata 显式值 > per-bot 覆盖 > workspace 覆盖（仅 permission）> platform 配置 > messaging 共享默认 > config 默认`。

**worker_type 解析（修订 F3）**：取证表明**不存在**单一的 `ResolveWorkerType`/`EffectiveWorkerType` 可复用入口——现有 5 级 fallback 仅由 `config.propagatePlatform`（`config_defaults.go:240`）在配置加载期落地**一级**（platform 级），其余层级散落。因此 first-cut 不假设"调用 config 既有单一解析"。两种落地（二选一，实施前确认）：
- **(a) 推荐**：在 `internal/config` 增一个薄的 `ResolveWorkerType(cfg, botName, platform) string`，把 5 级链（per-bot → platform YAML → platform env → messaging 共享默认 → 编译默认 `claude_code`）收敛为单一入口，agentspec 调用之。属轻量 pre-work，且对其他消费者也有价值。
- **(b)**：agentspec 内部按文档化的 5 级链自行解析（不依赖 config 新入口），但需在 resolver 测试中逐级断言，且与 `propagatePlatform` 行为保持镜像一致的回归测试。
- 无论 (a)/(b)，**行为不得偏离**当前已生效的 5 级语义（向后兼容硬约束，见 §1 约束 4）。

### 3.4 映射到现有结构（不改 Worker 接口）
```go
// MapToStartParams / MapToSessionInfo 把 AgentSpec 投射回现有结构。
func MapToStartParams(spec AgentSpec, base worker.SessionStartParams) worker.SessionStartParams
func MapToSessionInfo(spec AgentSpec, base worker.SessionInfo) worker.SessionInfo
```
- **PermissionMode tier → worker 专属语义（修订 F2）**：取证表明当前**不存在**单一的"tier → worker 启动参数"统一映射函数可供委托。现有 permission 相关代码是**运行时请求处理**，而非启动参数映射：`permissionModeFromCodexEffective`（codexcli:809）、`preparePermissionModeRequest`（claudecode:686）、`handleSetPermissionMode`（acp:958）、`MapPermissionRequest`（acp/mapper:407）、`setPermissionMode`（opencodeserver/commands:249）。各 worker 的 tier→启动参数映射目前**内联在各自 adapter** 里。
  - 因此 first-cut **收窄** AgentSpec 的职责：AgentSpec 只产出**归一化的 permission tier**（`PolicySpec.PermissionMode`），tier→各 worker 原生启动参数的翻译**保留在各 adapter 既有内联路径**，MapTo* 仅把 tier 写入 `SessionInfo.PermissionMode` 等现有字段，**不**在 agentspec 内重写映射。
  - 若后续要消除内联分歧，属独立 pre-work（"抽取共享 tier→native 映射"），**不在 first-cut 范围**；first-cut 的行为必须与现状逐 worker 一致（§4 矩阵逐行断言）。
  - 4 worker 的 tier 语义（仅记录现状，非 agentspec 实现）：claude_code `PermissionMode` 直传；codex_cli tier → `Sandbox`+`ApprovalMode` 组合；acp tier → `auto_approve`；opencode_server tier → OCS 权限参数。
- 映射是**幂等覆盖**：`base` 提供调用方已填字段，AgentSpec 仅覆盖其归一化拥有的字段，保留 SessionInfo 中非 AgentSpec 字段（MCPConfig/SystemPrompt/ConfigEnv 等仍由原路径填）。字段所有权边界见 §3.4.1。

### 3.4.1 字段所有权与优先级（修订 F6）

MapTo* 的"幂等覆盖"必须明确**哪些字段由 AgentSpec 拥有（会覆盖 base）**、**哪些字段属保留路径（base 原样保留）**，否则幂等性无法测试、易误清非归一化字段。

**AgentSpec 拥有字段**（MapTo* 用 AgentSpec 值覆盖 base；AgentSpec 零值语义为"不覆盖/沿用 base"，不写空）：

| SessionInfo / StartParams 字段 | AgentSpec 来源 | 覆盖规则 |
|-------------------------------|----------------|----------|
| `WorkerType` | `Worker.Type` | 始终覆盖 |
| `PermissionMode` | `Policy.PermissionMode` | 非空覆盖 |
| `SkipPermissions` | `Policy.SkipPermissions` | 始终覆盖（bool） |
| `AllowedTools` | `Policy.AllowedTools` | 非 nil 覆盖（nil=不限制，保留 base） |
| `DisallowedTools` | `Policy.DisallowedTools` | 非 nil 覆盖 |
| `AllowedModels` | `Worker.AllowedModels` | **first-cut 不主动注入**（见 F1）；仅当上游已提供时透传 |
| `Sandbox` | `Sandbox.Mode` | 非空覆盖 |
| `AllowedDirs` | `Sandbox.AllowedDirs` | 非 nil 覆盖 |
| `MaxTurns` / `MaxBudgetUSD` | `Budget.*` | 非零覆盖 |

**保留路径字段**（base 原样保留，MapTo* **绝不**触碰，由原构造路径填）：
- resume 族：`ContinueSession` / `ForkSession` / `ResumeSessionAt` / `ResumeSessionID`
- 注入族：`MCPConfig` / `SystemPrompt` / `ConfigEnv` / `ConfigBlocklist` / `InjectExclude`
- 其余：`ACPCommand`（per-session 覆盖，first-cut 保留现状）等

**优先级冲突规则**：同一字段若 `base`（调用方预填，如 resume 请求携带）与 AgentSpec 都给出值——
- AgentSpec 拥有字段：AgentSpec 胜（除非 AgentSpec 该字段为零值/nil，则沿用 base）。
- 保留路径字段：base 恒胜，AgentSpec 不参与。
- resume 语义特殊：resume/fork 字段**只在 base 出现时生效**，AgentSpec 永不覆盖（避免归一化破坏断点续传）。

### 3.5 集成（first-cut 仅两处，保证 WS≡REST）
- `internal/gateway/conn.go`（WS init）与 `internal/gateway/api.go:352`（REST create-session）改为：共用同一 `buildInput()` 采集 `Input` → `Resolve` → `MapToStartParams/MapToSessionInfo` → 现有 `StartSession`。
- **等价性不变量（修订 F4）**：对纯函数 resolver 而言"WS≡REST 等价"若不约束输入采集就是**同义反复**。真正的等价性保证落在 **`buildInput()` 共用**上——两路径必须把**语义相同的请求**映射成**相同的 `Input`**，再由纯函数 `Resolve` 产出相同 AgentSpec。
  - **已存在的分歧（必须正视，不得掩盖）**：取证显示 WS init（`conn.go:629`）会传 `AllowedTools: initData.Config.AllowedTools`，而 REST create（`api.go:352`）**不**传 AllowedTools。这是一个先于本设计的真实行为差异。
  - **first-cut 处置**：`buildInput()` 对两路径采用**同一套字段抽取逻辑**；若某字段（如 AllowedTools）在 REST 请求体中本无对应来源，则 `Input.InitMeta.AllowedTools` 为 nil（未声明），而非凭空注入——**保持现状语义，不借等价性之名引入行为变更**。若决定让 REST 也接受 AllowedTools，那是**独立的行为变更**，需单列并在验收中显式标注，不混入本等价性不变量。
  - 等价性测试因此断言：**给定两个字段语义等价的请求对象，WS 与 REST 各自的 `buildInput()` 产出相等的 `Input`** → `Resolve` 产出相等的 AgentSpec。
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
- [ ] WS init 与 REST create-session **共用 `buildInput()`**；对字段语义等价的请求，两路径产出相等 `Input` 进而相等 AgentSpec（等价性测试，修订 F4）。AllowedTools 等 REST 无来源的字段保持 nil，不得借等价性注入（F1）。
- [ ] `docs/reference/configuration.md` 与 `docs/v2/API-DESIGN.md` 更新。
- [ ] `make check` 与 `make docs-build` 通过。

## 6. Non-goals（first-cut）
- 不新增持久化字段/DB 列（#866）。
- 不改 Worker 接口、session key 派生、AEP wire format。
- 不引入 agent registry/marketplace、独立策略服务、策略语言。
- 不改 messaging/cron/admin 构造路径（后续增量）。
- **不注入/修复 `AllowedModels` webchat gap**（修订 F1）：归一化时**不**主动补 AllowedModels。理由：注入会改变 webchat WS 路径的实际模型可见性，属**行为变更**，违背 first-cut "无行为变更" 硬约束，且可能收窄用户当前可用的模型集导致回归。该 gap 若需修复，单列 issue 独立评估（见 §10 F1）。
- 不在 agentspec 内重写 permission tier→worker 原生参数映射（修订 F2）：仅产 tier，翻译保留在各 adapter 内联路径。
- 不实现 #848 identity 值对象（AgentSpec 仅持 IdentityRefs ID 引用）。

## 7. 风险与待决

| 风险/待决 | 说明 | 处理 |
|-----------|------|------|
| config↔worker import cycle | resolver 需同时读 config、写 worker 结构 | 独立 `internal/agentspec` 包（偏好，非强制，见 §3.1/F5）；保持 config/worker/session 不反向依赖 |
| AllowedModels webchat gap | 已知 webchat WS 路径未设 AllowedModels（memory 41279） | **first-cut 不修复**（修订 F1）：注入即行为变更，可能收窄用户可用模型集；单列 issue 独立评估 |
| 映射幂等性 | MapTo* 不能清掉 SessionInfo 非 AgentSpec 字段 | §3.4.1 字段所有权表（修订 F6）：拥有字段才覆盖、保留路径字段恒不触碰 + 单测覆盖 resume/MCPConfig/SystemPrompt 保留 |
| permission tier 映射分歧 | 4 worker 的 tier→原生语义**无单一共享映射**，现内联在各 adapter（修订 F2） | first-cut 只产 tier、不重写映射，翻译保留在各 adapter 内联路径且须与现状逐 worker 一致；抽取共享映射属独立 pre-work |
| worker_type 无单一解析入口 | 仅 `propagatePlatform` 落一级，无 `ResolveWorkerType`（修订 F3） | pre-work (a) config 增薄 `ResolveWorkerType` 收敛 5 级链，或 (b) agentspec 内镜像解析 + 逐级回归；行为不得偏离现状 |
| WS≡REST 等价性 | 两路径输入采集可能漂移；REST 不传 AllowedTools 是先存分歧 | 共用 `buildInput()`（修订 F4）；REST 无来源字段保持 nil，不借等价性注入行为变更 |
| 无灰度/回滚路径 | 直接替换构造路径风险集中（修订 F7） | 采用 shadow 模式：并行计算 AgentSpec 与旧路径产物对比、记录 diff 观测后再切换；回滚=回退到旧构造路径（§10 F7） |
| workspace 覆盖输入耦合 | resolver 不应依赖 store | workspace permission 由调用方解析后作为 Input 字段传入 |

## 8. 测试计划
- `internal/agentspec/resolver_test.go`：表驱动，4 worker × {默认/显式覆盖/未知 type}；secret-free 断言；WS≡REST 等价性（共用 `buildInput()`，字段语义等价请求 → 相等 `Input` → 相等 AgentSpec，修订 F4）。
- `internal/agentspec/resolver_test.go`（worker_type，修订 F3）：逐级断言 5 级 fallback（per-bot → platform YAML → platform env → messaging 默认 → 编译默认）与现状镜像一致。
- `internal/agentspec/mapper_test.go`：MapTo* 幂等覆盖 + 非 AgentSpec 字段保留（resume 族 / MCPConfig / SystemPrompt / ConfigEnv，依 §3.4.1 所有权表，修订 F6）。
- **Shadow 模式验证（修订 F7）**：集成初期以 shadow 模式并行运行——旧构造路径产出为权威结果，AgentSpec 路径仅旁路计算并 diff 记录（不生效）；diff 归零/可解释后切换为权威路径。回滚预案：保留旧构造路径开关，异常即回退。
- 回归：现有 gateway/conn/api/session 测试不破（`make check`）。

## 9. 后续衔接
- #848 在 AgentSpec.IdentityRefs 之上定义 `AgentIdentity` 值对象并落 `context_json`。
- #849/#850 从 AgentSpec 派生 runtime metadata keys（agent_id/worker_type/workspace_id）。
- #866 持久化 `EffectiveAgentSpecSnapshot`（secret-free 序列化）。

## 10. 独立审查修订记录（F1-F7，2026-07-22）

对初稿 5 条核心断言做代码取证后的 7 项修订。每项含：原断言 → 取证结论 → 处理。

| ID | 级别 | 原断言 | 取证结论（HEAD 代码） | 处理 |
|----|------|--------|------------------------|------|
| **F1** | HIGH | §7 称归一化时"顺手注入 AllowedModels，闭合 webchat gap" | webchat WS 路径确未设 AllowedModels（gap 真实），但**注入即行为变更**——会收窄用户当前可用模型集，违背 first-cut "无行为变更" 硬约束 | 移出 first-cut：§2.2/§6/§7 改为"不修复、单列 issue"；§3.4.1 标注 AllowedModels 不主动注入 |
| **F2** | HIGH | §3.4 称 tier→worker 语义"委托现有统一映射" | **不存在**单一 tier→启动参数映射函数；现有 permission 代码均为**运行时请求处理**（`permissionModeFromCodexEffective` codexcli:809、`preparePermissionModeRequest` claudecode:686、`handleSetPermissionMode` acp:958、`MapPermissionRequest` acp/mapper:407、`setPermissionMode` opencodeserver:249），映射内联在各 adapter | 收窄职责：AgentSpec 只产 tier，翻译保留在各 adapter 内联路径且须与现状逐 worker 一致；抽取共享映射列为独立 pre-work（§3.4/§6/§7） |
| **F3** | MED | §3.3 称"worker_type 复用现有 5 级 fallback，调用 config 既有解析" | **无**单一 `ResolveWorkerType`/`EffectiveWorkerType` 入口；5 级链仅 `propagatePlatform`（config_defaults.go:240）在加载期落**一级** | 列 pre-work：(a) config 增薄 `ResolveWorkerType`（推荐）或 (b) agentspec 内镜像 + 逐级回归；行为不得偏离现状（§3.3/§7/§8） |
| **F4** | MED | §3.5/§5 的"WS≡REST 等价 AgentSpec"作为验收 | 对纯函数 resolver 该断言**同义反复**；且 WS init（conn.go:629）传 AllowedTools、REST create（api.go:352）**不传**——先存分歧 | 验收改为"共用 `buildInput()`，语义等价请求 → 相等 Input"；REST 无来源字段保持 nil，不借等价性注入行为变更（§3.5/§5/§7） |
| **F5** | LOW | §3.1 称"必须新增 agentspec 包解决 import cycle" | 依赖为**单向**（worker→config；config 不 import worker；gateway 两者都 import），新包**非强制** | 改述为"推荐，偏好非约束"，补 3 条理由（隔离/可测/稳定 import 目标）（§3.1/§7） |
| **F6** | LOW | "映射幂等覆盖"语义欠精确 | — | 新增 §3.4.1 字段所有权表：AgentSpec 拥有字段 vs 保留路径字段（resume/MCPConfig/SystemPrompt/ConfigEnv），含优先级冲突规则（resume 恒由 base 生效） |
| **F7** | LOW | `Input.InitMetadata` 未定义；无灰度/回滚 | — | §3.3 定义 `InitMetadata`（显式 vs 未声明区分）；§7/§8 增 shadow 模式并行 diff + 回滚预案 |

**未改动的有效断言**（取证通过，保留）：
- 映射目标 `worker.SessionStartParams`/`SessionInfo` 字段布局（§2.1）与 HEAD 一致。
- 构造点 WS init（conn.go）/ REST create（api.go:352）真实存在。
- workspace 覆盖由调用方解析后作为 Input 传入（§2.4）合理。
- secret-free 不变量、未知 worker type 边界拒绝、向后兼容硬约束（§1）。

**遗留待决（实施前需确认）**：F2 的 pre-work 是否本迭代内做（默认否，first-cut 只产 tier）；F3 选 (a) 还是 (b)（默认 (a)）；F4 中 REST 是否补 AllowedTools（默认否，独立行为变更）。
