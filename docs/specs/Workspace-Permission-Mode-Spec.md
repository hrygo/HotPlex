# Workspace 权限模式规范

**状态**: Draft · **日期**: 2026-06-25 · **关联 Issue**: [#789](https://github.com/hrygo/hotplex/issues/789) · **版本目标**: v1.31.0

---

## 1. 背景与动机

当前 HotPlex 以任意模式启动 Worker 时，四种 Worker 适配器全部默认使用最高权限（bypass）模式：

| Worker | 当前默认行为 | 代码位置 |
|--------|------------|---------|
| Claude Code | `--dangerously-skip-permissions`（全跳过） | `internal/worker/claudecode/worker.go:309-315` |
| Codex CLI | YOLO：`sandbox=danger-full-access` + `approvalPolicy=never` | `internal/worker/codexcli/worker.go:701-711`, `Config` 35-57 |
| OpenCode Server | `set_permission_mode("bypassPermissions")` | `internal/worker/opencodeserver/worker.go:580-597` |
| ACP | `autoApprove=true`（全局默认） | `internal/worker/acp/worker.go:48-63` |

四种 Worker 的权限语义**完全不同构**：CC 是单维度 4 档 mode，Codex 是 `sandbox × approvalPolicy` 双维度 9 组合，OCS 是 CC 兼容的 mode 字符串，ACP 是 `autoApprove` 布尔。

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
| `auto-edit` | `--permission-mode acceptEdits` | `workspace-write` × `never` | `acceptEdits` | `true` |
| `bypass` | `--dangerously-skip-permissions` | `danger-full-access` × `never` | `bypassPermissions` | `true` |

### 3.2 退化说明

统一抽象下，部分 Worker 在相邻档位上无法区分，退化为同档：

| Worker | 退化档位 | 原因 |
|--------|---------|------|
| Claude Code | `workspace` ≡ `auto-edit`（均映射 `acceptEdits`） | CC 无"自动编辑但危险操作仍确认"的独立档；`acceptEdits` 已是最高非 bypass 档，其本身即"自动接受编辑、危险操作仍确认" |
| OpenCode Server | `workspace` ≡ `auto-edit`（均映射 `acceptEdits`） | OCS 采用 CC 兼容 mode 命名，同上原因 |
| ACP | `read-only` ≡ `workspace`（均 `autoApprove=false`），`auto-edit` ≡ `bypass`（均 `true`） | ACP 仅布尔权限粒度，无中间询问档 |

admin UI 需展示当前 workspace 的 `worker_preference` 下各档实际效果，避免 admin 对退化档位产生误判。

## 4. 数据模型

### 4.1 Workspace 表

新增列（`internal/session/sql/migrations/022_workspace_permission_mode.sql`）：

```sql
ALTER TABLE workspaces ADD COLUMN permission_mode TEXT NOT NULL DEFAULT 'bypass';
```

PostgreSQL 对应迁移：`internal/session/sql/migrations-postgres/022_workspace_permission_mode.pg.sql`（语法相同）。

存量行自动获得 `'bypass'`，行为不变。

### 4.2 Workspace 结构体

`internal/session/multitenancy_store.go:16` 的 `Workspace` 新增字段：

```go
PermissionMode string `json:"permission_mode"` // 统一档位；"" = 用全局默认
```

store 层 CreateWorkspace / UpdateWorkspace / ListWorkspace / GetWorkspace 的 SQL 列表与 scan 均需带上 `permission_mode`。

### 4.3 SessionInfo 字段语义重定义

`internal/worker/worker.go:269` 的 `PermissionMode` 注释更新：从 `"default"/"plan"/"auto-accept"`（CC 私有语义）改为统一档位 `read-only/workspace/auto-edit/bypass`。

`SkipPermissions`（`worker.go:271`）保留为 `bypass` 档的内部等价，向后兼容现有调用方；新链路统一使用 `PermissionMode`。

## 5. 配置链路与优先级

`config.yaml` 的 `worker` 段（`configs/config.yaml:176`）新增全局默认：

```yaml
worker:
  default_permission_mode: bypass  # opt-in 收紧；未配置则 bypass
```

优先级（单一链路）：

```
workspace.permission_mode（admin 显式设置）
        ↓ 为空（""）则
config.worker.default_permission_mode（全局默认，缺省 bypass）
        ↓
bridge.buildWorkerInfo 注入 SessionInfo.PermissionMode
        ↓
各 Worker 启动时内部映射到原生参数
```

无需 bot 级覆盖层。

## 6. 实现细节

### 6.1 注入点（bridge）

`internal/gateway/bridge.go:885 buildWorkerInfo` 新增权限模式注入。注入路径：

1. 从 `si`（`session.SessionInfo`）获取 `workspace_id`（实施时确认该字段是否已在 session 元数据中，若无需补齐 session → workspace 的关联读取）
2. bridge 通过现有 `GetWorkspaceByID`（`bridge.go:143`）读取 workspace 的 `permission_mode`
3. 空值（`""`）回退到 `config.worker.default_permission_mode`
4. 写入 `SessionInfo.PermissionMode`

不引入 `platformKey` 中转——权限模式是 workspace 级策略（非 per-bot 通道变量），直接在 `buildWorkerInfo` 读取最清晰。cron / 无 workspace session 因 `workspace_id` 为空，跳过读取直接落全局默认。

### 6.2 Claude Code（`claudecode/worker.go:309-315`）

将当前 `SkipPermissions / PermissionMode / else` 三分支重构为统一档位映射：

```go
mode := session.PermissionMode
if session.SkipPermissions { mode = worker.PermBypass } // 向后兼容
switch mode {
case worker.PermReadOnly:
    args = append(args, "--permission-mode", "plan")
case worker.PermWorkspace, worker.PermAutoEdit:
    args = append(args, "--permission-mode", "acceptEdits")
case worker.PermBypass:
    args = append(args, "--dangerously-skip-permissions")
default: // 空值，保留现有 bypass 行为
    args = append(args, "--dangerously-skip-permissions")
}
```

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

- 控件默认选中 `bypass`（新建 workspace 与全局默认一致）
- tooltip 文案随当前 workspace 的 `worker_preference` 动态展示该档在该 Worker 下的实际效果（含退化提示，如"此 Worker 下『工作区确认』与『自动编辑』效果相同"）
- 保存走现有 workspace update API，后端校验 `permission_mode ∈ {read-only, workspace, auto-edit, bypass, ""}`

涉及文件：
- 后端 handler：`internal/gateway/workspace_handlers.go`（校验 + 透传）
- 前端：workspace 管理 UI 组件（webchat admin 界面）

## 8. 向后兼容与迁移

| 维度 | 策略 |
|------|------|
| DB 迁移 | `permission_mode` 列 `DEFAULT 'bypass'`，存量 workspace 升级后 = bypass = 不变 |
| 全局配置 | `config.worker.default_permission_mode` 缺省 bypass |
| SessionInfo | `PermissionMode=""` 时各 Worker 走现有默认分支（= bypass） |
| `SkipPermissions` | 保留，作 `bypass` 别名，现有调用无需改动 |
| cron / 无 workspace session | 不读 workspace，走全局默认（bypass），行为不变 |

零破坏性：未显式收紧的 workspace 升级后权限模式与现状完全一致。

## 9. 测试策略

| 测试 | 范围 |
|------|------|
| 各 Worker 映射表测试 | 4 档 × 预期原生参数（CC args、Codex params map、OCS/ACP 调用参数），含退化档断言 |
| bridge 注入测试 | workspace 设档 → `SessionInfo.PermissionMode` 正确；空值 → 全局默认；无 workspace → 全局默认 |
| store CRUD 测试 | CreateWorkspace 带权限、UpdateWorkspace 改权限、List/Get 回显 |
| migration 测试 | 存量 workspace 升级后 `permission_mode='bypass'`；SQLite + PG 双驱动 |
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

- OCS 服务端实际支持的 mode 名集合（设计假设 `plan`/`acceptEdits`/`bypassPermissions`，需对照 opencode 版本校准）
- admin UI 中"无 worker_preference 的 workspace"如何展示 tooltip（回退到默认 worker_type 的映射）
