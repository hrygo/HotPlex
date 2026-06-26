# WebChat 一等公民化与多租户地基设计（spec ①：身份 + workspace + 隔离）

**日期**: 2026-06-15
**状态**: Draft（待实现计划）
**分支**: main · **基线版本**: v1.29.0 (fb857af1)
**范围**: 后端地基 —— 用户身份系统、workspace 数据模型、会话隔离打通、多租户配额框架
**作者**: brainstorming session → 设计文档

---

## 目录

- [1. 背景与现状](#1-背景与现状)
- [2. 双轨定位与关系模型（设计根基）](#2-双轨定位与关系模型设计根基)
- [3. 目标与非目标](#3-目标与非目标)
- [4. 关键决策汇总](#4-关键决策汇总)
- [5. 架构总览](#5-架构总览)
- [6. 数据模型](#6-数据模型)
- [7. session key 派生改造](#7-session-key-派生改造)
- [8. 认证流程与 IdentityProvider](#8-认证流程与-identityprovider)
- [9. workspace 生命周期与会话隔离](#9-workspace-生命周期与会话隔离)
- [10. 配额扩展](#10-配额扩展)
- [11. API 端点清单](#11-api-端点清单)
- [12. 错误码](#12-错误码)
- [13. 迁移策略](#13-迁移策略)
- [14. 测试策略](#14-测试策略)
- [15. 后续 spec 路线](#15-后续-spec-路线)

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

## 2. 双轨定位与关系模型（设计根基）

### 2.1 双定位

HotPlex 同时服务两类前端，部署形态与隔离需求根本不同：

| | **WebSocket Gateway + WebChat** | **Message Channel（Slack/Feishu）** |
|---|---|---|
| 部署形态 | 企业内**团队共享**一个 HotPlex 实例 | **个人独享** HotPlex 实例 |
| 用户数 | 多用户 | 单用户（实例即一人） |
| 隔离需求 | **per-user per-workspace 强隔离** | 不需要（就一人） |
| 用户身份 | HotPlex `User` 实体（登录账号） | 平台 user id（Slack/Feishu，非实体） |

**核心决策：双轨完全隔离。** `User`/`Workspace`/per-user 配置这些多租户实体**只服务 WebChat 轨**；Message Channel 轨保持现状（全局/Bot 级配置 + 平台 user id 仅入 session key 区分会话）。多租户能力是 WebChat 专属，不污染 Message Channel。

### 2.2 实体归属（双轨）

| 实体 | WebChat 轨（团队共享） | Message Channel 轨（个人独享） |
|---|---|---|
| **User**（HotPlex 登录账号） | ✅ 核心 | ❌ 无（平台 user id 非实体） |
| **Workspace** | ✅ 核心 | ❌ 无 |
| **WorkDir** | 经由 Workspace（必填属性） | ✅ 直接配置 |
| **AgentConfigs** | 两层：团队默认 → Workspace 自定义（spec ②） | 现状三级：全局 → 平台 → Bot |
| **WorkerType** | 两层：团队默认 → Workspace 选择（spec ③） | 现状：Bot → 平台 → messaging → 默认 |
| **Worker 凭证** | ❌ **不在 HotPlex 管辖**，worker 二进制自管 | ❌ 同左 |
| **Bot** | ❌ 不引入选择（见 §2.4） | ✅ 核心（路由/身份） |
| **Session** | 带 `workspace_id` | 带 platform fields，无 `workspace_id` |
| **Invitation** | ✅ 邀请制 | ❌ 无 |

### 2.3 WebChat 轨关系图（以 Workspace 为中心）

```
Invitation ──creates──▶ User ──owns 1:N──▶ Workspace ──contains 1:N──▶ Session
                                      │
                                      ├── work_dir        (必填属性, per-user 内 1:1)
                                      ├── agent-configs   (自定义, 覆盖团队默认, spec ②)
                                      └── worker type     (选择, 覆盖团队默认, spec ③)

Session:
  · workspace_id (非空, 归属校验)
  · key = hash(owner + wt + clientKey + workspace_id + work_dir)   # 方案3, §7
  · 不关联 Bot

启动 Worker:
  · work_dir 从 Workspace 取
  · 凭证: worker 自管 (claude 读 ~/.claude/env, codex 读自有配置), HotPlex 不经手
```

### 2.4 配置继承：两层，极简

WebChat 轨的 agent-configs 与 worker type 继承只有两层：

```
团队默认 (admin 配全局 agent-configs + 默认 worker type)
     │
     ▼  被覆盖
Workspace 自定义 (用户在 workspace 内定 agent-configs + 选 worker type)
     │
     ▼  继承
Session (自动继承所属 Workspace 的全部配置)
```

**Bot 在 WebChat 轨无实质作用**：凭证 worker 自管、agent-configs 在 Workspace 定制、worker type 在 Workspace 选——WebChat 会话不选 Bot。Message Channel 轨的 Bot 概念（路由/平台身份）不在本 spec 范围，保持现状。

### 2.5 Worker 凭证边界（重要）

**凭证不在 HotPlex 管辖范围。** HotPlex 的职责是启动 worker 进程（fork+exec，传 work_dir / system prompt / 会话参数），凭证由服务器上安装的 worker 二进制独立管理：

| Worker type | 凭证（worker 自管） |
|---|---|
| `claude_code` | Anthropic API key / `~/.claude` 登录态 |
| `codex_cli` | OpenAI API key |
| `opencode_server` | 对应 provider 凭证 |
| `acp` | 目标 Agent 凭证 |

用户在 Workspace 选 worker type，系统启动对应 worker 二进制，凭证从 worker 运行环境透明读取。HotPlex 不存储、不注入、不代理凭证。

---

## 3. 目标与非目标

### 3.1 目标（本 spec 范围）

1. **真实用户身份**：WebChat 的 user_id 从固定 `"webchat_user"` 升级为真实账号实体，提供登录/认证。
2. **workspace 数据模型**：引入 workspace 一等实体（per-user 的命名项目目录），承载未来的配置/配额/会话聚合。
3. **会话隔离打通**：WebChat 会话绑定到 workspace，实现 per-user per-workspace 隔离，用户间互不可见。
4. **多租户配额框架**：PoolManager 扩展 workspace 维度。
5. **认证扩展点**：抽象 `IdentityProvider` 接口，为后续 OAuth/SSO 叠加预留。

### 3.2 非目标（明确排除，留待后续 spec）

| 项 | 归属 spec |
|---|---|
| WebChat 前端登录/切换/选择 UI | spec ⑥（前端一等公民化） |
| per-Workspace agent-configs 自定义（团队默认 → Workspace 两层继承） | spec ② |
| 用户级 worker type 选择（Workspace 内选 worker） | spec ③ |
| OAuth/SSO 具体 provider（飞书/Slack/OIDC）落地 | spec ④ |
| workspace 多成员协作（共享 workspace） | YAGNI，协作场景暂以"不同用户指向同一 work_dir"解决 |
| Message Channel 引入 User/Workspace/per-user | 永不（双轨完全隔离，见 §2） |
| Worker 凭证管理（存储/注入/代理 API key） | 永不（凭证 worker 自管，见 §2.5） |

### 3.3 多租户形态

**单组织·多终端用户**：一个 HotPlex 实例服务一组受信用户，每人独立身份与 workspace，互不可见。不引入"组织/租户"实体（那是 SaaS 形态）。

---

## 4. 关键决策汇总

| 决策点 | 选择 | 理由 |
|---|---|---|
| 双轨定位 | WebChat 团队共享多租户 / Message Channel 个人独享，**双轨完全隔离** | 部署形态根本不同，多租户是 WebChat 专属（§2） |
| 配置继承层级 | 两层：团队默认 → Workspace 自定义 | 极简，以 Workspace 为中心，不绕 Bot/独立 per-user 层（§2.4） |
| WebChat 是否选 Bot | 不选，Bot 在 WebChat 轨无实质作用 | 凭证 worker 自管、配置/worker 都在 Workspace 定制（§2.4） |
| Worker 凭证归属 | **不在 HotPlex 管辖**，worker 二进制自管 | HotPlex 只启动进程，不碰 API key（§2.5） |
| 多租户形态 | 单组织·多终端用户 | 自托管内部部署最典型，避免组织实体复杂度 |
| workspace 数据模型 | 一等实体，`work_dir` 必填属性，per-user 内 1:1 | 既符合"workspace = 项目目录"直觉，又能挂载配置/配额 |
| 身份来源 | 内建账号（bcrypt）+ `IdentityProvider` 接口（OAuth 后续） | 零外部依赖最务实，留扩展点 |
| 权限模型 | bootstrap admin + 邀请制 + workspace 私有 | 无公开注册最安全，协作靠共享 work_dir |
| session key 改造 | 方案3：`workspace_id` + `work_dir` 都进 key | 保留 work_dir 防 session ID 冲突，加 workspace_id 逻辑归属 |
| bootstrap admin 创建方式 | CLI `hotplex admin create` | 无前端依赖，纯后端可验证 |
| `work_dir` 可变性 | workspace 级**可改**（落 owner 沙箱 + 无活跃会话守卫，见 WebChat-Workspace-Create-WorkDir-Prefix-Spec §5.1.4）；session 级 workspace-bound 不可改 | 改 work_dir 不再要求新建 workspace |
| 旧 webchat 共享会话 | GC 清理 | 无隔离产物，无保留价值 |

---

## 5. 架构总览

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

## 6. 数据模型

### 6.1 新表 `users`（账号实体）

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

### 6.2 新表 `workspaces`（per-user 项目目录实体）

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

- `work_dir` workspace 级可改：PATCH 携带时校验 owner 沙箱（`403 WORK_DIR_OUTSIDE_SANDBOX`，按 `OwnerUserID`）+ 活跃会话守卫（`409 WORKSPACE_NOT_EMPTY`，因 work_dir 进 session key 派生）；改 `name` / `agent_config_overrides` / `worker_preference` 不触发。session 级 work_dir 仍从 workspace 继承，workspace-bound session 不可自行 `/cd`（`400 WORK_DIR_IMMUTABLE`）。详见 WebChat-Workspace-Create-WorkDir-Prefix-Spec §5.1.4。
- `agent_config_overrides` / `worker_preference`：本 spec 建表留空，为 spec ③/④ 预留。

### 6.3 新表 `invitations`（admin 邀请制）

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

### 6.4 改表 `sessions`（加 `workspace_id` 列）

```sql
ALTER TABLE sessions ADD COLUMN workspace_id TEXT REFERENCES workspaces(id);
CREATE INDEX idx_sessions_workspace ON sessions(workspace_id);
```

- **nullable**：webchat 会话必填；Slack/Feishu/cron 现有会话保持 NULL（向后兼容）。
- `workspace_id` 参与会话隔离查询与配额计算，**不替代** session key（key 派生见 §7）。

### 6.5 现有表 `api_key_users`（迁移，见 §13）

`user_id` 字段语义从"任意字符串"改为指向 `users.id`。

---

## 7. session key 派生改造

### 7.1 方案3：workspace_id + work_dir 都进 key

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

### 7.2 调用点改造

- `internal/gateway/api.go` `CreateSession`（行 151-235）：入参从裸 `work_dir` 改为 `workspace_id`，校验归属后从 workspace 取 `work_dir` 与 `workspace.id`。
- `internal/session/key.go` `DeriveSessionKey` 签名变更（加 `workspaceID` 参数）。
- Slack/Feishu 的 `DerivePlatformSessionKey`（行 73-123）**不变**（平台会话无 workspace 概念，走原路径）。
- `DeriveCronSessionKey`（行 128-131）**不变**。

### 7.3 resume 语义

`workspace_id`（UUID）永不变；`work_dir` 在 workspace 无活跃会话时可改（§6.2 + WebChat-Workspace-Create-WorkDir-Prefix-Spec §5.1.4）。活跃会话守卫确保变更瞬间无 session 受 key 漂移影响，故 webchat 会话在其生命周期内 resume 的 key 输入稳定，resume 语义保持。

---

## 8. 认证流程与 IdentityProvider

### 8.1 IdentityProvider 接口

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

### 8.2 三条认证通道，统一产出 `users.id`

| 通道 | 入口 | 机制 | 产出 |
|---|---|---|---|
| **账号登录**（新） | `POST /api/auth/login` `{username, password}` | `LocalAccountProvider.Authenticate` 校验 bcrypt → 签发 HMAC cookie | `users.id` |
| **Cookie**（升级） | 后续请求 | 现有 `CookieAuth` 从固定 `"webchat_user"` **升级为解码真实 `users.id`** + TTL + 刷新 | `users.id` |
| **API Key**（保留） | `X-API-Key` header / `api_key` query | 现有 keyResolver 链不变，`api_key_users.user_id` 迁移指向 `users.id` | `users.id` |

`Authenticator.AuthenticateRequest` 返回的 user_id 升级为 `users.id`，下游 `Claims{UserID, APIKey}`（`auth.go:253-256`）全程指向真实用户。

### 8.3 Cookie 升级细节

现有 `CookieAuth`（HMAC cookie，固定 `"webchat_user"`）升级：
- 登录成功后 cookie 编码 `users.id` + 签发时间 + HMAC。
- Cookie TTL（建议 7 天）+ 滑动刷新（过半 TTL 自动续签）。
- 登出清除 cookie。

### 8.4 过渡状态与验收方式（重要范围边界）

本 spec 改造 `internal/webchat/server.go:76,80` 后，**移除**"同源自动签发固定 `webchat_user` cookie"的行为。由此产生的过渡状态：

- **生产模式**（已配置认证）：webchat 同源访问不再自动登录，未登录请求返回 `401`，需先经 `/api/auth/login`。**但 webchat 登录 UI 属于 ⑥spec**，故本 spec 期间 webchat 前端在生产模式下不可用（断开），需 ⑥spec 接入登录页后恢复。
- **dev 模式**（无任何认证配置，`auth.go:82-86`）：保留现有 `anonymous` 兜底，webchat dev 体验不中断。

**本 spec 验收方式 = HTTP API 测试**（curl + Go 测试覆盖 §14 所有场景），**不依赖 webchat UI**。webchat 生产 UI 恢复是 ⑥spec 的交付物。这是有意为之的范围切分：后端地基可独立验证，前端集成单独 spec 更聚焦。

### 8.5 bootstrap admin（CLI，无前端依赖）

首次部署 `users` 表为空时：

```bash
hotplex admin create --username alice --password '...' --admin
```

- 仅当 `users` 表为空或调用者是 admin 时允许。
- 创建后该用户即首个 admin，可登录并管理邀请。
- 密码从 flag 读取（不回显），或交互式提示输入。

### 8.6 邀请制注册

1. admin `POST /api/admin/invitations` `{role, ttl?}` → 生成一次性 `code`（密码学随机）+ `expires_at`。
2. 用户 `POST /api/auth/accept-invite` `{code, username, password}`：
   - 校验 code 存在、未使用（`used_by IS NULL`）、未过期。
   - 创建 `users` 行（`role` 取自 invitation，`password_hash` = bcrypt）。
   - 标记 invitation `used_by` + `used_at`。
   - 签发登录 cookie。

---

## 9. workspace 生命周期与会话隔离

### 9.1 workspace CRUD（`/api/workspaces`）

- **创建** `POST {name, work_dir}`：
  - `config.ExpandAndAbs(work_dir)` + `security.ValidateWorkDir(work_dir)` 双校验（防路径穿越，与 SwitchWorkDir 同标准，见 `.agents/rules/security.md`）。
  - `UNIQUE(owner_user_id, work_dir)` 约束保证 per-user 1:1。
- **列出** `GET`：仅返回 `owner_user_id = 当前用户` 的 workspace（私有）。
- **更新** `PATCH {name?, agent_config_overrides?, worker_preference?, work_dir?}`：
  - 可改 `name` / `agent_config_overrides` / `worker_preference` / `work_dir`（`work_dir` 须在 owner 沙箱下且 workspace 无活跃会话时方可改，详见 §6.2）。
- **删除** `DELETE`：
  - 校验无活跃会话；有则连带 TERMINATE（复用现有 `Transition(TERMINATED)`）。
  - 软删除（`status='deleted'`）或硬删除？建议**硬删除 + 校验无活跃会话**，避免 work_dir 解锁后被他人复用造成历史混淆。

### 9.2 CreateSession 改造（核心打通点）

```
现状:  POST /api/sessions?client_session_id=X&worker_type=claude_code
改造:  POST /api/sessions
       body: {workspace_id, client_session_id, worker_type?}
       1. 校验 workspace 归属：workspace.owner_user_id == 当前 user.id，否则 WORKSPACE_FORBIDDEN
       2. work_dir := workspace.work_dir
       3. worker_type := body.worker_type ?? workspace.worker_preference ?? 配置 fallback
       4. DeriveSessionKey(owner, wt, clientKey, workspace.id, work_dir)   // §7 方案3
       5. sessions 写入 workspace_id 列
```

`worker_type` 第一版仍主要走配置 fallback；`workspace.worker_preference` 占位（spec ④ 填充）。

### 9.3 会话隔离查询

- `GET /api/sessions`：按 `user_id` + 可选 `workspace_id` 过滤。webchat 用户只见自己 workspace 下的会话。
- `GET/DELETE /api/sessions/{id}`、`/history`、`/events`、`/cd`：经 `session.workspace_id → workspace.owner_user_id == 当前 user` 二次校验（复用现有 `ValidateOwnership` 思路 `manager.go:913-938`，扩展到 workspace 归属）。

### 9.4 SwitchWorkDir 语义重映射

现有 `POST /api/sessions/{id}/cd`（切工作目录，`bridge.go handleSwitchWorkDir`）在 workspace 模型下：

- 切换 `work_dir` = 切换到该 `work_dir` 对应的 workspace。
- 因 per-user 1:1，`work_dir → workspace` 确定性映射：查找（无则自动创建）`owner + workDir` 的 workspace → 在其下 resume/创建会话。
- 前端体验不变（仍是"切目录"），后端落到 workspace 实体。
- 安全校验不变（`ExpandAndAbs` + `ValidateWorkDir`）。

---

## 10. 配额扩展

### 10.1 PoolManager 三层

现有两层（全局 + per-user，`pool.go:46-51`）→ 新增 **per-workspace 并发**层：

```
Acquire 顺序（单 mutex 原子检查，沿用 pool.go 模式）:
  全局 maxSize  →  per-user 并发  →  per-workspace 并发（新）
```

- per-workspace 配额数据结构：`workspaceCount map[string]int`（key = `workspace_id`）。
- 内存维度第一版**不**细分到 workspace（YAGNI），仅并发维度。
- 配额满仍走现有"丢弃 message.delta，保留 state/done/error"背压策略（见 CLAUDE.md 背压规则）。
- 平台会话（`workspace_id IS NULL`）不参与 per-workspace 层，仅走全局 + per-user。

### 10.2 配额配置

per-workspace 并发上限：复用 `config.SecurityConfig` 或新增 `config.QuotaConfig`，默认值（建议 3）。可热重载。

---

## 11. API 端点清单

### 11.1 认证

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/auth/login` | `{username, password}` → 签发 cookie |
| POST | `/api/auth/logout` | 清除 cookie |
| GET | `/api/auth/me` | 返回当前用户信息 |
| POST | `/api/auth/accept-invite` | `{code, username, password}` → 创建账号 + 签发 cookie |

### 11.2 admin（需 admin 角色）

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/admin/invitations` | 创建邀请 `{role, ttl?}` |
| GET | `/api/admin/invitations` | 列出邀请 |
| DELETE | `/api/admin/invitations/{id}` | 撤销邀请 |
| GET | `/api/admin/users` | 列出用户 |
| PATCH | `/api/admin/users/{id}` | 启用/禁用用户 |

### 11.3 workspace

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/workspaces` | 列出自己的 workspace |
| POST | `/api/workspaces` | 创建 `{name, work_dir}` |
| GET | `/api/workspaces/{id}` | 详情 |
| PATCH | `/api/workspaces/{id}` | 更新（work_dir 可改，受 owner 沙箱 / 活跃会话 / per-owner 唯一约束） |
| DELETE | `/api/workspaces/{id}` | 删除（校验无活跃会话） |
| GET | `/api/workspaces/{id}/sessions` | 列出该 workspace 下的会话 |

### 11.4 session（改造现有）

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/sessions` | body 改为 `{workspace_id, client_session_id, worker_type?}` |
| GET | `/api/sessions` | 加 `?workspace_id=` 过滤 |
| GET/DELETE | `/api/sessions/{id}` 等 | 加 workspace 归属校验（现有端点保留） |

---

## 12. 错误码

沿用项目 `AppError{Code, ...}` 模式（`.agents/rules/golang.md`）。所有错误统一通过 `web.WriteAppError` 返回 `{"error":{"code":...,"message":...}}` JSON envelope（P2.8 统一信封）。

### 12.1 多租户端点新增码（gateway multitenancy）

| Code | HTTP | 场景 |
|---|---|---|
| `INVALID_CREDENTIALS` | 401 | 登录失败 |
| `INVITATION_EXPIRED` | 400 | 邀请码过期 |
| `INVITATION_USED` | 400 | 邀请码已使用 |
| `INVITATION_NOT_FOUND` | 404 | 邀请码不存在 |
| `WORKSPACE_NOT_FOUND` | 404 | workspace 不存在 |
| `WORKSPACE_FORBIDDEN` | 403 | workspace 归属不匹配 |
| `WORK_DIR_TAKEN` | 409 | per-user work_dir 1:1 冲突 |
| `WORK_DIR_IMMUTABLE` | 400 | workspace-bound session 尝试 `/cd` 切 work_dir（session 级不可脱离 workspace） |
| `WORKSPACE_NOT_EMPTY` | 409 | 删除时有活跃会话 |
| `WORKSPACE_VERSION_MISMATCH` | 409 | 并发更新冲突（乐观锁 CAS 失败，客户端应重新 Get 再重试） |
| `USER_DISABLED` | 403 | 用户已禁用 |
| `USERNAME_TAKEN` | 409 | 用户名已存在 |

### 12.2 Admin API 错误码

Admin API（Bearer Token, 端口 9999）与 `/api/admin/*`（Cookie, 端口 8888）共用以下通用码（两端均由 `web.WriteAppError` 产出同一信封）：

| Code | HTTP | 场景 |
|---|---|---|
| `BAD_REQUEST` | 400 | 请求体解析失败、参数校验错误、非法枚举值 |
| `UNAUTHORIZED` | 401 | Token 缺失或无效 |
| `INSUFFICIENT_SCOPE` | 403 | Bearer Token scope 不满足（Admin API） |
| `FORBIDDEN` | 403 | 非管理员访问管理端点 |
| `NOT_FOUND` | 404 | Session/Cron Job/Invitation 未找到 |
| `CONFLICT` | 409 | 资源状态冲突 |
| `RATE_LIMITED` | 429 | 触发 Rate Limit |
| `INTERNAL` | 500 | 未分类内部错误 |
| `NOT_IMPLEMENTED` | 501 | 接口尚未实现 |
| `SERVICE_UNAVAILABLE` | 503 | 数据库故障、Cron 未启用 |
| `NO_IDP` | 503 | 未配置身份提供者 |

> `USER_DISABLED`（403）见 §12.1，多租户端点与管理端共用。

配额错误复用现有 `QUOTA_EXCEEDED`（扩展 message 含 workspace 维度信息）。

---

## 13. 迁移策略

**核心原则：零破坏**。所有迁移支持 SQLite 与 PostgreSQL 双方言（`dbutil` + `sqlutil`）。

### 13.1 migration 017：建表 + sessions 加列

```sql
-- 建 users / workspaces / invitations 三表（见 §6.1-6.3）
-- sessions 加 workspace_id 列（nullable）+ 索引（见 §6.4）
```

- 三新表独立，无数据依赖。
- `sessions.workspace_id` nullable，现有会话自动得到 NULL。

### 13.2 migration 018：api_key_users 统一指向

```sql
-- 为每个 api_key_users.user_id（字符串）provision 一个 users 行：
--   users.id = 新 UUID, username = user_id 或派生, role='user', password_hash=''（API key 用户无密码）
--   status='active'
-- api_key_users.user_id 改为指向 users.id
```

- provision 用确定性映射（如 `username = "apikey:" + 原 user_id`）避免冲突。
- API key 用户 `password_hash` 为空，禁止账号登录通道（仅 API key 通道有效）——应用层校验 `password_hash == ''` 拒绝登录。

### 13.3 旧 webchat 共享会话清理

- `user_id='webchat_user'` 或 `'anonymous'` 的会话由 GC 清理（无 workspace 归属，无保留价值）。
- 清理方式：在 migration 后首次 GC 周期，或显式 `DELETE FROM sessions WHERE user_id IN ('webchat_user','anonymous')`。
- 这些会话的 session key 不含 workspace_id，与新体系不兼容。

### 13.4 平台会话/cron 不受影响

- `workspace_id` 为 NULL。
- key 派生走原路径（`DerivePlatformSessionKey` / `DeriveCronSessionKey` 不变）。
- 零行为变化。

---

## 14. 测试策略

遵循项目规范（`CLAUDE.md` + `.agents/rules/golang.md`）：

- **风格**：table-driven + `testify/require` + `t.Parallel()`，单模块 ≤5s（`-count=1 -race`）。
- **禁止** `time.Sleep` 等待异步，用 `require.Eventually` 或 channel 信号。

### 14.1 重点覆盖

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

### 14.2 隔离测试（最高优先级）

构造两个用户各自两个 workspace，断言：
- ListSessions 只返回当前用户当前 workspace 的会话。
- 跨用户/跨 workspace 访问 session 返回 `WORKSPACE_FORBIDDEN`/404。
- 配额在 workspace 维度独立计数。

---

## 15. 后续 spec 路线

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

（workspace 删除策略已在 §9.1 定为硬删除 + 校验无活跃会话，不再列入。）
