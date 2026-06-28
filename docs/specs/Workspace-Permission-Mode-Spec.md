# Workspace 权限模式规范

**状态**: Draft · **日期**: 2026-06-26（修订：CC `auto-edit` 档改用原生 `auto` mode，消除 §3.2 退化；2026-06-28 修订：empty→worker-default 语义对齐 r2 实现，消除"注入全局 bypass"误导） · **关联 Issue**: [#789](https://github.com/hrygo/hotplex/issues/789) · **版本目标**: v1.31.0

---

## 1. 背景与动机

当前 HotPlex 以任意模式启动 Worker 时，四种 Worker 适配器全部默认使用最高权限（bypass）模式：

| Worker | 当前默认行为 | 代码位置 |
|--------|------------|---------|
| Claude Code | `--dangerously-skip-permissions`（全跳过） | `internal/worker/claudecode/worker.go:309-315` |
| Codex CLI | YOLO：`sandbox=danger-full-access` + `approvalPolicy=never` | `internal/worker/codexcli/worker.go:701-711`, `Config` 35-57 |
| OpenCode Server | `set_permission_mode("bypassPermissions")` | `internal/worker/opencodeserver/worker.go:580-597` |
| ACP | `autoApprove=true`（全局默认） | `internal/worker/acp/worker.go:48-63` |

四种 Worker 的权限语义**完全不同构**：CC 是单维度 mode（本项目映射 `plan`/`acceptEdits`/`auto`/`bypass` 四档，原生共 6 档），Codex 是 `sandbox × approvalPolicy` 双维度 9 组合，OCS 是 CC 兼容的 mode 字符串，ACP 是 `autoApprove` 布尔。

`SessionInfo` 已定义 `PermissionMode` 与 `SkipPermissions` 字段（`internal/worker/worker.go:267-271`），但**全代码库无任何赋值点**（除测试），因此全部走各 Worker 的默认 bypass 分支。

**目标**：让 admin 能为每个 workspace 指定权限模式，该设置通过统一抽象映射到四种 Worker 的原生权限参数，收紧 Worker 的默认 blast radius。

## 2. 设计目标与非目标

### 目标
- admin 可在 workspace 管理页为每个 workspace 设置权限模式
- 同一设置兼容全部四种 Worker（CC / Codex CLI / OpenCode Server / ACP）
- 向后兼容：存量部署升级后行为不变（默认仍 bypass）
- 权限模式在 session 启动时锁定，workspace 后续修改仅对新 session 生效

### 非目标
- 不支持运行时实时切换活跃 session 的权限模式（启动锁定语义）
- 不引入 bot 级权限覆盖（workspace 已是租户边界，策略在 workspace 层统一）
- 不暴露各 Worker 原生权限参数的细粒度组合（统一抽象优先，原生细节由网关映射）
- 不改变 cron / 无 workspace 临时 session 的权限行为（继续走全局默认）

## 3. 统一权限档位

引入 4 档统一权限意图，由网关在 Worker 启动时映射到各 Worker 原生参数：

| 档位 | 语义 | 典型场景 |
|------|------|---------|
| `read-only` | 只读分析，不执行写操作 | 代码审查、只读问答 |
| `workspace` | 工作区读写，危险操作（shell/网络）需确认 | 日常开发 |
| `auto-edit` | 自动接受编辑，危险操作仍确认 | 高频编辑任务 |
| `bypass` | 全自动最高权限（当前默认） | 受信自动化、CI |

新增常量（`internal/worker/worker.go`）：

```go
const (
    PermReadOnly  = "read-only"
    PermWorkspace = "workspace"
    PermAutoEdit  = "auto-edit"
    PermBypass    = "bypass"
)
```

### 3.1 各 Worker 映射表

| 统一档位 | Claude Code | Codex CLI（sandbox × approvalPolicy） | OpenCode Server | ACP（autoApprove） |
|---------|-------------|---------------------------------------|-----------------|-------------------|
| `read-only` | `--permission-mode plan` | `read-only` × `untrusted` | `plan` | `false` |
| `workspace` | `--permission-mode acceptEdits` | `workspace-write` × `on-request` | `acceptEdits` | `false` |
| `auto-edit` | `--permission-mode auto` | `workspace-write` × `never` | `acceptEdits` | `true` |
| `bypass` | `--dangerously-skip-permissions` | `danger-full-access` × `never` | `bypassPermissions` | `true` |

### 3.2 退化说明

统一抽象下，部分 Worker 在相邻档位上无法区分，退化为同档：

| Worker | 退化档位 | 原因 |
|--------|---------|------|
| OpenCode Server | `workspace` ≡ `auto-edit`（均映射 `acceptEdits`） | OCS 服务端暂未暴露与 CC `auto` 等价的 mode（待 §11 校准），以 `acceptEdits` 承载两档 |
| ACP | `read-only` ≡ `workspace`（均 `autoApprove=false`），`auto-edit` ≡ `bypass`（均 `true`） | ACP 仅布尔权限粒度，无中间询问档 |

> **Claude Code 不再退化**：`workspace` → `acceptEdits`（自动接受编辑、危险操作仍确认），`auto-edit` → `auto`（全自动 + 后台安全分类器审查危险操作）。两档精确对齐 CC 原生 `acceptEdits`/`auto` 语义（详见 §6.2），admin 可在 CC workspace 下区分"编辑免确认"与"全自动"两种工作方式。

admin UI 需展示当前 workspace 的 `worker_preference` 下各档实际效果，避免 admin 对退化档位产生误判。

## 4. 数据模型

### 4.1 Workspace 表

新增列（`internal/session/sql/migrations/022_workspace_permission_mode.sql`）：

```sql
ALTER TABLE workspaces ADD COLUMN permission_mode TEXT;
```

采用 **nullable 列**（不加 `NOT NULL DEFAULT`），规避 SQLite（< 3.35 无法 DROP COLUMN）与 PostgreSQL 在 `ADD COLUMN ... DEFAULT` 行为上的差异。读取时 NULL 直接扫描为 `""`（store 层不调用 `NormalizePermissionMode`；空串语义见 §5）。

PostgreSQL 对应迁移：`internal/session/sql/migrations-postgres/022_workspace_permission_mode.pg.sql`（Up 语法相同；Down 为 `DROP COLUMN permission_mode`）。

存量行 `permission_mode` 为 `NULL`，读取归一化为 `""`（= "worker default"，见 §5），行为与升级前一致（各 Worker 走自身默认，CC/OCS→bypass）。

### 4.2 Workspace 结构体

`internal/session/multitenancy_store.go:16` 的 `Workspace` 新增字段：

```go
PermissionMode string `json:"permission_mode"` // 统一档位；"" = "worker default"（见 §5）
```

store 层 CreateWorkspace / UpdateWorkspace / ListWorkspace / GetWorkspace 的 SQL 列表与 scan 均需带上 `permission_mode`。

### 4.3 SessionInfo 字段语义重定义

`internal/worker/worker.go:269` 的 `PermissionMode` 注释更新：从 `"default"/"plan"/"auto-accept"`（CC 私有语义）改为统一档位 `read-only/workspace/auto-edit/bypass`。

`SkipPermissions`（`worker.go:271`）保留为 `bypass` 档的内部等价，向后兼容现有调用方；新链路统一使用 `PermissionMode`。

## 5. 配置链路与优先级

> **核心语义（r2 修订）**：权限模式是**显式收紧**语义——仅当 admin 为某个 workspace **显式**设置 `permission_mode` 时，bridge 才注入该档；否则注入空串 `""`，让每个 Worker 应用自身默认/操作员配置（CC/OCS→bypass；Codex→`cfg.Sandbox`/`cfg.ApprovalMode`；ACP→`cfg.AutoApprove`）。**全局默认不注入**——若注入全局 bypass，会静默覆盖操作员为受限 Codex/ACP 配的沙箱策略（见 §8 安全说明）。

`config.yaml` 的 `worker` 段保留 `default_permission_mode`（`configs/config.yaml`）：

```yaml
worker:
  default_permission_mode: bypass  # 当前为 no-op：已接受 + 热重载，但 bridge 不注入（见下）
```

> `config.worker.default_permission_mode` 当前是 **no-op**（#789 r2 P2）：配置被接受、可热重载，但 `resolveWorkspacePermissionMode` 不消费它——保留以备将来按 worker-type 细化（如"CC 默认 bypass、Codex 默认 workspace"）。

优先级链路（r2 实现）：

```
workspace.permission_mode（admin 显式设置）
        ↓
bridge.resolveWorkspacePermissionMode：
  · workspace 有显式覆盖 → 注入该档
  · 无覆盖 / fetch 失败 → 注入 ""（降级，不注入 bypass）
        ↓
各 Worker 启动时：
  · PermissionMode 非空 → 按档位映射原生参数
  · PermissionMode == "" → 用自身默认/操作员配置（CC/OCS→bypass；Codex→cfg；ACP→cfg）
```

无需 bot 级覆盖层。

## 6. 实现细节

### 6.1 注入点（bridge）

`internal/gateway/bridge.go:885 buildWorkerInfo` 新增权限模式注入（实际由 `resolveWorkspacePermissionMode` 在 `bridge_worker.go` 实现，仿 `resolveWorkspaceOverrides` 的 fetch→降级模板）。注入路径：

1. 从 `si`（`session.SessionInfo`）获取 `workspace_id`
2. bridge 通过现有 `GetWorkspaceByID` 读取 workspace 的 `permission_mode`
3. **显式覆盖**：`ws.PermissionMode != ""` → 注入该档；**否则注入 `""`**（r2 P2：不注入全局默认，避免覆盖受限 worker 配置）
4. fetch 失败 → `warnOverrideDegrade` 降级，注入 `""`（不注入 bypass）

不引入 `platformKey` 中转——权限模式是 workspace 级策略（非 per-bot 通道变量），直接在 `buildWorkerInfo` 读取最清晰。cron / 无 workspace session 因 `workspace_id` 为空，注入 `""`，各 Worker 走自身默认。

### 6.2 Claude Code（`claudecode/worker.go:309-315`）

将当前 `SkipPermissions / PermissionMode / else` 三分支重构为统一档位映射：

```go
mode := session.PermissionMode
if session.SkipPermissions { mode = worker.PermBypass } // 向后兼容
switch mode {
case worker.PermReadOnly:
    args = append(args, "--permission-mode", "plan")
case worker.PermWorkspace:
    args = append(args, "--permission-mode", "acceptEdits")
case worker.PermAutoEdit:
    args = append(args, "--permission-mode", "auto")
case worker.PermBypass:
    args = append(args, "--dangerously-skip-permissions")
default: // 空值，保留现有 bypass 行为
    args = append(args, "--dangerously-skip-permissions")
}
```

> **前提（CC `auto` mode）**：`auto` 由较新版本 claude CLI 提供，语义为"全自动执行 + 后台安全分类器审查危险操作"——精确承载统一 `auto-edit` 档（区别于 `workspace` 档的 `acceptEdits`：后者仅自动接受文件编辑/常规 fs 命令，shell·网络等危险操作仍确认）。**决策：强制升级**——生产环境 claude CLI 必须支持 `--permission-mode auto`；旧版 CLI 下 CC worker 启动即失败报错（明确提示升级 CLI），**不静默降级**到 `acceptEdits`。doctor/onboard 应检测 CLI 版本，版本下限与探测落点见 §11。

### 6.3 Codex CLI（`codexcli/worker.go:701 buildThreadStartParams`）— 当前 gap

Codex 当前**从不读 `PermissionMode`**，仅读 `SkipPermissions`。新增 `permissionModeFromSession` 映射，非空时覆盖 cfg 默认 YOLO：

```go
// permissionModeFromSession 将统一档位映射到 Codex sandbox × approvalPolicy。
// mode 为空时返回零值 ok=false，调用方保留 cfg 默认（YOLO）。
func permissionModeFromSession(mode string, skip bool) (sandbox, approval string, ok bool) {
    if skip { mode = worker.PermBypass }
    switch mode {
    case worker.PermReadOnly:  return "read-only", "untrusted", true
    case worker.PermWorkspace: return "workspace-write", "on-request", true
    case worker.PermAutoEdit:  return "workspace-write", "never", true
    case worker.PermBypass:    return "danger-full-access", "never", true
    }
    return "", "", false
}
```

在 `buildThreadStartParams` 中：映射 ok 时覆盖 `sandbox` 与 `approvalPolicy`；否则保留 cfg 默认。

### 6.4 OpenCode Server（`opencodeserver/worker.go:580-597`）

当前直传 `session.PermissionMode` 作为 OCS mode。需新增统一档位 → OCS mode 名映射（OCS 采用 CC 兼容 mode：`plan` / `acceptEdits` / `bypassPermissions`，见 `commands.go:205-221`）：

```go
mode := mapToOCSMode(session.PermissionMode) // "" → "bypassPermissions"（保留默认）
```

映射表：`read-only→plan`，`workspace/auto-edit→acceptEdits`，`bypass→bypassPermissions`，空值 `bypassPermissions`。

### 6.5 ACP（`acp/worker.go:48-63`）— 当前 gap

ACP 启动时**不读 SessionInfo 任何权限字段**，仅用全局 `autoApproveDefault`。改造 Start（约 `worker.go:63`）：

```go
w.autoApprove.Store(autoApproveDefault.Load()) // 既有：全局默认
// 新增：session 显式档位覆盖
switch session.PermissionMode {
case worker.PermReadOnly, worker.PermWorkspace:
    w.autoApprove.Store(false)
case worker.PermAutoEdit, worker.PermBypass:
    w.autoApprove.Store(true)
}
```

运行时 `set_permission_mode` 控制命令（`worker.go:824-832`）保持不变，仍可由用户在会话中调整。

## 7. admin UI

workspace 管理「创建 / 编辑」表单新增**权限模式**分段单选控件，4 档可选，每档配 tooltip 说明：

- 控件默认选中 `bypass`（新建 workspace 与各 Worker 默认一致：CC/OCS=bypass，Codex/ACP=操作员配置；admin 全局默认当前为 no-op，见 §5/§8）
- tooltip 文案随当前 workspace 的 `worker_preference` 动态展示该档在该 Worker 下的实际效果（含退化提示，如"此 Worker 下『工作区确认』与『自动编辑』效果相同"）
- 保存走现有 workspace update API，后端校验 `permission_mode ∈ {read-only, workspace, auto-edit, bypass, ""}`

涉及文件：
- 后端 handler：`internal/gateway/workspace_handlers.go`（校验 + 透传）
- 前端：workspace 管理 UI 组件（webchat admin 界面）

## 8. 向后兼容与迁移

| 维度 | 策略 |
|------|------|
| DB 迁移 | `permission_mode` 列 nullable（无 `DEFAULT`），存量行 `NULL`，读取归一化为 `""` |
| 全局配置 | `config.worker.default_permission_mode` 已接受 + 热重载，但当前为 **no-op**（bridge 不注入）；缺省 bypass |
| SessionInfo | `PermissionMode=""`（worker default）：CC/OCS 走 bypass；Codex 遵循 `cfg.Sandbox`/`cfg.ApprovalMode`；ACP 遵循 `cfg.AutoApprove` |
| `SkipPermissions` | 保留，作 `bypass` 别名（CC 优先级最高），现有调用无需改动 |
| cron / 无 workspace session | 不读 workspace，注入 `""`，各 Worker 走自身默认/配置，行为不变 |
| CC `auto` mode 依赖 | 仅 `auto-edit` 档要求 claude CLI 较新版本（强制升级，不回退）；默认 `bypass` 及 `read-only`/`workspace` 档不引入新依赖。旧 CLI + `auto-edit` → 启动失败报错，不降级 |

**安全说明（r2 P2 核心动机）**：bridge **不注入全局默认 bypass**。若操作员为受限环境配了 `codex.sandbox=read-only` 或 `acp.auto_approve=false`，无显式覆盖的 workspace 注入 `""` 后，这些 Worker 会遵循操作员配置（收紧）；只有当 admin **显式**为某 workspace 设 `permission_mode` 时才覆盖。这避免了"升级后静默把受限 worker 提权到 bypass"的回归。

零破坏性：未显式收紧的 workspace 升级后，各 Worker 走自身默认（CC/OCS=bypass，Codex/ACP=操作员配置），与升级前一致。

## 9. 测试策略

| 测试 | 范围 |
|------|------|
| 各 Worker 映射表测试 | 4 档 × 预期原生参数（CC args、Codex params map、OCS/ACP 调用参数），含退化档断言 |
| CC `auto-edit` 独立性 | `auto-edit` 档产出的 args 含 `--permission-mode auto`，与 `workspace` 档（`acceptEdits`）严格区分；同时验证 `read-only`→`plan`、`bypass`→`--dangerously-skip-permissions`、空值→`--dangerously-skip-permissions` |
| bridge 注入测试 | workspace 设档 → `SessionInfo.PermissionMode` 正确；空值 → `""`（worker default）；无 workspace → `""` |
| store CRUD 测试 | CreateWorkspace 带权限、UpdateWorkspace 改权限、List/Get 回显 |
| migration 测试 | 存量 workspace 升级后 `permission_mode` 为 NULL（读取为 `""`）；SQLite + PG 双驱动 |
| handler 校验测试 | 非法 `permission_mode` 值被拒（400）；合法 4 档 + 空值通过 |
| 向后兼容测试 | `PermissionMode=""` + `SkipPermissions=false` → 各 Worker 走 bypass 默认分支 |

## 10. 实施清单

**数据层**
- [ ] `internal/session/sql/migrations/022_workspace_permission_mode.sql`
- [ ] `internal/session/sql/migrations-postgres/022_workspace_permission_mode.pg.sql`
- [ ] `internal/session/multitenancy_store.go`：Workspace 结构 + Create/Update/List/Get SQL
- [ ] store 测试更新（PG mock、SQLite）

**Worker 层**
- [ ] `internal/worker/worker.go`：4 档常量 + PermissionMode 注释更新
- [ ] `internal/worker/claudecode/worker.go`：统一档位映射
- [ ] `internal/worker/codexcli/worker.go`：新增 `permissionModeFromSession` + 接入 buildThreadStartParams
- [ ] `internal/worker/opencodeserver/worker.go`：统一档位 → OCS mode 映射
- [ ] `internal/worker/acp/worker.go`：Start 读 PermissionMode 初始化 autoApprove
- [ ] 各 Worker 映射表测试

**Gateway 层**
- [ ] `internal/gateway/bridge.go`：buildWorkerInfo 注入 permission_mode
- [ ] `internal/gateway/workspace_handlers.go`：校验 + 透传

**配置**
- [ ] `internal/config/config_types.go` / `config_defaults.go`：`DefaultPermissionMode`
- [ ] `configs/config.yaml` / `configs/config-dev.yaml`：`worker.default_permission_mode: bypass`

**前端**
- [ ] webchat admin workspace 管理页：权限模式分段控件 + tooltip

## 11. 开放问题（实施阶段确认）

- OCS 服务端实际支持的 mode 名集合（设计假设 `plan`/`acceptEdits`/`bypassPermissions`，需对照 opencode 版本校准；若 OCS 后续支持与 CC `auto` 等价 mode，可消除 §3.2 中 OCS 的 `workspace`/`auto-edit` 退化）
- claude CLI 支持 `--permission-mode auto` 的最低版本待实测确定（**决策：强制升级 CLI，不做静默降级**）。校准后须：① 将版本下限写入 doctor 检查（`internal/cli/checkers`）；② CC worker 启动期版本探测，不达标直接拒绝启动并提示升级；③ 仅影响显式设为 `auto-edit` 档的 CC workspace，默认 `bypass` 不受此依赖约束
- admin UI 中"无 worker_preference 的 workspace"如何展示 tooltip（回退到默认 worker_type 的映射）
