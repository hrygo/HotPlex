---
title: Admin API 参考
weight: 5
description: HotPlex Gateway 管理 API 完整参考：Session 管理、Cron 任务、系统配置与监控端点
---

# Admin API 完整参考

HotPlex Admin API 提供网关运维管理能力：会话管理、健康检查、监控指标、配置审计、日志查询和定时任务控制。默认监听 `localhost:9999`，独立于网关主端口（`8888`）。

> **交互式 API 控制台**：可直接在浏览器中浏览和测试所有端点 → [打开 API 控制台](api-console.html)
> （基于 OpenAPI 规范自动生成，与代码保持同步）

## 认证

所有 Admin 端点（`/admin/health` 和 `/admin/health/ready` 除外）均需 Bearer Token 认证。Token 通过以下两种方式传递：

```bash
# 方式一：Authorization header（推荐）
curl -H "Authorization: Bearer <token>" http://localhost:9999/admin/stats

# 方式二：Query string（适用于浏览器场景）
curl http://localhost:9999/admin/stats?access_token=<token>
```

Token 使用 `crypto/subtle.ConstantTimeCompare` 进行常量时间比较，防止时序攻击。

### Token 配置

```yaml
admin:
  enabled: true
  addr: "localhost:9999"
  tokens:                          # 简单 token，使用 default_scopes
    - "my-admin-token"
  token_scopes:                    # 细粒度 scope token
    "ops-token": ["session:read", "stats:read", "health:read"]
    "admin-token": ["session:read", "session:write", "session:delete", "stats:read", "health:read", "admin:write"]
  default_scopes: ["session:read", "session:write", "session:delete", "stats:read", "health:read", "admin:write"]
```

### Scope 权限矩阵

不同的 Scope 控制着对 Admin API 不同模块的访问级别。

| Scope Token | Health | Sessions | Stats | Config | Debug | Cron | 覆盖的核心端点 (Endpoints) |
|:---|:---:|:---:|:---:|:---:|:---:|:---:|:---|
| `health:read` | 🟢 Read | - | - | - | - | - | `/admin/health/workers` |
| `session:read` | - | 🟢 Read | - | - | - | - | `GET /admin/sessions`<br>`GET /admin/sessions/{id}/stats` |
| `session:write` | - | 🟠 Write | - | - | - | - | `POST /admin/sessions/{id}/terminate` |
| `session:delete` | - | 🔴 Delete | - | - | - | - | `DELETE /admin/sessions/{id}` |
| `stats:read` | - | - | 🟢 Read | - | - | - | `GET /admin/stats`<br>`GET /admin/metrics` |
| `config:read` | - | - | - | 🟢 Read | - | - | `POST /admin/config/validate` |
| `config:write` | - | - | - | 🟠 Write | - | - | `POST /admin/config/rollback` |
| `admin:read` | - | - | - | - | 🟢 Read | 🟢 Read | `GET /admin/logs`<br>`GET /admin/debug/...`<br>`GET /admin/bots`<br>`GET /admin/cron/jobs` |
| `admin:write` | - | - | - | - | - | 🟠 Write | `POST/PATCH/DELETE /admin/cron/jobs`<br>`POST /admin/cron/jobs/{id}/run` |

> 💡 **图例**：🟢 **Read** (只读查询) | 🟠 **Write** (状态变更/操作) | 🔴 **Delete** (物理删除)

## 安全中间件

按以下顺序执行：

1. **CORS** — `Access-Control-Allow-Origin: *`，允许 `GET/POST/DELETE/OPTIONS`
2. **Panic Recovery** — `defer recover()` 捕获 handler panic，返回 `500 Internal Server Error`
3. **Rate Limiting** — 令牌桶算法（默认 10 req/s，burst 20），超限返回 `429 Too Many Requests`
4. **IP Whitelist** — CIDR 匹配（默认 `127.0.0.0/8`, `10.0.0.0/8`），使用 `r.RemoteAddr` 防止 X-Forwarded-For 伪造
5. **Token Auth** — Bearer Token 提取 + scope 校验

