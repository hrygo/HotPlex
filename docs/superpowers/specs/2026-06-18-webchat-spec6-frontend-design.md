# WebChat 多租户 spec ⑥ — 前端一等公民化设计方案

**日期**: 2026-06-18
**状态**: 提案中
**跟踪 issue**: [#760](https://github.com/hrygo/hotplex/issues/760)
**分支**: `feat/webchat-spec6-frontend`
**基本设计图**: [`WebChat-Multitenancy-Roadmap-Spec.md`](../../specs/WebChat-Multitenancy-Roadmap-Spec.md)

---

## 1. 目标与定位

本设计方案将 WebChat 前端从"匿名 SPA"升级为完整多租户一等公民 UI。同时解除此前在后端的遗留接线 (A1-A5)，确保端到端的数据流与多租户权限隔离。

---

## 2. 方案概览

### 2.1 强制登录 (Force Login)
1. 废除原有的匿名 `webchat_user` 自动会话创建机制。
2. 任何未登录的用户访问 `/` 时，都会通过 `/api/auth/me` 的状态返回（401 Unauthorized）重定向到 `/login`。
3. 登录页包含本地账号登录（Username/Password）与 OAuth SSO 登录。

### 2.2 工作空间隔离与会话过滤 (Workspace Selection)
1. 采用双侧边栏设计：
   - **左侧工作空间栏** (Workspace Sidebar)：展示用户所拥有的所有 Workspace。图标以精美的渐变色背景与首字母展示。底部包含设置齿轮与登出按钮。
   - **右侧会话列表栏** (Session Sidebar)：仅展示当前选定 Workspace 绑定下的 Chat 历史会话（通过 `/api/sessions?workspace_id=XXX` 过滤）。
2. 工作空间之间完全隔离。切换工作空间将只加载绑定到该空间的会话，如果该空间下无会话则自动静默或显式创建第一个会话。

### 2.3 设置页 (Settings Modal)
点击左侧下方的齿轮，弹出 Workspace & User 设置悬浮层：
- **General (常规)**: 查看/修改 Workspace 名称。展示不可变的工作目录绝对路径 (`work_dir`)。
- **AI Config (AI 配置)**:
  - 偏好 Worker 类型选择（切换 `claude_code` / `codexcli` 等，修改 `worker_preference`）。
  - Workspace 级 `agent_config_overrides`（YAML 格式文件编辑，校验后调用 `PATCH /api/workspaces/{id}`）。
- **Members (成员管理, 仅限 App Admin 角色展示)**:
  - **用户列表**：列出系统中的用户，提供启用/禁用（Status: active/disabled）操作。
  - **邀请码管理**：列出当前未失效的邀请码，支持新建邀请码（自定义 Role 与 TTL）以及删除邀请码。
- **Profile (个人资料)**: 查看当前用户身份、所属角色等。

### 2.4 后端潜伏接线 (A1-A5)
- **A1**: 在 `routes.go` 中注册 `/api/workspaces*` CRUD 路由，绑定到 `WorkspaceHandlers`。
- **A2/A5**: WebSocket Handshake 对齐：
  1. 客户端在 `init` 协议载荷中发送 `workspace_id`；
  2. 后端 `conn.go` 接收 `workspace_id`，查询其 `WorkDir` 并使用正确的 `workspace_id` 派生 `sessionID`（解决 DeriveSessionKey 派生分叉）。
- **A3**: per-request 状态校验：在 `AuthenticateRequest` 拦截点增加 IdentityProvider 用户状态查询，如果 `Status == "disabled"` 则拒绝访问。
- **A4**: Cookie HMAC 秘钥持久化：从 `~/.hotplex/data/cookie_secret.key` 读写密钥（若不存在则自动生成），支持 `security.cookie_secret` 配置覆盖。

---

## 3. 详细设计与实现

### 3.1 路由注册与权限校验
1. **工作空间路由**
   - `POST /api/workspaces` -> `Create`
   - `GET /api/workspaces` -> `List`
   - `GET /api/workspaces/{id}` -> `Get`
   - `PATCH /api/workspaces/{id}` -> `Update`
   - `DELETE /api/workspaces/{id}` -> `Delete`
   - 并注册对应的 `OPTIONS` 跨域预检路由。
2. **App 级 Admin 路由**
   - `POST /api/admin/invitations` -> `AdminCreateInvitation`
   - `GET /api/admin/invitations` -> `AdminListInvitations`
   - `DELETE /api/admin/invitations/{id}` -> `AdminDeleteInvitation`
   - `GET /api/admin/users` -> `AdminListUsers`
   - `PATCH /api/admin/users/{id}` -> `AdminUpdateUserStatus`
   - 并注册对应的 `OPTIONS` 跨域预检路由。
3. **IdentityProvider 注册**
   - 在 `routes.go` 初始化 `lap` 后，调用 `auth.SetIdentityProvider(lap)`，确保 `AuthenticateRequest` 和 `isAdmin` 可以正确查找到 IDP。

### 3.2 秘钥持久化与热更新
在 `CookieAuth` 构造方法中：
1. 优先使用配置传入的 `security.cookie_secret`，经过 SHA256 派生 32 字节密钥。
2. 若为空，从 `~/.hotplex/data/cookie_secret.key` 读取，失败则生成新密钥并以 0600 权限保存为 64 位十六进制字符串。

### 3.3 禁用状态即时拦截
在 `Authenticator.AuthenticateRequest` 中：
1. 提取到 `uid` 后，如果 `idp != nil && uid != "anonymous" && uid != "api_user"`，执行 `idp.Lookup(r.Context(), uid)`。
2. 若 `u.Status == "disabled"` 或发生错误，直接返回 `ErrUnauthorized`。

### 3.4 前端客户端升级
1. `types.ts` 和 `envelope.ts` 扩展 `workspace_id`。
2. `useSessions` 接收 `workspaceId`，重新渲染和过滤历史，传递给 `createSession` 和 WebSocket 初始化握手。

---

## 4. 界面与交互体验

### 4.1 登录界面 (Login Page)
- 精细打磨的玻璃拟态卡片，使用带发光动画的品牌 Logo。
- 动态获取 OAuth 供应商，渲染按钮组。
- 如果 URL 附带 `auth_error`，在顶部呈现红色气泡通知。

### 4.2 双侧边栏布局 (Double Sidebar)
- 最左侧为 64 像素的极简导航条，展示工作空间圆头像、偏好按钮及当前状态。
- 会话面板仅列出本工作空间下的 Chat，与旧 UI 的风格完全统一，支持平滑淡入淡出动效。

### 4.3 设置与成员中心
- 使用 Tab 切换的模态框展示。
- 只有角色为 `admin` 的用户才可以看见 "Members" 选项卡，其中表格支持分页与按钮快捷操作。
