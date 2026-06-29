# Admin 控制台 — Workspace Permission Mode 管理

**状态**: Implemented · **日期**: 2026-06-29 · **关联 issue**: #807 · **关联**: [Workspace-Permission-Mode-Spec.md](./Workspace-Permission-Mode-Spec.md)、[Workspace-Permission-Mode-Admin-Only-Revision-Spec.md](./Workspace-Permission-Mode-Admin-Only-Revision-Spec.md)（r3）· **版本目标**: v1.31.x

---

## 1. 背景与动机

r3（PR #805）已把 workspace `permission_mode` 收紧为 **admin-only**：

- 配置层：`config.worker.default_permission_mode` 真正被 bridge 消费，缺省 `bypass` → `workspace`。
- 接口层：`PATCH /api/workspaces/{id}` 对非 admin 改 `permission_mode` 返回 403 `PERMISSION_DENIED`。

但 r3 只解决了"谁能改"的**权限**问题，没解决"admin 怎么改"的**操作面**问题：

1. **没有 admin 入口**：`/admin` 视图（Dashboard / Bots / Sessions / Cron / API Keys / Settings）没有 Workspaces 页面。admin 想给某个用户的 workspace 设 `permission_mode`，只能用 `curl PATCH /api/workspaces/{id}` 手敲——既不直观，也走错了鉴权通道（`/api/workspaces` 是用户自助 REST，走 `Authenticator`；admin 视图应走 `admin-client` 双通道）。
2. **没有全局视图**：`GET /api/workspaces` 只返回 caller 自己的 workspace（`ListWorkspacesByOwner(uid)`）。store 层有 `ListAllWorkspaces` 但 HTTP 层从未暴露给 admin。admin 无法一眼看到"全平台所有 workspace 当前跑在什么权限档"。
3. **没有可读标识**：workspace 的 `OwnerUserID` 是 UUID，admin 无法从 UUID 识别"这是谁的 workspace"。

本 spec 解决这三点：给 admin 一个**只读列表 + 可改 permission_mode** 的 `/admin/workspaces` 控制台页面，列表带 owner 可读标识（display_name + username）。

## 2. 范围（已与 stakeholder 确认）

| 决策点 | 选择 | 理由 |
|---|---|---|
| 管理范围 | **最小化**：列表 + inline 改 permission_mode | 聚焦诉求，不做删除/详情页（YAGNI；删除已有 `/api/workspaces/{id}` 路径） |
| 生效时机 | **仅影响新建 session** | 与 `resolveWorkspacePermissionMode` 现有热重载语义一致；不打断用户进行中的对话 |
| 用户侧可见性 | **只读展示当前 mode** | 用户知情权，避免"为什么我不能改文件"的困惑；r3 的"完全隐藏"改为"只读 badge" |

**明确不做**：
- ❌ admin 删除 workspace（复用现有 `/api/workspaces/{id}` DELETE 即可，不进 admin 控制台）
- ❌ "立即生效"按钮（kill 活跃 session 重启）——会中断用户对话，超出最小化范围
- ❌ workspace 详情页（owner/创建时间/活跃 session 数等只读视图）——后续可加，本期 YAGNI

## 3. 核心改动

### 3.1 后端：`GET /admin/workspaces`（列出所有 workspace + owner 标识）

**路由**：`GET /admin/workspaces`（注册于 `cmd/hotplex/routes.go` 的 `adminMux`，走 `AdminAPI.Middleware` 鉴权——Bearer + cookie fallback，admin-only）。

**store 层新增**（`internal/session/multitenancy_store.go`，SQLiteStore + pgStore 双实现）：