Rate Limit 和 IP Whitelist 支持配置热重载，无需重启生效。

## 端点总览

### 健康检查

| 方法 | 路径 | Scope | 说明 |
|------|------|-------|------|
| GET | `/admin/health` | 无需认证 | 综合健康状态（gateway + DB + workers） |
| GET | `/admin/health/workers` | `health:read` | Worker 粒度健康状态 |
| GET | `/admin/health/ready` | 无需认证 | 就绪探针（k8s readiness） |

**GET /admin/health** — 无需认证，适合负载均衡器探活。返回 `status`（healthy/degraded）、`checks`（gateway + database + workers）和 `version`。数据库不可用时降级为 `degraded`，`database.error` 附带错误信息。

**GET /admin/health/workers** — Worker 粒度健康状态，含 `workers[]`（healthy/type/pid）和 `checked_at`。任一 Worker 不健康时返回 `503`。

### 会话管理

| 方法 | 路径 | Scope | 说明 |
|------|------|-------|------|
| GET | `/admin/sessions` | `session:read` | 列出会话（分页） |
| GET | `/admin/sessions/{id}` | `session:read` | 获取单个会话 |
| DELETE | `/admin/sessions/{id}` | `session:delete` | 物理删除会话 |
| POST | `/admin/sessions/{id}/terminate` | `session:write` | 终止会话（状态迁移） |
| GET | `/admin/sessions/{id}/stats` | `session:read` | 会话 Turn 统计 |

**GET /admin/sessions** — 支持 query 参数过滤：

```bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://localhost:9999/admin/sessions?limit=50&offset=0&platform=slack&user_id=U12345"
```

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `limit` | int | 100 | 每页数量（上限 1000） |
| `offset` | int | 0 | 偏移量 |
| `platform` | string | "" | 按平台过滤 |
| `user_id` | string | "" | 按用户过滤 |

**POST /admin/sessions/{id}/terminate** — 将会话状态迁移至 `terminated`（软终止，保留记录）。DELETE 则为物理删除。

> **注意**：会话创建不通过 Admin API，而是通过 Gateway API（`POST /api/sessions`）或 WebSocket `init` 握手完成。Admin API 仅提供只读查询和终止/删除操作。

### 监控指标

| 方法 | 路径 | Scope | 说明 |
|------|------|-------|------|
| GET | `/admin/stats` | `stats:read` | 网关聚合统计 |
| GET | `/admin/metrics` | `stats:read` | Prometheus 格式指标 |

**GET /admin/stats** — 返回 `gateway`（uptime/websocket_connections/sessions_active/sessions_total）、`workers`（按 worker_type 分组统计）和 `database`（sessions_count）。

### 配置管理

| 方法 | 路径 | Scope | 说明 |
|------|------|-------|------|
| POST | `/admin/config/validate` | `config:read` | 校验配置片段 |
| POST | `/admin/config/rollback` | `config:write` | 回滚到历史版本 |

**POST /admin/config/validate** — 校验配置合法性（不应用），请求体最大 1MB：

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"gateway":{"addr":":8888"},"pool":{"max_size":50}}' \
  http://localhost:9999/admin/config/validate
