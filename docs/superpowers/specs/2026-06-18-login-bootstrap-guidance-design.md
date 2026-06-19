# 登录页新用户引导设计

- 日期: 2026-06-18
- 分支: feat/webchat-spec6-frontend
- 状态: 设计已确认,待写实施计划

## 背景与问题

webchat spec6 多租户登录页(`/login`)当前有 Sign In + Accept Invite 两个 tab,受邀新用户(持有邀请码)可走 Accept Invite 注册。但存在两类缺口:

1. **bootstrap 零用户场景无引导**:全新部署后系统没有任何 admin。访客打开登录页,既不能 Sign In(无账号)也不能 Accept Invite(无 admin 发邀请),陷入死循环,且无任何提示告知需运行 `hotplex admin create` CLI。首个 admin 当前只能通过 `cmd/hotplex/admin_cmd.go` 的 `hotplex admin create` 命令创建。
2. **注册流程缺引导**:受邀者不易发现 Accept Invite 入口、不知邀请码来源;注册成功后无 onboarding;错误提示不够友好。

## 目标

- 全新部署时,登录页引导访客(运维)运行 CLI 创建首个 admin
- 受邀新用户:清晰入口指引、邀请码来源说明、注册后 onboarding、友好错误提示
- 安全:首个 admin 仅经 CLI 创建(服务器访问权即天然鉴权),UI 不开放创建

## 非目标(YAGNI)

- 不在 UI 开放"创建首个 admin"表单(安全风险:谁访问谁成 admin)
- 不做完整多步 onboarding 向导,只做一次性欢迎卡
- 不改动 `admin/login`(旧 admin token 页)

## 设计

### 后端

#### 1. bootstrap 状态端点

`GET /api/auth/bootstrap-status` → `{"bootstrapped": bool}`

- 查询:users 表是否存在 `role='admin'` 的用户
- 注册位置:**独立于 `cmd/hotplex/routes.go:204` 的 `if deps.CookieAuth != nil && deps.WorkspaceStore != nil` 块**。原因:未 bootstrap 时该块可能因 CookieAuth 为 nil 被整体跳过,而此端点正是未 bootstrap 时前端所需;它只依赖 UserStore
- 中间件:挂 `corsMw`(跨源前端访问)
- 鉴权:无需(只暴露 bool,不泄露计数或用户信息)

#### 2. UserStore.HasAdmin

`UserWorkspaceStore` 接口(`internal/session/multitenancy_store.go:61`)新增:

```go
HasAdmin(ctx context.Context) (bool, error)
```

- SQLiteStore:`SELECT 1 FROM users WHERE role='admin' LIMIT 1`
- PGStore:同语义
- `admin_cmd.go` 已使用同一 store 接口,保持一致

### 前端登录页(`webchat/app/login/page.tsx`)

#### 3. bootstrap 检测与引导卡

- 加载时并行调 `getBootstrapStatus()` + `getMe()`(后者已有)
- `bootstrapped=false` → 整页显示 **BootstrapGuide 卡片**(替代登录表单):
  - 标题:"初始化管理员账号"
  - 说明:全新部署,需先创建管理员
  - CLI 命令代码块 + 复制按钮:`hotplex admin create --username <name> --config <path>`
  - 提示:"创建后刷新此页"
- `bootstrapped=true` → 现有双 tab 流程

#### 4. 注册入口指引

- URL 含 `?invite=CODE` → 自动切到 Accept Invite tab 并预填邀请码
- Accept Invite 邀请码字段下加说明:"邀请码由管理员发放"
- Sign In tab 底部加链接:"收到邀请码?立即注册 →"(切到 Accept Invite)

#### 5. 错误友好度

扩展 `mapAuthError`(login/page.tsx)覆盖注册相关码,映射为中文可操作文案:

- `INVALID_USERNAME` / `INVALID_PASSWORD` / `USERNAME_TAKEN`(后端已有,见 `internal/gateway/auth_handlers.go`)
- 邀请码消费错误:实施时核对 AcceptInvite handler 实际返回码(预期含无效/过期/已用)

### onboarding

#### 6. 首次登录欢迎卡

触发机制:login handler 在 `TouchUserLastLogin`(`internal/gateway/auth_handlers.go:84`)**前**读取原 `last_login_at`,若为空则在响应中带 `first_login: true`。前端 `login()` 取此标志,主页据此显示一次性欢迎卡。

欢迎卡内容:

- 简短功能介绍
- "创建你的第一个 workspace" 引导入口

一次性:用户关闭或 `last_login_at` 非 null 后不再显示。

## 边界与安全

- **bootstrap-status 脱离 CookieAuth 守卫**:避免"未 bootstrap → auth 路由不注册 → 前端拿不到状态"死循环(本分支刚修过同类 404 问题)
- 首个 admin 仅 CLI 创建:不开放 UI 表单,杜绝"谁访问谁成 admin"
- `bootstrap-status` 只返回 bool:不暴露用户/admin 计数

## 测试

### 后端

- `bootstrap-status` 端点:空库 → `{"bootstrapped":false}`;有 admin → `true`;无鉴权可访问
- `HasAdmin`:SQLite/PG 各测 true/false 两路

### 前端

- 登录页 `bootstrapped=false` 渲染引导卡(非表单)
- `?invite=CODE` 自动切 tab 并预填
- `first_login` 触发欢迎卡

## 涉及文件(预估)

- `internal/session/multitenancy_store.go` — HasAdmin 接口 + SQLite 实现
- PG store 对应文件 — HasAdmin PG 实现
- `cmd/hotplex/routes.go` — bootstrap-status 路由(独立块)
- `internal/gateway/auth_handlers.go`(或新 handler 文件)— bootstrap-status handler;login 响应带 `first_login`
- `webchat/lib/api/auth.ts` — `getBootstrapStatus()`
- `webchat/app/login/page.tsx` — 检测、引导卡、入口指引、错误映射
- `webchat/app/page.tsx` 或新组件 — onboarding 欢迎卡