```go
// AdminWorkspaceView 是 admin 列表的投影：workspace 字段 + owner 可读标识。
// 避免 admin 拿着 UUID 无法识别归属（spec §1 动机 3）。
type AdminWorkspaceView struct {
    Workspace               // 嵌入：ID/OwnerUserID/Name/WorkDir/PermissionMode/Status/...
    OwnerDisplayName string // join users.display_name
    OwnerUsername    string // join users.username
}

// ListAllWorkspacesWithOwner 返回所有 workspace 并 join users 带可读标识。
// 供 admin 控制台全局视图使用（区别于 ListAllWorkspaces 的纯裸行，用于
// gateway 启动扫描的 stale override 检测，不带 owner）。
func (s *SQLiteStore) ListAllWorkspacesWithOwner(ctx context.Context) ([]*AdminWorkspaceView, error)
```

SQL（SQLiteStore，pgStore 同构换占位符）：

```sql
SELECT w.id, w.owner_user_id, w.name, w.work_dir, w.agent_config_overrides,
       w.worker_preference, w.permission_mode, w.status, w.created_at, w.updated_at,
       u.display_name, u.username
FROM workspaces w
LEFT JOIN users u ON u.id = w.owner_user_id
ORDER BY u.display_name, w.name;
```

> `LEFT JOIN` 而非 `INNER`：防御性地容忍 owner user 行被并发删除的窗口（workspace 仍应可见，owner 字段为空字符串）。正常路径下 FK 保证 owner 存在。

**响应**（`admin.WorkspaceListResponse`，定义于 `internal/admin/models.go`）：

```jsonc
{
  "workspaces": [
    {
      "id": "ws-uuid",
      "owner_user_id": "user-uuid",
      "owner_display_name": "张三",
      "owner_username": "zhangsan",
      "name": "我的项目",
      "work_dir": "/home/u/.hotplex/workspaces/user-uuid/my-project",
      "permission_mode": "workspace",       // "" = 走 config 默认
      "status": "active",
      "created_at": 1719600000,
      "updated_at": 1719600000
    }
  ]
}
```

**分页**：workspace 总量预期不大（admin 后台），首版**不分页**，全量返回（与 `ListAllWorkspaces` 现状一致）。若后续 api-key 通道程序化创建导致量级膨胀，再加 `web.ParsePagination` 下推——届时同步改 `ListAllWorkspacesWithOwner` 签名。

### 3.2 后端：`PATCH /admin/workspaces/{id}`（admin 改 permission_mode）

**路由**：`PATCH /admin/workspaces/{id}`（`adminMux`，admin-only）。

**请求体**（仅允许改 permission_mode，最小化范围）：

```jsonc
{ "permission_mode": "read-only" }   // 4 档之一；"" = 清除覆盖，回落 config 默认
```

**handler 逻辑**（新文件 `internal/admin/workspace_handlers.go`）：

```go
type updateAdminWorkspaceRequest struct {
    PermissionMode *string `json:"permission_mode"` // nil = omit (400); 必须显式提供
}

func (h *AdminWorkspaceHandlers) Update(w http.ResponseWriter, r *http.Request) {
    // 1. 解析 body：permission_mode 字段必须存在（nil → 400，避免误以为能改其它字段）
    // 2. worker.ValidatePermissionMode(*req.PermissionMode) → 400 INVALID_PERMISSION_MODE
    // 3. store.GetWorkspaceByID → 404 WORKSPACE_NOT_FOUND
    // 4. ws.PermissionMode = *req.PermissionMode
    // 5. store.UpdateWorkspace(ws, now) → CAS 冲突 409 WORKSPACE_VERSION_MISMATCH
    // 6. respondJSON(ws) —— 返回更新后的 workspace 裸行（不带 owner join，足够）
}
```

**与 `/api/workspaces/{id}` PATCH 的关系**：
- `/api/workspaces/{id}` 是**用户自助**端点（owner 自己改 name/work_dir/agent_config 等；permission_mode 字段对非 admin 403，对 admin 放行）。保留不动。
- `/admin/workspaces/{id}` 是**admin 控制台**端点（**只**能改 permission_mode，走 admin 鉴权通道）。
- 两者职责不同，不合并。admin 控制台不暴露 name/work_dir 修改，避免 admin 误改用户的工作目录。