```

支持校验：`gateway`（buffer sizes >= 0）、`db`（path 长度 <= 4096）、`worker`（timeouts）、`pool`（max_size 1-10000）。返回 `{ "valid", "errors[]", "warnings[]" }`。

**POST /admin/config/rollback** — 回滚到指定版本，请求体 `{"version": 3}`，返回 `{ "ok", "rolled_back", "history_index" }`。无 configWatcher 时返回 `503`。

### 日志与调试

| 方法 | 路径 | Scope | 说明 |
|------|------|-------|------|
| GET | `/admin/logs` | `admin:read` | 最近日志（环形缓冲区） |
| GET | `/admin/debug/sessions/{id}` | `admin:read` | 会话调试快照 |

**GET /admin/logs** — 从 100 条环形缓冲区读取，`?limit=N`（最大 1000）。返回 `{ "logs[]", "total", "limit" }`。

**GET /admin/debug/sessions/{id}** — 会话详情 + 调试快照（`debug.available`、`has_worker`、`turn_count`、`last_seq_sent`、`worker_health`）。

### Cron 定时任务

| 方法 | 路径 | Scope | 说明 |
|------|------|-------|------|
| GET | `/admin/cron/jobs` | `admin:read` | 列出所有任务 |
| GET | `/admin/cron/jobs/{id}` | `admin:read` | 获取单个任务 |
| POST | `/admin/cron/jobs` | `admin:write` | 创建任务 |
| PATCH | `/admin/cron/jobs/{id}` | `admin:write` | 更新任务 |
| DELETE | `/admin/cron/jobs/{id}` | `admin:write` | 删除任务 |
| POST | `/admin/cron/jobs/{id}/run` | `admin:write` | 手动触发执行 |
| GET | `/admin/cron/jobs/{id}/runs` | `admin:read` | 执行历史 |

**请求体约定**：

- **POST /admin/cron/jobs** — JSON body 含 `name`、`schedule`（cron:/every:/at: 前缀）、`message`、`bot_id`、`owner_id`、`enabled`。返回 `201 Created`。
- **PATCH /admin/cron/jobs/{id}** — 部分更新，JSON body。返回 `204 No Content`。
- **POST /admin/cron/jobs/{id}/run** — 手动触发（异步），返回 `202 Accepted`。
- **GET /admin/cron/jobs/{id}/runs** — 查询执行历史。

Cron 未启用时返回 `503 Service Unavailable`。

### Bot 管理

Bot 状态查询、配置管理和 Agent 配置文件操作端点。

| 方法 | 路径 | Scope | 说明 |
|------|------|-------|------|
| GET | `/admin/bots` | `admin:read` | 列出所有活跃 bot |
| GET | `/admin/bots/{name}` | `admin:read` | 单个 bot 详情 |
| POST | `/admin/bots` | `admin:write` | 注册新 bot |
| PATCH | `/admin/bots/{name}` | `admin:write` | 更新 bot 配置（部分更新） |
| DELETE | `/admin/bots/{name}` | `admin:write` | 删除 bot 注册 |
| GET | `/admin/bots/config` | `admin:read` | 列出所有 bot 配置 |
| GET | `/admin/bots/{name}/config` | `admin:read` | 单个 bot 完整配置 |
| GET | `/admin/bots/{name}/config/{file}` | `admin:read` | 读取 Agent 配置文件（如 SOUL.md、AGENTS.md） |
| PUT | `/admin/bots/{name}/config/{file}` | `admin:write` | 写入 Agent 配置文件 |
| GET | `/admin/bots/{name}/preview` | `admin:read` | 预览组装后的系统提示（B+C 通道完整输出） |

**GET /admin/bots** — 返回所有活跃 bot 列表，含 name、platform、worker_type、状态等信息。

**POST /admin/bots** — 注册新 bot，JSON body 含平台凭证和配置。返回 `201 Created`。

**PATCH /admin/bots/{name}** — 部分更新 bot 配置（凭证、worker_type 等）。返回 `204 No Content`。

**GET /admin/bots/{name}/preview** — 返回该 bot 组装后的完整系统提示，包含 B 通道（directives）和 C 通道（context）内容，便于调试 Agent 人格配置。

**PUT /admin/bots/{name}/config/{file}`** — 写入指定 Agent 配置文件（如 `SOUL.md`、`AGENTS.md`、`USER.md`）。请求体为文件内容，Content-Type 为 `text/plain`。

### 网关重启

| 方法 | 路径 | Scope | 说明 |
|------|------|-------|------|
| POST | `/admin/restart` | `admin:write` | 触发网关重启 |

**POST /admin/restart** — 异步触发网关重启。Gateway 在 500ms 延迟后执行重启，立即返回 `{ "status": "restarting" }`。使用 `restart helper`（独立 PGID）确保安全隔离。未配置 restart handler 时返回 `503`。

### API Key 用户管理

