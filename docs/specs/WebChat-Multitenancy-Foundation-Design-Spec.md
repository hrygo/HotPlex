# WebChat 一等公民化与多租户地基设计（spec ①：身份 + workspace + 隔离）

**日期**: 2026-06-15
**状态**: Draft（待实现计划）
**分支**: main · **基线版本**: v1.29.0 (fb857af1)
**范围**: 后端地基 —— 用户身份系统、workspace 数据模型、会话隔离打通、多租户配额框架
**作者**: brainstorming session → 设计文档

---

## 目录

- [1. 背景与现状](#1-背景与现状)
- [2. 目标与非目标](#2-目标与非目标)
- [3. 关键决策汇总](#3-关键决策汇总)
- [4. 架构总览](#4-架构总览)
- [5. 数据模型](#5-数据模型)
- [6. session key 派生改造](#6-session-key-派生改造)
- [7. 认证流程与 IdentityProvider](#7-认证流程与-identityprovider)
- [8. workspace 生命周期与会话隔离](#8-workspace-生命周期与会话隔离)
- [9. 配额扩展](#9-配额扩展)
- [10. API 端点清单](#10-api-端点清单)
- [11. 错误码](#11-错误码)
- [12. 迁移策略](#12-迁移策略)
- [13. 测试策略](#13-测试策略)
- [14. 后续 spec 路线](#14-后续-spec-路线)

---

## 1. 背景与现状

HotPlex 是自托管的 Agent 网关，当前对接 Slack / Feishu / WebChat 三类前端。其中 WebChat 是 go:embed 嵌入的 Next.js SPA，通过 WebSocket 与 gateway 通信。

**当前隔离与身份现状（已核对源码）**：

| 维度 | 现状 | 关键位置 |
|---|---|---|
| WebChat 用户身份 | 所有访问者共享固定身份 `"webchat_user"`（嵌入模式）或 `"anonymous"`（dev 模式），无登录系统 | `internal/webchat/server.go:76,80`（含 `TODO(security): support real user identity via login/OAuth`） |
| 认证链 | 三优先级：`X-API-Key` header → `api_key` query → HMAC Cookie（webchat 同源 fallback） | `internal/security/auth.go:63-100` |
| user_id 语义 | 裸字符串，无用户实体。来源：cookie 固定值 / dev `anonymous` / API key 解析（`api_key_users.user_id`，1:1，migration 016） | `internal/security/auth.go:178`, `internal/security/apikey_resolver.go` |
| 会话隔离 | UUIDv5 session key 含 `owner + worker_type + client_key + work_dir`（webchat 路径），但 webchat 全是同一 user_id，隔离形同虚设 | `internal/session/key.go:25-30` |
| 配额 | PoolManager 两层：全局并发（20）+ per-user 并发（5）+ per-user 内存。仅 user 维度 | `internal/session/pool.go:46-51` |
| agent-configs | 三级 fallback：全局 → 平台 → bot。**完全无 per-user 层** | `internal/agentconfig/loader.go:177-216` |
| worker 选择 | 用户完全不能选，由配置 fallback（bot → platform → messaging → 编译默认）决定 | `cmd/hotplex/messaging_init.go:152-157` |
| workspace 概念 | **全仓不存在**。最接近的是 `work_dir`（文件系统路径，参与 session key 派生） | grep 全仓无 workspace 隔离实体 |

**核心结论**：WebChat 当前**没有用户/租户隔离的雏形**。引入真实多租户需要从身份地基开始构建。

---

## 2. 目标与非目标

### 2.1 目标（本 spec 范围）

1. **真实用户身份**：WebChat 的 user_id 从固定 `"webchat_user"` 升级为真实账号实体，提供登录/认证。
2. **workspace 数据模型**：引入 workspace 一等实体（per-user 的命名项目目录），承载未来的配置/配额/会话聚合。
3. **会话隔离打通**：WebChat 会话绑定到 workspace，实现 per-user per-workspace 隔离，用户间互不可见。
4. **多租户配额框架**：PoolManager 扩展 workspace 维度。
5. **认证扩展点**：抽象 `IdentityProvider` 接口，为后续 OAuth/SSO 叠加预留。

### 2.2 非目标（明确排除，留待后续 spec）

| 项 | 归属 spec |
|---|---|
| WebChat 前端登录/切换/选择 UI | spec ⑥（前端一等公民化） |
| per-user / per-workspace agent-configs 继承 | spec ③ |
| 用户级 worker 选择 UI | spec ④ |
| OAuth/SSO 具体 provider（飞书/Slack/OIDC）落地 | spec ① 之后的认证增强 |
| workspace 多成员协作（共享 workspace） | YAGNI，协作场景暂以"不同用户指向同一 work_dir"解决 |

### 2.3 多租户形态

**单组织·多终端用户**：一个 HotPlex 实例服务一组受信用户，每人独立身份与 workspace，互不可见。不引入"组织/租户"实体（那是 SaaS 形态）。

---

## 3. 关键决策汇总

| 决策点 | 选择 | 理由 |
|---|---|---|
| 多租户形态 | 单组织·多终端用户 | 自托管内部部署最典型，避免组织实体复杂度 |
| workspace 数据模型 | 一等实体，`work_dir` 必填属性，per-user 内 1:1 | 既符合"workspace = 项目目录"直觉，又能挂载配置/配额 |
| 身份来源 | 内建账号（bcrypt）+ `IdentityProvider` 接口（OAuth 后续） | 零外部依赖最务实，留扩展点 |
| 权限模型 | bootstrap admin + 邀请制 + workspace 私有 | 无公开注册最安全，协作靠共享 work_dir |
| session key 改造 | 方案3：`workspace_id` + `work_dir` 都进 key | 保留 work_dir 防 session ID 冲突，加 workspace_id 逻辑归属 |
| bootstrap admin 创建方式 | CLI `hotplex admin create` | 无前端依赖，纯后端可验证 |
| `work_dir` 可变性 | 创建后**不可变**（进 key 派生，改了断 resume） | 换目录 = 新建 workspace |
| 旧 webchat 共享会话 | GC 清理 | 无隔离产物，无保留价值 |

---

## 4. 架构总览

```
users (新) ──< workspaces (新) ──< sessions (改: +workspace_id)
   │                │
   └─< invitations (新)
api_key_users (现有) ──user_id──> users.id   (migration 018: 统一指向)
```

**三层抽象**：

1. **身份层**：`users` 实体 + `IdentityProvider` 接口。三条认证通道（账号登录 / Cookie / API Key）统一产出 `users.id`。
2. **workspace 层**：per-user 的项目目录实体，session 的归属载体。
3. **会话层**：`sessions` 加 `workspace_id` 列，隔离与配额经此列落地。

**关键设计原则**：
- **零破坏**：现有平台会话（Slack/Feishu）和 cron 会话的 `workspace_id` 为 NULL，行为不变。
- **复用现有机制**：work_dir 安全校验复用 `security.ValidateWorkDir`；所有权校验复用 `ValidateOwnership` 思路；背压复用现有策略。
- **接口扩展而非替换**：`IdentityProvider` 是加法，现有 `Authenticator`/API key 机制保留。

---

## 5. 数据模型

### 5.1 新表 `users`（账号实体）

```sql
CREATE TABLE users (
    id            TEXT PRIMARY KEY,            -- UUID
    username      TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,               -- bcrypt
    role          TEXT NOT NULL DEFAULT 'user', -- 'admin' | 'user'
    display_name  TEXT NOT NULL DEFAULT '',
    status        TEXT NOT NULL DEFAULT 'active', -- 'active' | 'disabled'
    created_at    INTEGER NOT NULL,
    updated_at    INTEGER NOT NULL,
    last_login_at INTEGER
);
CREATE INDEX idx_users_status ON users(status);
```

- `id`：UUID，权威用户标识，下游所有 `user_id` 指向它。
- `password_hash`：bcrypt（cost 项目统一值）。
- `role`：`admin` 可管理用户/邀请/全局配额；`user` 只能管自己的 workspace。
- `status='disabled'` 的用户登录被拒，已有 cookie 失效。

### 5.2 新表 `workspaces`（per-user 项目目录实体）

```sql
CREATE TABLE workspaces (
    id                     TEXT PRIMARY KEY,   -- UUID
    owner_user_id          TEXT NOT NULL REFERENCES users(id),
    name                   TEXT NOT NULL,
    work_dir               TEXT NOT NULL,
    agent_config_overrides TEXT,               -- JSON, spec ③ 填充，spec ① 留 NULL
    worker_preference      TEXT,               -- 'claude_code'|'opencode_server'|'codex_cli'|'acp'|NULL, spec ④ 填充
    status                 TEXT NOT NULL DEFAULT 'active',
    created_at             INTEGER NOT NULL,
    updated_at             INTEGER NOT NULL,
    UNIQUE(owner_user_id, work_dir)            -- per-user 内 1:1；不同用户可指向同一 work_dir 协作
);
CREATE INDEX idx_workspaces_owner ON workspaces(owner_user_id);
```

- `work_dir` 创建后**不可变**（应用层强制，PATCH 不接受该字段）。
- `agent_config_overrides` / `worker_preference`：本 spec 建表留空，为 spec ③/④ 预留。

### 5.3 新表 `invitations`（admin 邀请制）

```sql
CREATE TABLE invitations (
    id          TEXT PRIMARY KEY,              -- UUID
    code        TEXT NOT NULL UNIQUE,          -- 一次性邀请码
    created_by  TEXT NOT NULL REFERENCES users(id),
    role        TEXT NOT NULL DEFAULT 'user',  -- 被邀请人角色
    used_by     TEXT REFERENCES users(id),     -- NULL = 未使用
    expires_at  INTEGER NOT NULL,
    created_at  INTEGER NOT NULL,
    used_at     INTEGER
);
CREATE INDEX idx_invitations_code ON invitations(code);
```

### 5.4 改表 `sessions`（加 `workspace_id` 列）

```sql
ALTER TABLE sessions ADD COLUMN workspace_id TEXT REFERENCES workspaces(id);
CREATE INDEX idx_sessions_workspace ON sessions(workspace_id);
```

- **nullable**：webchat 会话必填；Slack/Feishu/cron 现有会话保持 NULL（向后兼容）。
- `workspace_id` 参与会话隔离查询与配额计算，**不替代** session key（key 派生见 §6）。

### 5.5 现有表 `api_key_users`（迁移，见 §12）

`user_id` 字段语义从"任意字符串"改为指向 `users.id`。

---

## 6. session key 派生改造

### 6.1 方案3：workspace_id + work_dir 都进 key

webchat 路径的 session key 派生改造：

```go
// 改造前 (key.go:25)
func DeriveSessionKey(ownerID string, wt WorkerType, clientKey, workDir string) string {
    name := ownerID + "|" + string(wt) + "|" + clientKey + "|" + workDir
    ...
}

// 改造后
func DeriveSessionKey(ownerID string, wt WorkerType, clientKey, workspaceID, workDir string) string {
    name := ownerID + "|" + string(wt) + "|" + clientKey + "|" + workspaceID + "|" + workDir
    ...
}
```

- 新增 `workspaceID` 参数：**空串不参与 hash**（向后兼容非 webchat 调用者，如未来可能的内部调用）。
- webchat 场景传 `(owner, wt, clientKey, workspace.id, workspace.work_dir)`。
- `work_dir` 仍进 key：保留现有"防止 Session ID already in use"的冲突防护语义（`key.go:62-63` 注释）。
- `workspace_id` 进 key：提供逻辑归属层，确保 session 的 key 派生与 `sessions.workspace_id` 列一致。

### 6.2 调用点改造

- `internal/gateway/api.go` `CreateSession`（行 151-235）：入参从裸 `work_dir` 改为 `workspace_id`，校验归属后从 workspace 取 `work_dir` 与 `workspace.id`。
- `internal/session/key.go` `DeriveSessionKey` 签名变更（加 `workspaceID` 参数）。
- Slack/Feishu 的 `DerivePlatformSessionKey`（行 73-123）**不变**（平台会话无 workspace 概念，走原路径）。
- `DeriveCronSessionKey`（行 128-131）**不变**。

### 6.3 resume 语义

`workspace_id`（UUID）永不变；`work_dir` 不可变（§5.2）。故方案3 下 webchat 会话 resume 的 key 输入完全稳定，resume 语义与现有一致。

---

## 7. 认证流程与 IdentityProvider

### 7.1 IdentityProvider 接口

```go
// internal/security/identity_provider.go (新)
type User struct {
    ID           string
    Username     string
    Role         string
    Status       string
    DisplayName  string
}

type Credentials interface{ Kind() string }  // LoginCredentials / OAuthCredentials / ...

type IdentityProvider interface {
    // Authenticate 校验凭证，返回 user ID。
    Authenticate(ctx context.Context, creds Credentials) (userID string, err error)
    // Lookup 按 ID 查用户。
    Lookup(ctx context.Context, userID string) (*User, error)
}
```

第一个实现 `LocalAccountProvider`（users 表 + bcrypt）。OAuth provider 后续作为第二实现，不动调用方。

### 7.2 三条认证通道，统一产出 `users.id`

| 通道 | 入口 | 机制 | 产出 |
|---|---|---|---|
| **账号登录**（新） | `POST /api/auth/login` `{username, password}` | `LocalAccountProvider.Authenticate` 校验 bcrypt → 签发 HMAC cookie | `users.id` |
| **Cookie**（升级） | 后续请求 | 现有 `CookieAuth` 从固定 `"webchat_user"` **升级为解码真实 `users.id`** + TTL + 刷新 | `users.id` |
| **API Key**（保留） | `X-API-Key` header / `api_key` query | 现有 keyResolver 链不变，`api_key_users.user_id` 迁移指向 `users.id` | `users.id` |

`Authenticator.AuthenticateRequest` 返回的 user_id 升级为 `users.id`，下游 `Claims{UserID, APIKey}`（`auth.go:253-256`）全程指向真实用户。

### 7.3 Cookie 升级细节

现有 `CookieAuth`（HMAC cookie，固定 `"webchat_user"`）升级：
- 登录成功后 cookie 编码 `users.id` + 签发时间 + HMAC。
- Cookie TTL（建议 7 天）+ 滑动刷新（过半 TTL 自动续签）。
- 登出清除 cookie。

### 7.6 过渡状态与验收方式（重要范围边界）

本 spec 改造 `internal/webchat/server.go:76,80` 后，**移除**"同源自动签发固定 `webchat_user` cookie"的行为。由此产生的过渡状态：

- **生产模式**（已配置认证）：webchat 同源访问不再自动登录，未登录请求返回 `401`，需先经 `/api/auth/login`。**但 webchat 登录 UI 属于 ⑥spec**，故本 spec 期间 webchat 前端在生产模式下不可用（断开），需 ⑥spec 接入登录页后恢复。
- **dev 模式**（无任何认证配置，`auth.go:82-86`）：保留现有 `anonymous` 兜底，webchat dev 体验不中断。

**本 spec 验收方式 = HTTP API 测试**（curl + Go 测试覆盖 §13 所有场景），**不依赖 webchat UI**。webchat 生产 UI 恢复是 ⑥spec 的交付物。这是有意为之的范围切分：后端地基可独立验证，前端集成单独 spec 更聚焦。

### 7.4 bootstrap admin（CLI，无前端依赖）

首次部署 `users` 表为空时：

```bash
hotplex admin create --username alice --password '...' --admin
```

- 仅当 `users` 表为空或调用者是 admin 时允许。
- 创建后该用户即首个 admin，可登录并管理邀请。
- 密码从 flag 读取（不回显），或交互式提示输入。

### 7.5 邀请制注册

1. admin `POST /api/admin/invitations` `{role, ttl?}` → 生成一次性 `code`（密码学随机）+ `expires_at`。
2. 用户 `POST /api/auth/accept-invite` `{code, username, password}`：
   - 校验 code 存在、未使用（`used_by IS NULL`）、未过期。
   - 创建 `users` 行（`role` 取自 invitation，`password_hash` = bcrypt）。
   - 标记 invitation `used_by` + `used_at`。
   - 签发登录 cookie。

---

## 8. workspace 生命周期与会话隔离

### 8.1 workspace CRUD（`/api/workspaces`）

- **创建** `POST {name, work_dir}`：
  - `config.ExpandAndAbs(work_dir)` + `security.ValidateWorkDir(work_dir)` 双校验（防路径穿越，与 SwitchWorkDir 同标准，见 `.agents/rules/security.md`）。
  - `UNIQUE(owner_user_id, work_dir)` 约束保证 per-user 1:1。
- **列出** `GET`：仅返回 `owner_user_id = 当前用户` 的 workspace（私有）。
- **更新** `PATCH {name?, agent_config_overrides?, worker_preference?}`：
  - 可改 `name` 及预留字段。**`work_dir` 不可变**（应用层拒绝，返回错误）。
- **删除** `DELETE`：
  - 校验无活跃会话；有则连带 TERMINATE（复用现有 `Transition(TERMINATED)`）。
  - 软删除（`status='deleted'`）或硬删除？建议**硬删除 + 校验无活跃会话**，避免 work_dir 解锁后被他人复用造成历史混淆。

### 8.2 CreateSession 改造（核心打通点）

```
现状:  POST /api/sessions?client_session_id=X&worker_type=claude_code
改造:  POST /api/sessions
       body: {workspace_id, client_session_id, worker_type?}
       1. 校验 workspace 归属：workspace.owner_user_id == 当前 user.id，否则 WORKSPACE_FORBIDDEN
       2. work_dir := workspace.work_dir
       3. worker_type := body.worker_type ?? workspace.worker_preference ?? 配置 fallback
       4. DeriveSessionKey(owner, wt, clientKey, workspace.id, work_dir)   // §6 方案3
       5. sessions 写入 workspace_id 列
```

`worker_type` 第一版仍主要走配置 fallback；`workspace.worker_preference` 占位（spec ④ 填充）。

### 8.3 会话隔离查询

- `GET /api/sessions`：按 `user_id` + 可选 `workspace_id` 过滤。webchat 用户只见自己 workspace 下的会话。
- `GET/DELETE /api/sessions/{id}`、`/history`、`/events`、`/cd`：经 `session.workspace_id → workspace.owner_user_id == 当前 user` 二次校验（复用现有 `ValidateOwnership` 思路 `manager.go:913-938`，扩展到 workspace 归属）。

### 8.4 SwitchWorkDir 语义重映射

现有 `POST /api/sessions/{id}/cd`（切工作目录，`bridge.go handleSwitchWorkDir`）在 workspace 模型下：

- 切换 `work_dir` = 切换到该 `work_dir` 对应的 workspace。
- 因 per-user 1:1，`work_dir → workspace` 确定性映射：查找（无则自动创建）`owner + workDir` 的 workspace → 在其下 resume/创建会话。
- 前端体验不变（仍是"切目录"），后端落到 workspace 实体。
- 安全校验不变（`ExpandAndAbs` + `ValidateWorkDir`）。

---

## 9. 配额扩展

### 9.1 PoolManager 三层

现有两层（全局 + per-user，`pool.go:46-51`）→ 新增 **per-workspace 并发**层：

```
Acquire 顺序（单 mutex 原子检查，沿用 pool.go 模式）:
  全局 maxSize  →  per-user 并发  →  per-workspace 并发（新）
```

- per-workspace 配额数据结构：`workspaceCount map[string]int`（key = `workspace_id`）。
- 内存维度第一版**不**细分到 workspace（YAGNI），仅并发维度。
- 配额满仍走现有"丢弃 message.delta，保留 state/done/error"背压策略（见 CLAUDE.md 背压规则）。
- 平台会话（`workspace_id IS NULL`）不参与 per-workspace 层，仅走全局 + per-user。

### 9.2 配额配置

per-workspace 并发上限：复用 `config.SecurityConfig` 或新增 `config.QuotaConfig`，默认值（建议 3）。可热重载。

---

## 10. API 端点清单

### 10.1 认证

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/auth/login` | `{username, password}` → 签发 cookie |
| POST | `/api/auth/logout` | 清除 cookie |
| GET | `/api/auth/me` | 返回当前用户信息 |
| POST | `/api/auth/accept-invite` | `{code, username, password}` → 创建账号 + 签发 cookie |

### 10.2 admin（需 admin 角色）

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/admin/invitations` | 创建邀请 `{role, ttl?}` |
| GET | `/api/admin/invitations` | 列出邀请 |
| DELETE | `/api/admin/invitations/{id}` | 撤销邀请 |
| GET | `/api/admin/users` | 列出用户 |
| PATCH | `/api/admin/users/{id}` | 启用/禁用用户 |

### 10.3 workspace

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/workspaces` | 列出自己的 workspace |
| POST | `/api/workspaces` | 创建 `{name, work_dir}` |
| GET | `/api/workspaces/{id}` | 详情 |
| PATCH | `/api/workspaces/{id}` | 更新（work_dir 不可变） |
| DELETE | `/api/workspaces/{id}` | 删除（校验无活跃会话） |
| GET | `/api/workspaces/{id}/sessions` | 列出该 workspace 下的会话 |

### 10.4 session（改造现有）

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/sessions` | body 改为 `{workspace_id, client_session_id, worker_type?}` |
| GET | `/api/sessions` | 加 `?workspace_id=` 过滤 |
| GET/DELETE | `/api/sessions/{id}` 等 | 加 workspace 归属校验（现有端点保留） |

---

## 11. 错误码

沿用项目 `AppError{Code, ...}` 模式（`.agents/rules/golang.md`）。新增码：

| Code | HTTP | 场景 |
|---|---|---|
| `INVALID_CREDENTIALS` | 401 | 登录失败 |
| `INVITATION_EXPIRED` | 400 | 邀请码过期 |
| `INVITATION_USED` | 400 | 邀请码已使用 |
| `INVITATION_NOT_FOUND` | 404 | 邀请码不存在 |
| `WORKSPACE_NOT_FOUND` | 404 | workspace 不存在 |
| `WORKSPACE_FORBIDDEN` | 403 | workspace 归属不匹配 |
| `WORK_DIR_TAKEN` | 409 | per-user work_dir 1:1 冲突 |
| `WORK_DIR_IMMUTABLE` | 400 | 尝试修改 work_dir |
| `WORKSPACE_NOT_EMPTY` | 409 | 删除时有活跃会话 |
| `USER_DISABLED` | 403 | 用户已禁用 |
| `USERNAME_TAKEN` | 409 | 用户名已存在 |

配额错误复用现有 `QUOTA_EXCEEDED`（扩展 message 含 workspace 维度信息）。

---

## 12. 迁移策略

**核心原则：零破坏**。所有迁移支持 SQLite 与 PostgreSQL 双方言（`dbutil` + `sqlutil`）。

### 12.1 migration 017：建表 + sessions 加列

```sql
-- 建 users / workspaces / invitations 三表（见 §5.1-5.3）
-- sessions 加 workspace_id 列（nullable）+ 索引（见 §5.4）
```

- 三新表独立，无数据依赖。
- `sessions.workspace_id` nullable，现有会话自动得到 NULL。

### 12.2 migration 018：api_key_users 统一指向

```sql
-- 为每个 api_key_users.user_id（字符串）provision 一个 users 行：
--   users.id = 新 UUID, username = user_id 或派生, role='user', password_hash=''（API key 用户无密码）
--   status='active'
-- api_key_users.user_id 改为指向 users.id
```

- provision 用确定性映射（如 `username = "apikey:" + 原 user_id`）避免冲突。
- API key 用户 `password_hash` 为空，禁止账号登录通道（仅 API key 通道有效）——应用层校验 `password_hash == ''` 拒绝登录。

### 12.3 旧 webchat 共享会话清理

- `user_id='webchat_user'` 或 `'anonymous'` 的会话由 GC 清理（无 workspace 归属，无保留价值）。
- 清理方式：在 migration 后首次 GC 周期，或显式 `DELETE FROM sessions WHERE user_id IN ('webchat_user','anonymous')`。
- 这些会话的 session key 不含 workspace_id，与新体系不兼容。

### 12.4 平台会话/cron 不受影响

- `workspace_id` 为 NULL。
- key 派生走原路径（`DerivePlatformSessionKey` / `DeriveCronSessionKey` 不变）。
- 零行为变化。

---

## 13. 测试策略

遵循项目规范（`CLAUDE.md` + `.agents/rules/golang.md`）：

- **风格**：table-driven + `testify/require` + `t.Parallel()`，单模块 ≤5s（`-count=1 -race`）。
- **禁止** `time.Sleep` 等待异步，用 `require.Eventually` 或 channel 信号。

### 13.1 重点覆盖

| 模块 | 测试要点 |
|---|---|
| users/workspaces/invitations store | CRUD + UNIQUE 约束 + 软状态 |
| 认证三通道 | 账号登录 bcrypt / Cookie 签发与校验 / API key 解析 → users.id |
| IdentityProvider | LocalAccountProvider 校验，接口可替换性 |
| **跨用户/跨 workspace 互不可见** | 隔离核心：A 用户看不到 B 的 workspace/session |
| 配额三层 | 全局/per-user/per-workspace 各自触发拒绝 |
| 邀请流程 | 过期/已用/不存在/成功创建 |
| session key 改造 | 方案3 派生正确性 + 空 workspaceID 向后兼容 |
| SwitchWorkDir 重映射 | work_dir → workspace 确定性映射 + 自动创建 |
| 迁移 | 017/018 幂等 + api_key provision 正确性 |

### 13.2 隔离测试（最高优先级）

构造两个用户各自两个 workspace，断言：
- ListSessions 只返回当前用户当前 workspace 的会话。
- 跨用户/跨 workspace 访问 session 返回 `WORKSPACE_FORBIDDEN`/404。
- 配额在 workspace 维度独立计数。

---

## 14. 后续 spec 路线

本 spec（spec ①）落地后，按依赖顺序推进：

```
① 身份 + workspace + 隔离（本 spec）  ← 后端地基
        │
        ▼
② per-user/per-workspace 配置继承（agent-configs fallback 链改造）
        │
        ├──────────────┐
        ▼              ▼
③ 用户级 worker 选择   ④ OAuth/SSO provider 落地
        │              │
        └──────┬───────┘
               ▼
⑤ 多租户配额增强（内存维度、计费等）
               │
               ▼
⑥ webchat 前端一等公民化（登录/workspace 切换/worker 选择/配置编辑 UI）
```

每个后续 spec 各自独立的 design → plan → implementation 周期。

---

## 附录 A：受影响文件清单（预估）

| 文件 | 改动类型 |
|---|---|
| `internal/session/sql/migrations/017_*.sql` | 新增（建表） |
| `internal/session/sql/migrations/018_*.sql` | 新增（api_key provision） |
| `internal/session/store.go` | 扩展（users/workspaces/invitations store） |
| `internal/session/manager.go` | 扩展（workspace 归属校验、配额） |
| `internal/session/pool.go` | 扩展（per-workspace 配额层） |
| `internal/session/key.go` | 改签名（`DeriveSessionKey` + workspaceID） |
| `internal/security/identity_provider.go` | 新增 |
| `internal/security/local_account_provider.go` | 新增 |
| `internal/security/auth.go` | 升级（AuthenticateRequest → users.id） |
| `internal/security/cookie_auth.go` | 升级（编码真实 user.id + TTL） |
| `internal/gateway/api.go` | 改造（CreateSession workspace_id 入参） |
| `internal/gateway/auth_handlers.go`（新） | 登录/登出/me/accept-invite/admin 端点 |
| `internal/gateway/workspace_handlers.go`（新） | workspace CRUD |
| `cmd/hotplex/admin_cmd.go`（新） | `hotplex admin create` CLI |
| `cmd/hotplex/routes.go` | 注册新路由 |
| `internal/config/config_types.go` | 新增 `QuotaConfig`（per-workspace 上限） |
| `internal/webchat/server.go` | cookie 签发改为登录态（移除固定 webchat_user） |

（精确文件与行号在实现计划 phase 细化。）

---

## 附录 B：实现阶段固化的参数默认值

以下参数均有合理默认，不阻塞设计推进，在实现计划中固化（非 Open Question，已收敛）：

1. **密码 bcrypt cost**：沿用项目现有值（若存在）或统一 12。
2. **Cookie TTL 默认值**：7 天，滑动刷新阈值 3.5 天。
3. **per-workspace 并发默认上限**：3（可配置）。
4. **API key 用户 provision 的 username 命名**：`apikey:<原user_id>` 避免与真人 username 冲突。

（workspace 删除策略已在 §8.1 定为硬删除 + 校验无活跃会话，不再列入。）