**生效语义**：`UpdateWorkspace` 写库后，**运行中 session 不受影响**（worker 进程已启动，权限参数已注入）。新 session（新对话 / `/reset` 后）初始化时 `resolveWorkspacePermissionMode` 会读到新值。UI 上对 admin 标注"对新会话生效"。

### 3.3 后端：审计接入

`internal/admin/audit.go`：

1. 新增动作枚举：
   ```go
   AuditWorkspacePermissionModeUpdate = "workspace.permission_mode.update"
   ```
2. `adminActionFor` 注册路径匹配（新增 `workspaceAction` helper，或直接在 switch 加 case）：
   ```go
   case strings.Contains(path, "/workspaces") && method == http.MethodPatch:
       return AuditWorkspacePermissionModeUpdate
   ```
   > 注意顺序：`/workspaces` 不与现有 `/sessions`/`/bots`/`/cron`/`/api-keys` 子串冲突，放在 switch 末尾即可。

写操作由 middleware 级 `admin_audit` slog 统一记录（`actor` = uid 或 `admin-token`，`target` = `/admin/workspaces/{id}`，`result` = ok/failed）。**读操作（GET list）不审计**（与现有 admin 读端点一致）。

### 3.4 前端：`/admin/workspaces` 页面

**新页面** `webchat/app/admin/workspaces/page.tsx`：

表格列：

| 列 | 字段 | 说明 |
|---|---|---|
| Owner | `owner_display_name (owner_username)` | **主标识**，admin 据此识别归属 |
| Workspace | `name` | 用户起的工作区名 |
| Work Dir | `work_dir` | 截断展示，title 显全路径 |
| Permission Mode | `permission_mode` | 当前档位；inline 下拉选择器 |
| 操作 | — | 下拉选档 + 保存按钮 |

**Permission Mode 下拉**：5 个选项（4 档 + "默认（workspace）"）。`""` 在 UI 显示为"默认（workspace）"并标注"= config 全局默认"。

**交互**：admin 在某行下拉选档 → 点保存 → 调 `PATCH /admin/workspaces/{id}` → 成功后该行 badge 更新 + toast 提示"已更新，对新会话生效"；失败 toast 显错误码。

**权限提示**：页面顶部固定提示条"修改仅对新会话生效；运行中的会话保持原权限模式"。

**admin-nav 入口**：`webchat/components/admin/admin-nav.tsx` 的 `NAV_ITEMS` 加一项（图标用 folder/document 集，置于 Sessions 之后）：
```ts
{ label: 'Workspaces', href: '/admin/workspaces', exact: false }
```

### 3.5 前端：API 模块

新文件 `webchat/lib/api/admin-workspaces.ts`（走 `adminFetch` 双通道）：

```ts
export async function listAdminWorkspaces(): Promise<AdminWorkspace[]>
export async function updateAdminWorkspacePermissionMode(
  id: string, permissionMode: string,
): Promise<Workspace>
```

类型定义补到 `webchat/lib/types/admin.ts`：`AdminWorkspace`（含 owner_display_name/owner_username）。

### 3.6 前端：普通用户侧 GeneralTab 只读展示

r3 把 `permission_mode` 控制从 `GeneralTab` **完全隐藏**（非 admin 不渲染）。本 spec 改为**只读 badge 展示**：

- admin 用户：保持 r3 的可编辑下拉（不变）。
- 非 admin 用户：渲染只读 badge（如 `权限模式: workspace`），**无修改入口**。值来自 workspace 的 `permission_mode`（`""` 显示"默认"）。

> 落点：`webchat/app/components/chat/` 下 GeneralTab 相关组件。需读 workspace 当前 `permission_mode`（用户侧 `GET /api/workspaces/{id}` 已返回该字段）。

## 4. 安全考量