管理 API Key 到用户身份的映射，用于企业级多用户 Session 隔离。每个 `user_id` 与 API Key 为 **1:1 映射**（一个 user_id 仅能关联一个 API Key），创建或更新时若 user_id 已被其他映射占用则返回 `409 Conflict`。需要数据库支持（SQLite 或 PostgreSQL），未配置 DB resolver 时返回 `501 Not Implemented`。

| 方法 | 路径 | Scope | 说明 |
|------|------|-------|------|
| GET | `/admin/api-keys` | `admin:read` | 列出所有 API Key 映射 |
| POST | `/admin/api-keys` | `admin:write` | 创建 API Key 映射 |
| GET | `/admin/api-keys/{id}` | `admin:read` | 获取单个映射详情 |
| PATCH | `/admin/api-keys/{id}` | `admin:write` | 更新映射 |
| DELETE | `/admin/api-keys/{id}` | `admin:write` | 删除映射 |

**POST /admin/api-keys** — 创建 API Key → UserID 映射。JSON body 含 `user_id`（必填，最长 128 字符）和 `description`（可选，最长 512 字符）。API Key 由系统自动生成（24 字节随机 hex）。返回 `201 Created`；若 `user_id` 已存在则返回 `409 Conflict`。

**GET /admin/api-keys** — 返回所有映射列表，`api_key` 字段自动脱敏（仅显示前 8 + 后 4 位）。

**PATCH /admin/api-keys/{id}** — 更新指定映射的 `user_id` 和 `description`。若新 `user_id` 与其他映射冲突则返回 `409 Conflict`。

**DELETE /admin/api-keys/{id}** — 物理删除映射，同时清除缓存的 resolver 条目。

## Gateway API 端点

Gateway API（`/api/sessions`）监听在网关主端口（`8888`），面向客户端 SDK 和 WebSocket 连接，使用 API Key 认证（非 Bearer Token）。

| 方法 | 路径 | 认证 | 说明 |
|------|------|------|------|
| GET | `/api/sessions` | API Key | 列出当前用户的会话 |
| POST | `/api/sessions` | API Key | 创建会话 |
| GET | `/api/sessions/{id}` | API Key | 获取单个会话 |
| DELETE | `/api/sessions/{id}` | API Key | 删除会话 |
| POST | `/api/sessions/{id}/cd` | API Key | 切换工作目录 |
| GET | `/api/sessions/{id}/history` | API Key | 获取会话历史 |
| GET | `/api/sessions/{id}/events` | API Key | 获取会话事件流 |

所有 Gateway API 端点启用 CORS（`Access-Control-Allow-Origin: *`），支持 `GET`、`POST`、`DELETE`、`OPTIONS` 方法。

### WebChat 多租户端点（spec ① / ④ / ⑥）

WebChat 多租户登录与工作区管理端点，同样监听在网关主端口 `8888`，使用 **Cookie 认证**（登录后签发的 HMAC Cookie，非 API Key）。仅在启用 WebChat 多租户（`CookieAuth` + `WorkspaceStore` 就绪）时注册，普通 SDK/WebSocket 集成不涉及。

**账号登录（spec ①）**：

| 方法 | 路径 | 认证 | 说明 |
|------|------|------|------|
| POST | `/api/auth/login` | 无 | 内建账号登录（username/password），成功签发 Cookie |
| POST | `/api/auth/logout` | Cookie | 登出并清除 Cookie |
| GET | `/api/auth/me` | Cookie | 当前登录用户信息 |
| POST | `/api/auth/accept-invite` | 无（邀请码） | 凭邀请码（body `code`）注册本地账号并**自动登录**（签发 cookie，返回 `first_login:true`），加入 workspace |
| GET | `/api/auth/bootstrap-status` | 无 | 首次部署引导状态（是否已存在管理员），登录页据此决定是否引导建号 |

**企业 SSO（spec ④，OIDC + PKCE）**：

| 方法 | 路径 | 认证 | 说明 |
|------|------|------|------|
| GET | `/api/auth/oauth/providers` | 无 | 列出已配置的 SSO provider（登录页据此渲染按钮） |
| GET | `/api/auth/oauth/{provider}/login` | 无 | 发起 OIDC 登录，重定向到 IdP（name 须匹配 `[a-z0-9-]+`） |
| GET | `/api/auth/oauth/{provider}/callback` | state cookie | OIDC 回调，完成 token exchange + ID Token 验证后签发 Cookie |

