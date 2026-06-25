# WebChat Admin 后台收口与功能重排

**状态**: 设计草案，待评审
**日期**: 2026-06-25
**关联**: `WebChat-Multitenancy-Foundation-Design-Spec.md`、`WebChat-Workspace-Create-WorkDir-Prefix-Spec.md`、`premium-ux-sdk-integration.md`

---

## 1. 背景

`/admin` 是一套**独立于 chat 的管理后台**，与普通用户的 `/settings` 并行存在。当前存在四类问题：

### 1.1 越权入口（安全问题）

聊天侧边栏 `SessionPanel.tsx:226-241` 渲染了一个 "Admin" 入口（`<a href="/admin">`），**对所有已登录用户可见，无角色守卫**。`SessionPanelProps`（`SessionPanel.tsx:118-125`）不包含 user/role 字段，组件层无法判断。

普通用户点击后被引导到 `/admin/login`。登录页（`admin/login/page.tsx:13-37`）只做 `testConnection`（probe `/admin/health`），**不校验当前用户角色**；`AdminShell`（`admin-shell.tsx:14-19`）的鉴权也只 probe token 有效性。于是一旦任何人填入一个能连上网关的 admin token，就进入后台——`/admin/login` 成为越权攻击面（社会工程 / token 泄露 / 暴力试探）。

### 1.2 双轨鉴权混乱

admin 后台使用**独立的 admin token**（`adminUrl` + Bearer，存 localStorage，见 `use-admin-auth.ts`、`lib/api/admin-client.ts`），与 chat 的 cookie session 是两套完全独立的凭证体系。同一个 admin 用户在 `/settings` 用 cookie、在 `/admin` 要另填 token，体验割裂；且两条通道在后端分别由不同中间件保护，策略漂移风险高。

### 1.3 功能重复

| 能力 | `/admin/*`（admin token 通道） | `/settings`（cookie 通道） |
|---|---|---|
| 成员管理（邀请/启停/删除） | `admin/members/page.tsx` | `settings-modal/members-tab.tsx` |
| Workspace 配置 | （已清理） | `settings-modal/general-tab.tsx` |
| AI 配置 | （已清理） | `settings-modal/ai-config-tab.tsx` |

`/admin/members` 与 `/settings?tab=members` 是**同一能力的两套 UI + 两个数据通道**。更糟的是 `admin/members/page.tsx:150` 判 `currentUser?.role !== 'admin'`，但独立后台根本拿不到 chat session 的 `currentUser`，该守卫形同虚设。

### 1.4 运维功能面向错误受众

Dashboard 的 "Restart Go Gateway"（`admin/page.tsx:219`，含 4 步动画轮询面板）是网关生命周期操作，属于平台运维，不应暴露给从 chat 进入的终端用户。

## 2. 目标

- **G1 入口收口（安全）**：`/admin` 入口仅对 `role === 'admin'` 的用户可见；非 admin 访问 `/admin/*` 被重定向回 `/`，不展示登录页。
- **G2 鉴权收敛**：webchat 内嵌场景下 `/admin` 复用 chat 的 cookie session + 后端 `requireAdmin`，消除"另填一次 admin token"的割裂体验；独立 admin token 仅保留为远程网关运维的 fallback。
- **G3 功能去重**：废弃 `/admin/members`（能力已存在于 `/settings`）；`/admin/settings`（实为连接配置）重命名/收缩，消除命名误导。
- **G4 危险操作治理**：Dashboard 重启等高危动作增加角色二次校验 + 操作确认。
- **G5 操作审计**：admin 后台高危操作（重启、删 bot、删 API key、改成员状态）写入审计日志。

## 3. 非目标

- **不改后端 admin API 契约**：`/api/admin/*` 路径与 payload 保持兼容，保护 SDK / CLI 消费者。
- **不拆除独立 admin token 通道**：远程运维场景（本地 CLI 连远程网关）仍需要它，仅做"内嵌场景优先 cookie"的路由选择。
- **不重构 Bots / Sessions / Cron / API Keys 的功能本身**：它们是合理的系统级能力，本次只做归属明确化与权限收口。
- **不做审计日志的可视化看板**：本次只落日志写入，查询 UI 后续迭代。

## 4. 决策记录

| 决策点 | 选择 | 理由 |
|---|---|---|
| `/admin` 入口可见性 | 仅 `role === 'admin'` | 最小权限；普通用户无业务诉求进入后台 |
| 鉴权通道 | 内嵌用 cookie session + `requireAdmin`；独立 token 降级为远程运维 fallback | 统一体验、收敛攻击面，不破坏远程运维 |
| `/admin/members` | 废弃，重定向到 `/settings?tab=members` | 能力已存在于 `/settings`，消除双份维护 |
| `/admin/settings` | 重命名为 "Admin Connection"，定位为 token/远程网关管理 | 当前命名（Settings）与功能不符 |
| 重启网关 | 保留但加角色守卫 + 显式二次确认 | 危险操作不应面向终端用户随手触发 |
| 审计日志 | 复用现有日志骨架（slog JSON），新增 `admin_audit` 结构化字段 | 不引入新存储组件 |

