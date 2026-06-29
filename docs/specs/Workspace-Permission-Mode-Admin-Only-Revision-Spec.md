# Workspace 权限模式 — Admin-Only 配置 + 默认收紧修订

**状态**: Draft · **日期**: 2026-06-29 · **修订**: r3（针对 [Workspace-Permission-Mode-Spec.md](./Workspace-Permission-Mode-Spec.md) r2）· **关联**: 原 issue [#789](https://github.com/hrygo/hotplex/issues/789) · **版本目标**: v1.31.x

---

## 1. 背景

原 spec（r2）落地了 4 档统一 Permission Mode，但留下两点宽松默认：

1. **默认值偏高**：`config.worker.default_permission_mode` 被设为 `"bypass"` 且刻意做成 **no-op**（spec §5/§8：bridge `resolveWorkspacePermissionMode` 在 workspace 无显式覆盖时注入 `""`，让各 Worker 走自身默认——CC/OCS→bypass）。结果是绝大多数未显式配置的 workspace 实际跑在最高权限。
2. **配置入口过宽**：`workspace_handlers.go` 的 Create/Update 仅做格式校验（`ValidatePermissionMode`），**owner 自己**就能给自己的 workspace 设/改 `permission_mode`，无需 admin。

本次修订（r3）针对这两点收紧：

- **决策 A**：`config.worker.default_permission_mode` 从 no-op 转为**真正被 bridge 消费**，缺省值 `bypass` → `workspace`。
- **决策 B**：`permission_mode` 的设置/修改权限收归 **admin-only**，非 admin 传该字段 → 403（fail-closed）。
- **决策 C**：config 默认仅在 **workspace session** 生效；无 workspace session（cron / Slack / Feishu 平台 channel）保持注入 `""`，保留 r2 §8 对操作员受限 Codex/ACP 配置的保护。

## 2. 安全权衡（ceiling 语义化解 r2 §8 担忧）

r2 §8 不注入全局默认的核心动机：避免覆盖操作员为受限环境配的 `codex.sandbox=read-only` / `acp.auto_approve=false`。r3 把默认值**同时**从 `bypass` 降为 `workspace`，并把注入语义定义为 **ceiling（权限上限）**：注入值只能**收紧**操作员配置，绝不放宽。

**ceiling 落地点在各 Worker 内部**（`session.PermissionMode` 是 gateway 的意图，Worker 把它与操作员配置取**更严**者）：

| Worker | ceiling 实现 | 例：注入 `workspace` + 操作员 `codex.sandbox=read-only` |
|---|---|---|
| CC / OCS | 无独立操作员权限配置，`""`→bypass 即被注入值替换 | `workspace` → acceptEdits（收紧） |
| **Codex** | `buildThreadStartParams` 取 `max(rank(session tier), rank(cfg.Sandbox/ApprovalMode))` | sandbox 保持 `read-only`（操作员更严，**不提权**）；approval 取更严者 |
| ACP | `auto_approve` 二值，`workspace`→false 已是严极 | `false`（收紧），不可能从 false 升 true |

| 场景 | r2 行为（无覆盖） | r3 行为（无覆盖，ceiling） | 方向 |
|---|---|---|---|
| CC / OCS workspace | `""` → bypass | `workspace` → acceptEdits | 收紧 ✓ |
| Codex workspace（操作员默认 danger-full-access） | `""` → danger-full-access | `workspace` → workspace-write × on-request | 收紧 ✓ |
| Codex workspace（操作员 read-only） | `""` → read-only | `workspace` ceiling → 仍 read-only | 不变 ✓（ceiling 保护） |
| ACP workspace | `""` → 操作员配置 | `workspace` → approve=false | 收紧 ✓ |
| cron / 平台 channel | `""` | `""`（不变） | 不变 ✓ |

**结论**：注入值作为 permissiveness ceiling，由各 Worker clamp——对操作员默认配置是收紧，对操作员更严配置保持不变，**无提权路径**。这正是 r3 review P1（codex 注入 workspace 覆盖 read-only 操作员配置）的修复。

**fail-closed 降级**：`resolveWorkspacePermissionMode` 在 `GetWorkspaceByID` 出错（DB 抖动）时也返回 bridge 默认值（workspace），而非 `""`——对 CC/OCS 而言 `""`→bypass，空降级会在 DB 故障期把 workspace session 静默升到 bypass（r2 P1）。返回默认值即 fail-closed。

## 3. 核心改动

### 3.1 config 默认值（1 处）

`internal/config/config_defaults.go:82`：

```go
// 原
DefaultPermissionMode: "bypass", // no-op since #789 r2 P2 ...
// 新
DefaultPermissionMode: "workspace", // bridge 在 workspace 无显式覆盖时注入此值（r3）
```

> 注：`config` 包不可 import `internal/worker`（依赖方向），沿用现有字面量风格（与 `"claude_code"` / `"sqlite"` 一致），不引入常量引用。

`NormalizePermissionMode`（`worker.go:275`）**不变**：仍 `empty → bypass`，作为运维显式设空时的向后兼容 fallback。Default 既已非空（`"workspace"`），Normalize 后保持 workspace。

### 3.2 bridge 真正消费默认（spec §5 no-op 落地）

`internal/gateway/bridge_worker.go:resolveWorkspacePermissionMode` 的"workspace 无显式覆盖"分支：

```go
if ws.PermissionMode != "" {
    return ws.PermissionMode // 显式覆盖优先（不变）
}
// r3：返回 config 全局默认（workspace），而非 ""。
// 因默认值同档下调，注入即收紧，无提权（见本 spec §2）。
return b.defaultPermissionMode.Load().(string)
```

`workspaceID == ""` 分支（cron / 平台 channel）**保持 `return ""`**（决策 C）。

bridge 侧无需新增接线——`defaultPermissionMode atomic.Value` 已存在（`bridge.go:68`），由 `NewBridge:122` 经 `worker.NormalizePermissionMode` 初始化、`UpdateDefaultPermissionMode:140` 热重载、`gateway_run.go:360-362` 监听 config 变更触发。

### 3.3 handler admin-only 校验（fail-closed）

`internal/gateway/workspace_handlers.go` 的 **Create** 与 **Update**，在现有 `ValidatePermissionMode` 之前加一道 admin 门：

```go
if req.PermissionMode != nil && !h.isAdmin(r, uid) {
    writeAppError(w, http.StatusForbidden, "PERMISSION_DENIED",
        "permission_mode can only be configured by admins")
    return
}
```

语义：

- 非 admin **不传**该字段（`nil`）→ 正常；workspace 走 config 默认 workspace。
- 非 admin **传任意值**（含 `""` 清空）→ **403**。
- admin 可选 4 档 + `""`（清空回默认）。
- `isAdmin` 复用现有 `h.isAdmin(r, uid)`（`workspace_handlers.go:64`，`role==admin && status==active`，同一 channel 身份源）。
- Update 路径 owner 非 admin 仍可改 `name` / `work_dir` / `agent_config_overrides` / `worker_preference`，仅 `permission_mode` 被门控。

## 4. 前端

`webchat/app/components/chat/settings-modal/general-tab.tsx`：

1. `GeneralTabProps` 增 `isAdmin: boolean`；由 `webchat/app/settings/page.tsx` 传入（已有 `currentUser?.role === 'admin'`，参考 `settings/page.tsx:86`）。
2. 非 admin：**隐藏** Permission Mode 控件整块；`handleSave` 在非 admin 时不发送 `permissionMode` 字段（避免触发 403）。
3. 默认选中值 `'bypass'` → `'workspace'`（`useState` 初始值 `:47`、`useEffect` 同步 `:60`、`dirty` 比较 `:74`）。
4. `PERMISSION_MODE_OPTIONS`（`:23-28`）：把 `"(default)"` 标签从 `bypass` 移到 `workspace` 档；`bypass` 文案改为 `Bypass — Full access`。
5. 存量展示：`workspace.permission_mode === ""` 时，非 admin 看到的是隐藏；admin 看到下拉停在 workspace 档（因后端默认即 workspace，UI 与实际生效一致）。

`webchat/app/settings/page.tsx`：定位渲染 `<GeneralTab>` 处，把 `currentUser?.role === 'admin'` 作为 `isAdmin` 传入。

## 5. 注释与文档同步（no-op → 生效）

r2 在多处注释里标注了"no-op / NOT injected"，r3 需逐一修订：

| 文件 | 位置 | 改动 |
|---|---|---|
| `internal/gateway/bridge.go` | `:68` `defaultPermissionMode` 字段注释 | 删 "NOT consumed by resolveWorkspacePermissionMode"，改为"workspace 无显式覆盖时注入此值（r3）" |
| `internal/gateway/deps.go` | `:46` `DefaultPermissionMode` 注释 | 同上 |
| `internal/config/config_types.go` | `:475` `DefaultPermissionMode` mapstructure 注释 | 删 "no-op"，改为"workspace 无覆盖时注入；缺省 workspace（r3）" |
| `cmd/hotplex/gateway_run.go` | `:362` 热重载日志附近 | 无需改逻辑，注释如有"no-op"一并清 |
| `internal/worker/worker.go` | `:275` `NormalizePermissionMode` doc | consumer 列表追加 `resolveWorkspacePermissionMode` |
| `docs/specs/Workspace-Permission-Mode-Spec.md` | 顶部 | 加 r3 横幅声明（r2 正文 §5/§7/§8 保留作历史记录，引用本 spec §2），不逐节改写正文 |
| `docs/reference/admin-api.md` | 错误码表 + Workspace 管理端点 | 补 `PERMISSION_DENIED`（403）错误码 + `permission_mode` admin-only 标注（非 admin 传 → 403） |

## 6. 向后兼容

| 维度 | r3 行为 |
|---|---|
| 存量 `permission_mode IS NULL` 的 workspace | 自动按 workspace 档运行（收紧） |
| 显式配置过 `permission_mode` 的 workspace | 不变（显式覆盖优先） |
| cron / 无 workspace session | 不变（注入 `""`） |
| `SkipPermissions` 别名 | 不变 |
| 运维 `config.worker.default_permission_mode: ""` | `NormalizePermissionMode` → workspace（与 Default 种子一致；r2 的 ""→bypass 回退已退役——显式设空不应重新引入 bypass 注入） |
| 运维想全局回到 bypass | 显式 `config.worker.default_permission_mode: bypass` 即可（workspace session 全跑 bypass） |
| 非 admin 已持有 permission_mode 的 workspace | 数据不动；下次该 workspace 起session 按库里值；非 admin 仅失去"改"的权限 |

**零数据迁移**：不改 DB schema，不改存量行，纯运行时默认值切换。

> **存量 bypass 残留（已知项，非自动迁移）**：r2 UI 把 `Bypass — Full access (default)` 作为下拉首选项，故升级前打开过 General 选项卡并保存的 workspace 多数存了显式 `permission_mode="bypass"`。r3 的 ceiling 注入只覆盖 `PermissionMode==""` 的 workspace；**显式存了 `bypass` 的 workspace 升级后仍跑 bypass**——r3 收紧不触及它们。运维若要对这些 workspace 也收紧，需由 admin 显式 PATCH 清空（`permission_mode=""`）以回落到 workspace 默认。不做自动数据迁移，因静默改写存储值可能违背 operator 当初的显式选择。

## 7. 测试策略

| 测试 | 文件 | 断言 |
|---|---|---|
| config 默认值 | `internal/config` | `Default().Worker.DefaultPermissionMode == "workspace"` |
| bridge 无覆盖 → 默认 | `internal/gateway/bridge_worker_test.go` | workspace 存在且 `PermissionMode==""` → 返回 `"workspace"`（或 bridge 配置的默认值） |
| bridge 显式覆盖优先 | 同上 | workspace `PermissionMode=="auto-edit"` → 返回 `"auto-edit"` |
| bridge 无 workspace → `""` | 同上 | `workspaceID==""` → `""`（cron/平台保护） |
| bridge 热重载默认 | 同上 | `UpdateDefaultPermissionMode("read-only")` 后，无覆盖 workspace 返回 `"read-only"` |
| handler 非 admin Create 传字段 → 403 | `internal/gateway/workspace_handlers_test.go` | `PERMISSION_DENIED` |
| handler 非 admin Update 传字段 → 403 | 同上 | `PERMISSION_DENIED` |
| handler admin Create/Update 传字段 → OK | 同上 | 4 档 + `""` 均通过 |
| handler 非 admin 不传字段 → OK | 同上 | workspace 创建成功，走默认 |
| 现有"无覆盖→empty"断言回归 | `permission_mode_test.go` / `bridge_worker_test.go` | 凡断言"无覆盖返回空串"的用例改为返回默认值（r3 语义） |

## 8. 实施清单

**后端**
- [x] `internal/config/config_defaults.go`：`DefaultPermissionMode` `"bypass"` → `"workspace"` + 注释
- [x] `internal/gateway/bridge_worker.go`：`resolveWorkspacePermissionMode` 无覆盖分支改返回 `b.defaultPermissionMode.Load().(string)`
- [x] `internal/gateway/workspace_handlers.go`：Create + Update 增 admin-only 门（`PERMISSION_DENIED` 403）
- [x] `internal/gateway/bridge.go` / `deps.go`：`defaultPermissionMode` 相关注释去 no-op
- [x] `internal/config/config_types.go` / `cmd/hotplex/gateway_run.go`：注释去 no-op
- [x] `internal/worker/worker.go`：`NormalizePermissionMode` doc 更新 consumer

**前端**
- [x] `webchat/app/components/chat/settings-modal/general-tab.tsx`：`isAdmin` prop + 非 admin 隐藏控件 + 默认 workspace + options 标签
- [x] `webchat/app/settings/page.tsx`：传 `isAdmin` 给 `<GeneralTab>`

**测试**
- [x] config 默认值断言
- [x] bridge resolve 三分支（无覆盖/覆盖/无 workspace）+ 热重载
- [x] handler admin-only 四象限（非admin传/不传 × Create/Update）

**文档**
- [x] `docs/specs/Workspace-Permission-Mode-Spec.md` 顶部加 r3 横幅（r2 正文保留作历史记录，引用本 spec）
- [x] `docs/reference/admin-api.md`：补 `PERMISSION_DENIED` 错误码 + `permission_mode` admin-only 标注

### 8.1 r3 code-review 修复（max effort review）

| 修复 | 文件 | 改动 |
|---|---|---|
| **P1 codex 提权** | `internal/worker/codexcli/worker.go` | `buildThreadStartParams` 把 session tier 从"无条件覆盖"改为 **ceiling**：`codexSandboxRank`/`codexApprovalRank` 取 `max(rank(session), rank(operator))`，杜绝注入 workspace 覆盖操作员 read-only |
| **P1 DB 抖动 fail-open** | `internal/gateway/bridge_worker.go` | `GetWorkspaceByID` 出错分支返回 bridge 默认值（workspace）而非 `""`（CC/OCS `""`→bypass），fail-closed |
| **P2 config validate 误绿** | `cmd/hotplex/config_cmd.go` | `Validate()` 硬错误与 `Warnings()` 拆分：硬错误 `✗` + 非零退出（对齐 `gateway_run`），警告 `⚠` 不阻断 |
| **P3 webchat 无条件发送 permMode** | `webchat/.../general-tab.tsx` | `permissionMode` 仅在 dirty 时发送，name-only save 不触 admin 门字段 |
| **P3 Update 重复 isAdmin** | `internal/gateway/workspace_handlers.go` | admin 权威 memoize（`resolveAdmin` 闭包），admin 改他人 workspace 带 permMode 仅 1 次 idp.Lookup |
| **P3 注释去 stale** | `worker.go` / `bridge_worker.go` | 删 "not an escalation" 伪断言，改为 ceiling 契约描述 |

**新增测试**：`TestBuildThreadStartParams_PermissionCeiling`（4 例：收紧/不提权/空/显式 bypass 不放宽）；`TestResolveWorkspacePermissionMode` 的 fetch-error 用例改为断言降级到默认值。

## 9. 非目标

- 不改 DB schema（不加列、不改 migration）。
- 不引入 per-worker-type 默认（`config.worker.default_permission_mode` 仍是单值；r2 §11 留的"按 worker-type 细化"留待将来）。
- 不改 cron / 平台 channel session 的权限行为。
- 不做运行时实时切换（启动锁定语义不变）。
- **不改 `/perm` 运行时命令**：`worker_cmds.go:handleSetPermMode` 是 pre-existing 的运行时 per-session worker IPC（`/perm bypass` / `$perm`），作用于**当前 live session 的 worker 进程**，不落库、不写持久 `Workspace.PermissionMode`。r3 的 admin-only 范围仅覆盖持久字段的 Create/Update（决策 B）；`/perm` 是另一安全边界，其是否收归 admin 留作单独 follow-up，不在本 PR。