**Workspace 管理（spec ⑥ A1）**：

| 方法 | 路径 | 认证 | 说明 |
|------|------|------|------|
| POST | `/api/workspaces` | Cookie | 创建 workspace |
| GET | `/api/workspaces` | Cookie | 列出当前用户可访问的 workspace |
| GET | `/api/workspaces/{id}` | Cookie | workspace 详情（含归属校验） |
| PATCH | `/api/workspaces/{id}` | Cookie | 更新 workspace |
| DELETE | `/api/workspaces/{id}` | Cookie | 删除 workspace |

**Workspace 用户与邀请管理（spec ⑥，admin 维度）**：

| 方法 | 路径 | 认证 | 说明 |
|------|------|------|------|
| GET | `/api/admin/users` | Cookie | 列出 workspace 用户 |
| PATCH | `/api/admin/users/{id}` | Cookie | 启用/禁用用户（disable 后 per-request 即时拦截） |
| POST | `/api/admin/invitations` | Cookie | 创建邀请码 |
| GET | `/api/admin/invitations` | Cookie | 列出邀请 |
| DELETE | `/api/admin/invitations/{id}` | Cookie | 删除邀请 |

> ⚠️ **注意区分**：此处的 `/api/admin/*`（端口 `8888`，Cookie 认证，WebChat workspace 维度）与本页上方「认证」章节描述的 Admin API（端口 `9999`，Bearer Token，网关运维维度）是**两套独立端点**，认证模型和作用域完全不同，不要混淆。

## 错误响应格式

所有错误统一返回 JSON envelope（`Content-Type: application/json`），形状为：

```json
{"error":{"code":"NOT_FOUND","message":"resource not found"}}
```

`code` 为机器可读的大写下划线标识符，`message` 为人类可读的英文描述。客户端应依据 `code` 做分支，而非解析 `message`。此信封由 `web.WriteAppError` 统一生成，Admin API（Bearer）与 `/api/admin/*`（Cookie）共用同一形状。

### 错误码表

| HTTP | code | 典型场景 |
|------|------|----------|
| 400 | `BAD_REQUEST` | JSON 解析失败、参数校验错误、非法枚举值 |
| 401 | `UNAUTHORIZED` | Token 缺失或无效 |
| 401 | `INVALID_CREDENTIALS` | Cookie 会话失效或未登录 |
| 403 | `INSUFFICIENT_SCOPE` | Bearer Token scope 不满足（Admin API） |
| 403 | `FORBIDDEN` | 非管理员访问管理端点 |
| 403 | `USER_DISABLED` | 用户已被禁用 |
| 404 | `NOT_FOUND` | Session/Cron Job/Invitation 未找到 |
| 409 | `CONFLICT` | 资源状态冲突 |
| 429 | `RATE_LIMITED` | 触发 Rate Limit |
| 500 | `INTERNAL` | Handler panic、未分类内部错误 |
| 501 | `NOT_IMPLEMENTED` | 该接口尚未实现 |
| 503 | `SERVICE_UNAVAILABLE` | 数据库故障、Cron 未启用 |
| 503 | `NO_IDP` | 未配置身份提供者 |

## 常用操作示例

```bash
# 快速健康检查（无需 Token）
curl http://localhost:9999/admin/health

# 查看活跃会话
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:9999/admin/sessions?limit=10

# 查看 Prometheus 指标
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:9999/admin/metrics

# 终止异常会话
curl -X POST -H "Authorization: Bearer $TOKEN" \
  http://localhost:9999/admin/sessions/abc-123/terminate

# 查看最近日志
curl -H "Authorization: Bearer $TOKEN" \
  "http://localhost:9999/admin/logs?limit=20"

# 调试特定会话
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:9999/admin/debug/sessions/abc-123

# 触发 Cron 任务
curl -X POST -H "Authorization: Bearer $TOKEN" \
  http://localhost:9999/admin/cron/jobs/daily-health/run
```