## 5. 设计

### 5.1 前端：入口与路由守卫

#### 5.1.1 SessionPanel 透传 role

`SessionPanelProps` 增加 `currentUserRole?: 'admin' | 'user'`，由 `ChatContainer.assistant-ui.tsx` 从已获取的 `currentUser` 透传。"Admin" 入口（`SessionPanel.tsx:226-241`）外层加 `{currentUserRole === 'admin' && (...)}` 守卫。

#### 5.1.2 AdminShell 角色 guard

`admin-shell.tsx` 在 `useAdminAuth` 之外，额外读取 chat session 的 currentUser（`getMe()`）：
- `role !== 'admin'` → `router.replace('/')`，不渲染后台、不渲染登录页。
- 登录页 `/admin/login` 同样加该 guard，非 admin 直接跳走（关闭越权入口）。

#### 5.1.3 内嵌场景免二次登录

`AdminShell` 探测到 cookie session 且 `role === 'admin'` 时，优先用 cookie 通道（`/api/admin/*` 已支持 cookie，见 `lib/api/auth.ts:128` 注释）；仅当 cookie 不可用（远程网关）才回落到 localStorage 的 admin token + `/admin/login`。

### 5.2 前端：功能去重与重命名

| 模块 | 动作 |
|---|---|
| `/admin/members` | 删除页面，路由重定向到 `/settings?tab=members` |
| `/admin/settings` | 重命名为 **Admin Connection**，仅保留 admin token / 远程网关 URL 管理 |
| `admin-nav.tsx` | 移除 Members 项；Connection 项标签更新 |
| `/admin`（Dashboard）| Restart 按钮加 `role === 'admin'` 守卫（后端已强制，前端补齐显式禁用 + 二次确认文案） |

### 5.3 后端：权限校验与审计

#### 5.3.1 确认 `/admin/*` HTTP 层 `requireAdmin`

后端 admin handler 链路（`internal/admin/handlers.go`）对每个 `/api/admin/*` 路由强制 `requireAdmin`（`role==admin && status==active`，见 `lib/api/auth.ts:131` 注释）。本次以前端收口为主，后端校验作为权威兜底复核一遍覆盖度（特别是 `restart`、`delete bot`、`delete api key` 等高危端点）。

#### 5.3.2 admin 操作审计

在 admin handler 的写操作路径插入结构化审计日志：

```go
slog.Info("admin_audit",
    "actor_user_id", actorID,        // 从 session 解析
    "action", "gateway.restart",     // 规范化动作枚举
    "target", "/admin/restart",
    "result", "ok|failed",
    "request_id", reqID,
)
```

动作枚举：`gateway.restart` / `bot.create` / `bot.delete` / `apikey.create` / `apikey.delete` / `member.status.update` / `invitation.create` / `invitation.delete`。日志落现有 slog JSON handler，不新增存储。

### 5.4 归属重排总览

```
系统级（/admin，仅 platform admin，cookie + token 双通道）
├── Dashboard        运维概览 + 重启（危险，二次确认）
├── Bots             多 bot 配置
├── API Keys         凭证管理
├── Sessions         全量会话审计
├── Cron             全局定时任务
└── Admin Connection token / 远程网关管理（原 /admin/settings）

workspace 级（/settings，cookie 通道，workspace admin/owner）
├── General          workspace 名称 / workDir / worker
├── AI Config        agent 配置覆盖
├── Members          成员管理（吸收原 /admin/members）
└── Profile          个人凭证
```

## 6. 验收

- **A1 入口收口**：普通用户（`role=user`）在 chat 侧边栏看不到 Admin 入口；直接访问 `/admin` 或 `/admin/login` 被重定向到 `/`。
- **A2 鉴权收敛**：admin 用户从 chat 进 `/admin` 无需二次填 token（cookie 通道生效）；清掉 cookie 后回落到 admin token 登录。
- **A3 去重**：`/admin/members` 重定向到 `/settings?tab=members`；admin-nav 无 Members 项；原 `/admin/settings` 显示为 Admin Connection。
- **A4 危险操作**：非 admin 触发 `/admin/restart` 在前端被拦截、后端返回 403。
- **A5 审计**：执行 restart / delete bot / delete api key 后，网关日志出现对应 `admin_audit` 结构化记录，字段齐全。
- **A6 回归**：`pnpm tsc --noEmit` 通过；远程运维路径（独立 admin token）仍可登录。

## 7. 阶段拆分

| 阶段 | 范围 | 优先级 |
|---|---|---|
| Phase 1 | A1 + A2：入口收口 + cookie 通道优先 | P0 |
| Phase 2 | A3：Members 去重 + Connection 重命名 | P1 |
| Phase 3 | A4 + A5：危险操作守卫 + 审计日志 | P2 |
| Phase 4 | 文档更新（CLAUDE.md admin 章节 / 用户文档） | P3 |