| 威胁 | 缓解 |
|---|---|
| 非 admin 越权调 `PATCH /admin/workspaces/{id}` | `AdminAPI.Middleware` 强制 admin（role==admin && status==active）；handler 层不重复校验（middleware 已保证） |
| 非 admin 越权调 `GET /admin/workspaces` | 同上，middleware 拦截 |
| admin 误改用户 work_dir | `/admin/workspaces/{id}` PATCH **只接受** `permission_mode` 字段，work_dir/name 等忽略；改 work_dir 仍走用户自助 `/api/workspaces/{id}` |
| owner 隔离破坏 | admin 改 permission_mode 不涉及 sandbox 路径校验（permission_mode 是 worker 行为参数，非文件系统权限），无越权风险 |
| 普通用户侧只读展示泄露 | 非 admin 只读展示的是**自己的** workspace mode（已属己有数据），无越权；不展示他人 workspace |

## 5. 数据契约汇总

| 端点 | 方法 | 鉴权 | 用途 |
|---|---|---|---|
| `/admin/workspaces` | GET | admin（middleware） | 列出所有 workspace + owner 标识 |
| `/admin/workspaces/{id}` | PATCH | admin（middleware） | 改单个 workspace 的 permission_mode |
| `/api/workspaces` | GET | 用户（Authenticator） | 不变：列自己的 |
| `/api/workspaces/{id}` | PATCH | 用户（Authenticator） | 不变：改自己的；permission_mode 仍 admin-only 403 |

错误码（`/admin/workspaces/{id}` PATCH）：
- `400 INVALID_PERMISSION_MODE` — 档位非法
- `400 BAD_REQUEST` — body 缺 `permission_mode` 字段
- `404 WORKSPACE_NOT_FOUND` — id 不存在
- `409 WORKSPACE_VERSION_MISMATCH` — CAS 冲突，提示重新拉取

## 6. 测试

**后端**（`internal/admin/workspace_handlers_test.go` + store test）：
- `GET /admin/workspaces`：admin 返回全量 + owner join；非 admin 401/403；owner user 缺失时（LEFT JOIN）owner 字段为空且 workspace 仍返回。
- `PATCH /admin/workspaces/{id}`：4 档合法值通过；非法值 400；缺失字段 400；不存在 404；CAS 冲突 409；审计记录写入（`workspace.permission_mode.update`）。
- store：`ListAllWorkspacesWithOwner` SQLite + PG 双实现一致性（table-driven）。

**前端**：
- `/admin/workspaces` 页面渲染列表 + inline 修改流程（mock adminFetch）。
- GeneralTab 非 admin 只读 badge 渲染、无修改入口。
- admin-nav 新入口高亮。

## 7. 文档

- `docs/reference/admin-api.md`：补充 `GET /admin/workspaces` 与 `PATCH /admin/workspaces/{id}` 契约（请求/响应/错误码），标注 admin-only + permission_mode 4 档语义。
- 本 spec 实施后状态改 `Implemented`，关联 PR。

## 8. 实施清单

**后端**
- [ ] `internal/session/multitenancy_store.go`：新增 `AdminWorkspaceView` + `ListAllWorkspacesWithOwner`（SQLiteStore + pgStore）
- [ ] `internal/admin/workspace_handlers.go`：新增 `AdminWorkspaceHandlers`（List + Update）
- [ ] `internal/admin/models.go`：新增 `WorkspaceListResponse`
- [ ] `internal/admin/audit.go`：新增 `AuditWorkspacePermissionModeUpdate` + `adminActionFor` 路径匹配
- [ ] `cmd/hotplex/routes.go`：注册 `GET /admin/workspaces` + `PATCH /admin/workspaces/{id}`
- [ ] 后端测试 + `make check`

**前端**
- [ ] `webchat/lib/types/admin.ts`：`AdminWorkspace` 类型
- [ ] `webchat/lib/api/admin-workspaces.ts`：list + patch
- [ ] `webchat/app/admin/workspaces/page.tsx`：表格 + inline 修改
- [ ] `webchat/components/admin/admin-nav.tsx`：加 Workspaces 入口
- [ ] `webchat/app/components/chat/` GeneralTab：非 admin 只读 badge
- [ ] `cd webchat && npx tsc --noEmit`

**文档**
- [ ] `docs/reference/admin-api.md` 补两端点契约
- [ ] 本 spec 状态 → Implemented
