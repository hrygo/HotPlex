# WebChat 多租户地基（spec ①）实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 WebChat 引入真实用户身份、workspace 一等实体、会话隔离与多租户配额，使企业团队可共享一个 HotPlex 实例并实现 per-user/per-workspace 隔离。

**Architecture:** 三层抽象——身份层（`users` + `IdentityProvider` 接口，三条认证通道统一产出 `users.id`）、workspace 层（per-user 项目目录实体）、会话层（`sessions` 加 `workspace_id` 列）。零破坏迁移：Message Channel 轨（Slack/Feishu/cron）会话 `workspace_id` 为 NULL，行为不变。依赖倒置避免循环依赖：`internal/security` 定义 `UserStore` 接口，`internal/session` 实现它（`session → security` 单向依赖）。

**Tech Stack:** Go 1.22+（`http.ServeMux` 路由）、`pressly/goose/v3`（迁移）、`modernc.org/sqlite` + `jackc/pgx/v5`（双数据库）、`golang.org/x/crypto/bcrypt`（密码哈希，**项目首次引入**）、`github.com/google/uuid`（UUID 生成）、`testify/require`（测试）。

**关联设计:** [`../specs/WebChat-Multitenancy-Foundation-Design-Spec.md`](../specs/WebChat-Multitenancy-Foundation-Design-Spec.md)

**验收方式:** HTTP API 测试（curl + Go 测试覆盖隔离场景），**不依赖 webchat 前端**（前端登录 UI 归 spec ⑥）。本 spec 期间 webchat 生产 UI 断开，dev 模式保留 anonymous 兜底。

---

## 关键设计决策（执行时不可偏离）

| 决策 | 值 | 来源 |
|---|---|---|
| 双轨隔离 | 多租户**仅限 WebChat 轨**；Message Channel 轨保持现状 | spec §2 |
| 配置继承 | 两层：团队默认 → workspace 自定义（**无 per-user 层**） | spec §2.4 |
| WebChat 选 Bot | **不选**，Bot 在 WebChat 轨无作用 | spec §2.4 |
| Worker 凭证 | **不在 HotPlex 管辖**，不存储/注入/代理 | spec §2.5 |
| session key | 方案3：`workspace_id` + `work_dir` 都进 UUIDv5 hash | spec §7 |
| work_dir | workspace 创建后**不可变** | spec §6.2/§4 |
| bcrypt cost | **12**（项目首次引入，无现有值） | spec 附录 B |
| Cookie TTL | 7 天，滑动刷新阈值 3.5 天 | spec 附录 B |
| per-workspace 并发默认上限 | 3 | spec 附录 B |
| API key 用户 provision username | `apikey:<原user_id>` | spec 附录 B |
| workspace 删除 | 硬删除 + 校验无活跃会话 | spec §9.1 |

## 双方言 SQL 规则（所有迁移/store 任务通用）

每个迁移写**两份**文件：
- SQLite: `internal/session/sql/migrations/NNN_name.sql`
- PostgreSQL: `internal/session/sql/migrations-postgres/NNN_name.pg.sql`

方言差异速查（参考现有 `010_api_key_users.sql` vs `.pg.sql`）：

| 项 | SQLite | PostgreSQL |
|---|---|---|
| 自增主键 | `INTEGER PRIMARY KEY` | `"id" BIGSERIAL PRIMARY KEY` |
| 时间默认 | `INTEGER NOT NULL`（unix epoch） | 同左（项目统一用 INTEGER epoch，**非** `TIMESTAMP`，见 `sessions` 表现有列） |
| 标识符引用 | 不加引号 | 加双引号 `"col"` |
| 布尔值 | `1`/`0` | `TRUE`/`FALSE`（但项目统一用 TEXT/INTEGER 表示状态，避免 bool 列） |
| 外键 | `REFERENCES`（需 `PRAGMA foreign_keys=ON`，已启用） | `REFERENCES` |

**约定**：本 spec 所有新表沿用项目现有模式——时间用 `INTEGER NOT NULL`（unix epoch 秒），状态用 `TEXT`，主键用 `TEXT`（UUID，应用层生成）。迁移文件用 `-- +goose Up` / `-- +goose Down` 标记。

## 依赖方向（避免循环依赖）

```
cmd/hotplex ──▶ gateway ──▶ session ──▶ security   (单向)
                          ──▶ config
security 定义 UserStore 接口；session 实现 UserStore 并注入 security。
security 绝不 import session（否则循环）。
```

---

## File Structure

### 新增文件

| 文件 | 职责 |
|---|---|
| `internal/session/sql/migrations/017_multitenancy_tables.sql` | SQLite：建 users/workspaces/invitations + sessions.workspace_id |
| `internal/session/sql/migrations-postgres/017_multitenancy_tables.pg.sql` | PostgreSQL 镜像 |
| `internal/session/sql/migrations/018_api_key_users_provision.sql` | SQLite：api_key_users 统一指向 users.id |
| `internal/session/sql/migrations-postgres/018_api_key_users_provision.pg.sql` | PostgreSQL 镜像 |
| `internal/session/sql/queries/users.create.sql` 等（6 个） | users CRUD 查询 |
| `internal/session/sql/queries/workspaces.create.sql` 等（7 个） | workspaces CRUD 查询 |
| `internal/session/sql/queries/invitations.create.sql` 等（5 个） | invitations CRUD 查询 |
| `internal/security/identity_provider.go` | `IdentityProvider` 接口 + `User`/`Credentials` 类型 + `UserStore` 接口 |
| `internal/security/local_account_provider.go` | `LocalAccountProvider`（bcrypt）实现 |
| `internal/gateway/auth_handlers.go` | login/logout/me/accept-invite/admin 端点 |
| `internal/gateway/workspace_handlers.go` | workspace CRUD 端点 |
| `cmd/hotplex/admin_cmd.go` | `hotplex admin create` CLI |
| `internal/gateway/auth_handlers_test.go` | 认证隔离测试 |
| `internal/gateway/workspace_handlers_test.go` | workspace CRUD 测试 |

### 修改文件

| 文件 | 改动 |
|---|---|
| `internal/session/manager.go` | SessionInfo 加 `WorkspaceID`；`ValidateOwnership` 扩展 workspace 归属 |
| `internal/session/store.go` | Upsert/scan 加 workspace_id；新增 users/workspaces/invitations store 方法；实现 `security.UserStore` |
| `internal/session/pg_store.go` | 镜像 SQLiteStore 改动 |
| `internal/session/key.go` | `DeriveSessionKey` 加 `workspaceID` 参数 |
| `internal/session/pool.go` | 新增 per-workspace 并发配额层 |
| `internal/security/cookie.go` | cookie 编码真实 user.id + TTL + 滑动刷新 |
| `internal/security/auth.go` | `AuthenticateRequest` 返回真实 users.id（接入 IdentityProvider） |
| `internal/gateway/api.go` | CreateSession 改 workspace_id 入参 + 归属校验 + work_dir 取自 workspace |
| `internal/webchat/server.go` | 移除固定 `webchat_user` cookie 签发 |
| `cmd/hotplex/routes.go` | 注册 /api/auth/* /api/admin/* /api/workspaces 路由 |
| `internal/config/config_types.go` | 新增 `QuotaConfig.MaxConcurrentPerWorkspace` |
| `go.mod` / `go.sum` | 引入 `golang.org/x/crypto` |

---

## Phase 0：数据模型迁移

### Task 0.1: migration 017 — 建表 + sessions 加列（SQLite）

**Files:**
- Create: `internal/session/sql/migrations/017_multitenancy_tables.sql`

- [ ] **Step 1: 写迁移 SQL**

```sql
-- +goose Up
-- WebChat 多租户地基：users / workspaces / invitations 三表 + sessions.workspace_id

CREATE TABLE users (
    id            TEXT PRIMARY KEY,
    username      TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL DEFAULT '',
    role          TEXT NOT NULL DEFAULT 'user',
    display_name  TEXT NOT NULL DEFAULT '',
    status        TEXT NOT NULL DEFAULT 'active',
    created_at    INTEGER NOT NULL,
    updated_at    INTEGER NOT NULL,
    last_login_at INTEGER
);
CREATE INDEX idx_users_status ON users(status);

CREATE TABLE workspaces (
    id                     TEXT PRIMARY KEY,
    owner_user_id          TEXT NOT NULL REFERENCES users(id),
    name                   TEXT NOT NULL,
    work_dir               TEXT NOT NULL,
    agent_config_overrides TEXT,
    worker_preference      TEXT,
    status                 TEXT NOT NULL DEFAULT 'active',
    created_at             INTEGER NOT NULL,
    updated_at             INTEGER NOT NULL,
    UNIQUE(owner_user_id, work_dir)
);
CREATE INDEX idx_workspaces_owner ON workspaces(owner_user_id);

CREATE TABLE invitations (
    id         TEXT PRIMARY KEY,
    code       TEXT NOT NULL UNIQUE,
    created_by TEXT NOT NULL REFERENCES users(id),
    role       TEXT NOT NULL DEFAULT 'user',
    used_by    TEXT REFERENCES users(id),
    expires_at INTEGER NOT NULL,
    created_at INTEGER NOT NULL,
    used_at    INTEGER
);
CREATE INDEX idx_invitations_code ON invitations(code);

ALTER TABLE sessions ADD COLUMN workspace_id TEXT REFERENCES workspaces(id);
CREATE INDEX idx_sessions_workspace ON sessions(workspace_id);

-- +goose Down
DROP INDEX IF EXISTS idx_sessions_workspace;
ALTER TABLE sessions DROP COLUMN workspace_id;

DROP INDEX IF EXISTS idx_invitations_code;
DROP TABLE IF EXISTS invitations;

DROP INDEX IF EXISTS idx_workspaces_owner;
DROP TABLE IF EXISTS workspaces;

DROP INDEX IF EXISTS idx_users_status;
DROP TABLE IF EXISTS users;
```

- [ ] **Step 2: 验证 SQL 语法（ goose dry-run 不可用，用 sqlite3 CLI 手动验证建表）**

Run: `sqlite3 /tmp/test_017.sql < internal/session/sql/migrations/017_multitenancy_tables.sql 2>&1 | head`

> 注：`-- +goose Up` 标记是 goose 的，sqlite3 CLI 会当作注释忽略，但会执行下方 DDL。预期：无错误输出（空表建成功）。若报错 `duplicate column` 或语法错，修正后重试。

Expected: 无输出（建表成功）

- [ ] **Step 3: 提交**

```bash
git add internal/session/sql/migrations/017_multitenancy_tables.sql
git commit -m "feat(session): migration 017 — users/workspaces/invitations 表 + sessions.workspace_id (SQLite)"
```

### Task 0.2: migration 017 PostgreSQL 镜像

**Files:**
- Create: `internal/session/sql/migrations-postgres/017_multitenancy_tables.pg.sql`

- [ ] **Step 1: 写 PG 版迁移（标识符加双引号，逻辑与 SQLite 一致）**

```sql
-- +goose Up
-- WebChat 多租户地基：users / workspaces / invitations + sessions.workspace_id

CREATE TABLE "users" (
    "id"            TEXT PRIMARY KEY,
    "username"      TEXT NOT NULL UNIQUE,
    "password_hash" TEXT NOT NULL DEFAULT '',
    "role"          TEXT NOT NULL DEFAULT 'user',
    "display_name"  TEXT NOT NULL DEFAULT '',
    "status"        TEXT NOT NULL DEFAULT 'active',
    "created_at"    BIGINT NOT NULL,
    "updated_at"    BIGINT NOT NULL,
    "last_login_at" BIGINT
);
CREATE INDEX "idx_users_status" ON "users"("status");

CREATE TABLE "workspaces" (
    "id"                     TEXT PRIMARY KEY,
    "owner_user_id"          TEXT NOT NULL REFERENCES "users"("id"),
    "name"                   TEXT NOT NULL,
    "work_dir"               TEXT NOT NULL,
    "agent_config_overrides" TEXT,
    "worker_preference"      TEXT,
    "status"                 TEXT NOT NULL DEFAULT 'active',
    "created_at"             BIGINT NOT NULL,
    "updated_at"             BIGINT NOT NULL,
    UNIQUE("owner_user_id", "work_dir")
);
CREATE INDEX "idx_workspaces_owner" ON "workspaces"("owner_user_id");

CREATE TABLE "invitations" (
    "id"         TEXT PRIMARY KEY,
    "code"       TEXT NOT NULL UNIQUE,
    "created_by" TEXT NOT NULL REFERENCES "users"("id"),
    "role"       TEXT NOT NULL DEFAULT 'user',
    "used_by"    TEXT REFERENCES "users"("id"),
    "expires_at" BIGINT NOT NULL,
    "created_at" BIGINT NOT NULL,
    "used_at"    BIGINT
);
CREATE INDEX "idx_invitations_code" ON "invitations"("code");

ALTER TABLE "sessions" ADD COLUMN "workspace_id" TEXT REFERENCES "workspaces"("id");
CREATE INDEX "idx_sessions_workspace" ON "sessions"("workspace_id");

-- +goose Down
DROP INDEX IF EXISTS "idx_sessions_workspace";
ALTER TABLE "sessions" DROP COLUMN IF EXISTS "workspace_id";

DROP INDEX IF EXISTS "idx_invitations_code";
DROP TABLE IF EXISTS "invitations";

DROP INDEX IF EXISTS "idx_workspaces_owner";
DROP TABLE IF EXISTS "workspaces";

DROP INDEX IF EXISTS "idx_users_status";
DROP TABLE IF EXISTS "users";
```

- [ ] **Step 2: 提交**

```bash
git add internal/session/sql/migrations-postgres/017_multitenancy_tables.pg.sql
git commit -m "feat(session): migration 017 — PostgreSQL 镜像"
```

### Task 0.3: migration 018 — api_key_users provision（双方言）

> 目的：把现有 `api_key_users.user_id`（任意字符串）统一指向 `users.id`。为每个已有 user_id 在 users 表 provision 一行（username=`apikey:<原user_id>`，password_hash 为空=禁止账号登录）。

**Files:**
- Create: `internal/session/sql/migrations/018_api_key_users_provision.sql`
- Create: `internal/session/sql/migrations-postgres/018_api_key_users_provision.pg.sql`

- [ ] **Step 1: SQLite 版**

```sql
-- +goose Up
-- 为每个 api_key_users.user_id 在 users 表 provision 一行，并把 user_id 指向新 users.id。
-- password_hash='' 表示禁止账号登录通道（仅 API key 通道有效）。
-- username='apikey:<原user_id>' 用确定性前缀避免与真人 username 冲突。

INSERT INTO users (id, username, password_hash, role, display_name, status, created_at, updated_at)
SELECT
    lower(hex(randomblob(16))),
    'apikey:' || user_id,
    '',
    'user',
    '',
    'active',
    strftime('%s','now'),
    strftime('%s','now')
FROM (SELECT DISTINCT user_id FROM api_key_users) AS d
WHERE NOT EXISTS (SELECT 1 FROM users WHERE username = 'apikey:' || d.user_id);

UPDATE api_key_users
SET user_id = (SELECT u.id FROM users u WHERE u.username = 'apikey:' || api_key_users.user_id)
WHERE EXISTS (SELECT 1 FROM users u WHERE u.username = 'apikey:' || api_key_users.user_id);

-- +goose Down
-- 不可逆地还原 user_id（原值已被覆盖），Down 仅清空 provision 的 users 行。
DELETE FROM users WHERE username LIKE 'apikey:%';
```

- [ ] **Step 2: PostgreSQL 版**

```sql
-- +goose Up
INSERT INTO "users" ("id", "username", "password_hash", "role", "display_name", "status", "created_at", "updated_at")
SELECT
    gen_random_uuid(),
    'apikey:' || user_id,
    '',
    'user',
    '',
    'active',
    EXTRACT(EPOCH FROM NOW())::BIGINT,
    EXTRACT(EPOCH FROM NOW())::BIGINT
FROM (SELECT DISTINCT user_id FROM "api_key_users") AS d
WHERE NOT EXISTS (SELECT 1 FROM "users" u WHERE u.username = 'apikey:' || d.user_id);

UPDATE "api_key_users"
SET user_id = (SELECT u.id FROM "users" u WHERE u.username = 'apikey:' || "api_key_users".user_id)
WHERE EXISTS (SELECT 1 FROM "users" u WHERE u.username = 'apikey:' || "api_key_users".user_id);

-- +goose Down
DELETE FROM "users" WHERE username LIKE 'apikey:%';
```

- [ ] **Step 3: 提交**

```bash
git add internal/session/sql/migrations/018_api_key_users_provision.sql \
        internal/session/sql/migrations-postgres/018_api_key_users_provision.pg.sql
git commit -m "feat(session): migration 018 — api_key_users 统一指向 users.id"
```

### Task 0.4: SessionInfo 加 WorkspaceID + Upsert/scan 改造

**Files:**
- Modify: `internal/session/manager.go:182-221`（SessionInfo struct）
- Modify: `internal/session/sql/queries/sessions.upsert_session.sql`
- Modify: `internal/session/store.go`（Upsert 参数、scanSession）
- Modify: `internal/session/pg_store.go`（同步 Upsert）

- [ ] **Step 1: 先写失败测试 — SessionInfo 持久化 workspace_id**

Create `internal/session/store_workspace_test.go`:

```go
package session

import (
	"context"
	"testing"
	"time"

	"github.com/hrygo/hotplex/internal/worker"
	"github.com/stretchr/testify/require"
)

func TestUpsertAndGet_PreservesWorkspaceID(t *testing.T) {
	t.Parallel()
	store, cleanup := newTestStore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()
	wsID := "ws-123"
	info := &SessionInfo{
		ID:         "sess-ws-1",
		UserID:     "u1",
		OwnerID:    "u1",
		WorkerType: worker.TypeClaudeCode,
		State:      "created",
		WorkDir:    "/tmp/proj",
		WorkspaceID: wsID,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	require.NoError(t, store.Upsert(ctx, info))

	got, err := store.Get(ctx, "sess-ws-1")
	require.NoError(t, err)
	require.Equal(t, wsID, got.WorkspaceID, "workspace_id 必须持久化并读回")
}
```

> `newTestStore(t)` 是项目现有测试辅助（见 `store_test.go`），返回临时 SQLiteStore + cleanup。若签名不同，沿用现有测试里的构造方式。

- [ ] **Step 2: 运行测试，确认失败**

Run: `go test ./internal/session/ -run TestUpsertAndGet_PreservesWorkspaceID -count=1`

Expected: 编译失败（`info.WorkspaceID` undefined）

- [ ] **Step 3: 给 SessionInfo 加字段**

Modify `internal/session/manager.go`，在 `SessionInfo` struct 的 `ClientKey` 字段后追加：

```go
    ClientKey       string              `json:"client_key,omitempty"`
    WorkspaceID     string              `json:"workspace_id,omitempty"` // WebChat 多租户：会话归属的 workspace（平台/cron 会话为空）
}
```

- [ ] **Step 4: 改 upsert SQL 加 workspace_id 列**

Modify `internal/session/sql/queries/sessions.upsert_session.sql`，在列清单末尾（`client_key` 后）加 `workspace_id`，对应占位符与 ON CONFLICT 保持不覆盖（workspace_id 创建后不变）:

```sql
INSERT INTO sessions (id, user_id, owner_id, bot_id, bot_name, worker_session_id, worker_type, state,
    platform, platform_key_json, work_dir, title, created_at, updated_at, expires_at,
    idle_expires_at, context_json, source, client_key, workspace_id)
 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
 ON CONFLICT(id) DO UPDATE SET
  state=excluded.state,
  owner_id=CASE WHEN excluded.owner_id != '' THEN excluded.owner_id ELSE sessions.owner_id END,
  bot_name=CASE WHEN excluded.bot_name != '' THEN excluded.bot_name ELSE sessions.bot_name END,
  updated_at=excluded.updated_at,
  expires_at=excluded.expires_at,
  idle_expires_at=excluded.idle_expires_at,
  title=CASE WHEN excluded.title != '' THEN excluded.title ELSE sessions.title END,
  context_json=excluded.context_json,
  source=CASE WHEN excluded.source != '' THEN excluded.source ELSE sessions.source END,
  client_key=CASE WHEN excluded.client_key != '' THEN excluded.client_key ELSE sessions.client_key END,
  -- workspace_id 创建后不可变：ON CONFLICT 不写入 excluded，保留原值
  workspace_id=CASE WHEN sessions.workspace_id IS NULL THEN excluded.workspace_id ELSE sessions.workspace_id END;
```

> 关键：ON CONFLICT 时只在原值为 NULL 时才写入，防止 workspace_id 被后续 upsert 覆盖。现在共 20 个 `?` 占位符。

- [ ] **Step 5: 改 store.go 的 Upsert 传参与 SELECT/scan**

Modify `internal/session/store.go` 的 `scanSession`：`sc.Scan(...)` 末尾在 `&info.ClientKey,` 后加 `&info.WorkspaceID,`。注意 `scanSession` 现有变量列表末尾需补 `var workspaceID sql.NullString`（nullable 列），Scan 后 `info.WorkspaceID = workspaceID.String`。

(c) `store.list_sessions.sql` 的 SELECT 同样末尾加 `workspace_id`（若该查询用 `scanSession`，scan 已覆盖）。

> **关于 Upsert 参数**：现在共 20 个参数，末尾是 `info.WorkspaceID`。完整调用：

```go
        _, err := s.db.ExecContext(ctx, queries["sessions.upsert_session"],
            info.ID, info.UserID, info.OwnerID, info.BotID, info.BotName,
            info.WorkerSessionID, info.WorkerType, string(info.State),
            info.Platform, string(pkJSON), info.WorkDir, info.Title,
            info.CreatedAt, info.UpdatedAt, info.ExpiresAt, info.IdleExpiresAt,
            string(ctxJSON), info.Source, info.ClientKey, info.WorkspaceID,
        )
```

- [ ] **Step 6: 改 pg_store.go 镜像**

`internal/session/pg_store.go` 的 Upsert 实现同样在参数末尾追加 `info.WorkspaceID`，使其与 20 占位符对齐。PG 的 SELECT 查询走同一 `queries` map（rebind 后），scanSession 共用。

- [ ] **Step 7: 运行测试确认通过**

Run: `go test ./internal/session/ -run TestUpsertAndGet_PreservesWorkspaceID -count=1 -race`

Expected: PASS

- [ ] **Step 8: 提交**

```bash
git add internal/session/manager.go internal/session/store.go internal/session/pg_store.go \
        internal/session/sql/queries/sessions.upsert_session.sql \
        internal/session/sql/queries/store.get_session.sql \
        internal/session/sql/queries/store.list_sessions.sql \
        internal/session/store_workspace_test.go
git commit -m "feat(session): SessionInfo 持久化 workspace_id（向后兼容 nullable）"
```

---

## Phase 1：Store 层 — users / workspaces / invitations CRUD

### Task 1.1: users store 查询文件

**Files:**
- Create: `internal/session/sql/queries/users.create.sql`
- Create: `internal/session/sql/queries/users.get_by_id.sql`
- Create: `internal/session/sql/queries/users.get_by_username.sql`
- Create: `internal/session/sql/queries/users.list.sql`
- Create: `internal/session/sql/queries/users.update_status.sql`
- Create: `internal/session/sql/queries/users.touch_last_login.sql`

- [ ] **Step 1: 写 6 个查询文件**

`users.create.sql`:
```sql
INSERT INTO users (id, username, password_hash, role, display_name, status, created_at, updated_at, last_login_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, NULL)
```

`users.get_by_id.sql`:
```sql
SELECT id, username, password_hash, role, display_name, status, created_at, updated_at, last_login_at
FROM users WHERE id = ?
```

`users.get_by_username.sql`:
```sql
SELECT id, username, password_hash, role, display_name, status, created_at, updated_at, last_login_at
FROM users WHERE username = ?
```

`users.list.sql`:
```sql
SELECT id, username, password_hash, role, display_name, status, created_at, updated_at, last_login_at
FROM users ORDER BY created_at ASC LIMIT ? OFFSET ?
```

`users.update_status.sql`:
```sql
UPDATE users SET status = ?, updated_at = ? WHERE id = ?
```

`users.touch_last_login.sql`:
```sql
UPDATE users SET last_login_at = ?, updated_at = ? WHERE id = ?
```

- [ ] **Step 2: 提交**

```bash
git add internal/session/sql/queries/users.*.sql
git commit -m "feat(session): users store 查询文件"
```

### Task 1.2: users store Go 方法 + 实现 security.UserStore

**Files:**
- Modify: `internal/security/identity_provider.go`（Create — 接口与类型）
- Modify: `internal/session/store.go`（加 User 类型 + store 方法）
- Modify: `internal/session/pg_store.go`（镜像）

- [ ] **Step 1: 写 security 包的 UserStore 接口**

Create `internal/security/identity_provider.go`:

```go
package security

import "context"

// User is the canonical user record surfaced to the identity layer.
// Defined in security (not session) to keep the dependency direction single:
// session implements UserStore; security never imports session.
type User struct {
	ID           string
	Username     string
	PasswordHash string
	Role         string
	Status       string
	DisplayName  string
}

// UserStore is the persistence interface required by LocalAccountProvider.
// Implemented by session.SQLiteStore / session.pgStore.
type UserStore interface {
	CreateUser(ctx context.Context, u *User, now int64) error
	GetUserByID(ctx context.Context, id string) (*User, error)
	GetUserByUsername(ctx context.Context, username string) (*User, error)
}

// Credentials is a marker interface for credential payloads.
type Credentials interface{ Kind() string }

// LoginCredentials carries username/password for the account-login channel.
type LoginCredentials struct {
	Username string
	Password string
}

func (LoginCredentials) Kind() string { return "login" }

// IdentityProvider authenticates credentials and looks up users.
// LocalAccountProvider is the first implementation; OAuthProvider is a future second.
type IdentityProvider interface {
	Authenticate(ctx context.Context, creds Credentials) (userID string, err error)
	Lookup(ctx context.Context, userID string) (*User, error)
}

// Sentinel errors for the identity layer.
type IdentityError struct{ Code string }

func (e *IdentityError) Error() string { return "identity: " + e.Code }

const (
	ErrCodeInvalidCredentials = "INVALID_CREDENTIALS"
	ErrCodeUserDisabled       = "USER_DISABLED"
	ErrCodeUserNotFound       = "USER_NOT_FOUND"
)
```

- [ ] **Step 2: 写失败测试 — CreateUser + GetUserByUsername**

Create `internal/session/users_store_test.go`:

```go
package session

import (
	"context"
	"testing"

	"github.com/hrygo/hotplex/internal/security"
	"github.com/stretchr/testify/require"
)

func TestUsersStore_CreateAndGet(t *testing.T) {
	t.Parallel()
	store, cleanup := newTestStore(t)
	defer cleanup()

	ctx := context.Background()
	u := &security.User{
		ID:           "u-1",
		Username:     "alice",
		PasswordHash: "$2a$12$fakehash",
		Role:         "admin",
		Status:       "active",
	}
	require.NoError(t, store.CreateUser(ctx, u, 1700000000))

	got, err := store.GetUserByUsername(ctx, "alice")
	require.NoError(t, err)
	require.Equal(t, "u-1", got.ID)
	require.Equal(t, "$2a$12$fakehash", got.PasswordHash)
	require.Equal(t, "admin", got.Role)
}

func TestUsersStore_GetByUsername_NotFound(t *testing.T) {
	t.Parallel()
	store, cleanup := newTestStore(t)
	defer cleanup()
	_, err := store.GetUserByUsername(context.Background(), "nobody")
	require.ErrorIs(t, err, ErrUserNotFound)
}
```

- [ ] **Step 3: 运行测试确认失败**

Run: `go test ./internal/session/ -run TestUsersStore -count=1`

Expected: 编译失败（CreateUser/GetUserByUsername/ErrUserNotFound 未定义）

- [ ] **Step 4: 在 store.go 加 User store 实现**

Modify `internal/session/store.go`，加 import `"github.com/hrygo/hotplex/internal/security"`，并新增：

```go
// ErrUserNotFound is returned when a user lookup yields no row.
var ErrUserNotFound = errors.New("session: user not found")

func scanUser(sc rowScanner) (*security.User, error) {
	var u security.User
	var lastLogin sql.NullInt64
	err := sc.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.DisplayName, &u.Status,
		&createdAt, &updatedAt, &lastLogin)
	if err != nil {
		return nil, err
	}
	return &u, nil
}
```

> 注意：上面 `scanUser` 引用了 `createdAt`/`updatedAt`——需改为本地变量。完整正确实现：

```go
func scanUser(sc rowScanner) (*security.User, error) {
	var u security.User
	var createdAt, updatedAt, lastLogin sql.NullInt64
	err := sc.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.DisplayName, &u.Status,
		&createdAt, &updatedAt, &lastLogin)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (s *SQLiteStore) CreateUser(ctx context.Context, u *security.User, now int64) error {
	return s.writeMu.WithLock(func() error {
		_, err := s.db.ExecContext(ctx, queries["users.create"],
			u.ID, u.Username, u.PasswordHash, u.Role, u.DisplayName, u.Status, now, now)
		return err
	})
}

func (s *SQLiteStore) GetUserByID(ctx context.Context, id string) (*security.User, error) {
	row := s.db.QueryRowContext(ctx, queries["users.get_by_id"], id)
	u, err := scanUser(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	return u, err
}

func (s *SQLiteStore) GetUserByUsername(ctx context.Context, username string) (*security.User, error) {
	row := s.db.QueryRowContext(ctx, queries["users.get_by_username"], username)
	u, err := scanUser(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	return u, err
}
```

- [ ] **Step 5: pg_store.go 镜像（用 s.queries + s.db）**

`internal/session/pg_store.go` 新增同名方法，把 `queries[...]` 换成 `s.queries[...]`，`s.db` 换成 `s.db.DB`（pgStore 的 db 是 `*dbutil.DB`，内嵌 `*sql.DB`，可直接 `s.db.QueryRowContext`）。签名完全一致以满足 `security.UserStore` 接口。

- [ ] **Step 6: 运行测试确认通过**

Run: `go test ./internal/session/ -run TestUsersStore -count=1 -race`

Expected: PASS

- [ ] **Step 7: 提交**

```bash
git add internal/security/identity_provider.go internal/session/store.go internal/session/pg_store.go \
        internal/session/users_store_test.go
git commit -m "feat(session): users store + security.UserStore 接口（依赖倒置避免循环依赖）"
```

### Task 1.3: workspaces store（查询 + 方法）

**Files:**
- Create: `internal/session/sql/queries/workspaces.create.sql` 等 7 个
- Modify: `internal/session/store.go`（Workspace 类型 + 方法）
- Modify: `internal/session/pg_store.go`（镜像）

- [ ] **Step 1: 写 7 个查询文件**

`workspaces.create.sql`:
```sql
INSERT INTO workspaces (id, owner_user_id, name, work_dir, agent_config_overrides, worker_preference, status, created_at, updated_at)
VALUES (?, ?, ?, ?, NULL, NULL, 'active', ?, ?)
```

`workspaces.get_by_id.sql`:
```sql
SELECT id, owner_user_id, name, work_dir, agent_config_overrides, worker_preference, status, created_at, updated_at
FROM workspaces WHERE id = ?
```

`workspaces.list_by_owner.sql`:
```sql
SELECT id, owner_user_id, name, work_dir, agent_config_overrides, worker_preference, status, created_at, updated_at
FROM workspaces WHERE owner_user_id = ? AND status = 'active' ORDER BY created_at ASC
```

`workspaces.get_by_owner_and_workdir.sql`:
```sql
SELECT id, owner_user_id, name, work_dir, agent_config_overrides, worker_preference, status, created_at, updated_at
FROM workspaces WHERE owner_user_id = ? AND work_dir = ? AND status = 'active'
```

`workspaces.update.sql`:
```sql
UPDATE workspaces SET name = ?, agent_config_overrides = ?, worker_preference = ?, updated_at = ? WHERE id = ?
```

`workspaces.delete.sql`:
```sql
DELETE FROM workspaces WHERE id = ?
```

`workspaces.count_active_sessions.sql`:
```sql
SELECT COUNT(*) FROM sessions WHERE workspace_id = ? AND state IN ('created','running','idle')
```

- [ ] **Step 2: 写失败测试**

Create `internal/session/workspaces_store_test.go`:

```go
package session

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWorkspacesStore_CreateUniqueConflict(t *testing.T) {
	t.Parallel()
	store, cleanup := newTestStore(t)
	defer cleanup()
	ctx := context.Background()

	// 先建 owner user（FK 约束）
	require.NoError(t, store.CreateUser(ctx, &securityUser("u-1", "alice"), 1700000000))

	w := &Workspace{ID: "ws-1", OwnerUserID: "u-1", Name: "proj", WorkDir: "/tmp/proj"}
	require.NoError(t, store.CreateWorkspace(ctx, w, 1700000000))

	// 同 owner 同 work_dir 应冲突（UNIQUE）
	w2 := &Workspace{ID: "ws-2", OwnerUserID: "u-1", Name: "proj2", WorkDir: "/tmp/proj"}
	err := store.CreateWorkspace(ctx, w2, 1700000000)
	require.Error(t, err, "同 owner+work_dir 必须 1:1 唯一")
}

func TestWorkspacesStore_GetByOwnerAndWorkDir(t *testing.T) {
	t.Parallel()
	store, cleanup := newTestStore(t)
	defer cleanup()
	ctx := context.Background()
	require.NoError(t, store.CreateUser(ctx, &securityUser("u-1", "alice"), 1700000000))
	require.NoError(t, store.CreateWorkspace(ctx, &Workspace{ID: "ws-1", OwnerUserID: "u-1", Name: "p", WorkDir: "/tmp/x"}, 1700000000))

	got, err := store.GetWorkspaceByOwnerAndWorkDir(ctx, "u-1", "/tmp/x")
	require.NoError(t, err)
	require.Equal(t, "ws-1", got.ID)
}

// securityUser 构造测试用 security.User（避免与本包 User 混淆）。
func securityUser(id, name string) *security.User {
	return &security.User{ID: id, Username: name, Role: "user", Status: "active"}
}
```

- [ ] **Step 3: 运行确认失败**

Run: `go test ./internal/session/ -run TestWorkspacesStore -count=1`

Expected: 编译失败（Workspace/CreateWorkspace 未定义）

- [ ] **Step 4: 加 Workspace 类型与 store 方法**

Modify `internal/session/store.go`:

```go
// Workspace is a per-user named project directory. See spec §6.2.
type Workspace struct {
	ID                   string
	OwnerUserID          string
	Name                 string
	WorkDir              string
	AgentConfigOverrides string // JSON; spec ② 填充
	WorkerPreference     string // spec ③ 填充
	Status               string
}

func scanWorkspace(sc rowScanner) (*Workspace, error) {
	var w Workspace
	var overrides sql.NullString
	var pref sql.NullString
	err := sc.Scan(&w.ID, &w.OwnerUserID, &w.Name, &w.WorkDir, &overrides, &pref, &w.Status,
		&createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	w.AgentConfigOverrides = overrides.String
	w.WorkerPreference = pref.String
	return &w, nil
}
```

> 同样 `createdAt`/`updatedAt` 用本地变量。正确版：

```go
func scanWorkspace(sc rowScanner) (*Workspace, error) {
	var w Workspace
	var overrides, pref sql.NullString
	var createdAt, updatedAt sql.NullInt64
	err := sc.Scan(&w.ID, &w.OwnerUserID, &w.Name, &w.WorkDir, &overrides, &pref, &w.Status,
		&createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	w.AgentConfigOverrides = overrides.String
	w.WorkerPreference = pref.String
	return &w, nil
}

func (s *SQLiteStore) CreateWorkspace(ctx context.Context, w *Workspace, now int64) error {
	return s.writeMu.WithLock(func() error {
		_, err := s.db.ExecContext(ctx, queries["workspaces.create"],
			w.ID, w.OwnerUserID, w.Name, w.WorkDir, now, now)
		return err
	})
}

func (s *SQLiteStore) GetWorkspaceByID(ctx context.Context, id string) (*Workspace, error) {
	row := s.db.QueryRowContext(ctx, queries["workspaces.get_by_id"], id)
	w, err := scanWorkspace(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrWorkspaceNotFound
	}
	return w, err
}

func (s *SQLiteStore) ListWorkspacesByOwner(ctx context.Context, ownerUserID string) ([]*Workspace, error) {
	rows, err := s.db.QueryContext(ctx, queries["workspaces.list_by_owner"], ownerUserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Workspace
	for rows.Next() {
		w, err := scanWorkspace(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) GetWorkspaceByOwnerAndWorkDir(ctx context.Context, ownerUserID, workDir string) (*Workspace, error) {
	row := s.db.QueryRowContext(ctx, queries["workspaces.get_by_owner_and_workdir"], ownerUserID, workDir)
	w, err := scanWorkspace(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrWorkspaceNotFound
	}
	return w, err
}

func (s *SQLiteStore) UpdateWorkspace(ctx context.Context, w *Workspace, now int64) error {
	return s.writeMu.WithLock(func() error {
		_, err := s.db.ExecContext(ctx, queries["workspaces.update"],
			w.Name, nullableString(w.AgentConfigOverrides), nullableString(w.WorkerPreference), now, w.ID)
		return err
	})
}

func (s *SQLiteStore) DeleteWorkspace(ctx context.Context, id string) error {
	return s.writeMu.WithLock(func() error {
		_, err := s.db.ExecContext(ctx, queries["workspaces.delete"], id)
		return err
	})
}

func (s *SQLiteStore) CountActiveSessionsInWorkspace(ctx context.Context, workspaceID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, queries["workspaces.count_active_sessions"], workspaceID).Scan(&n)
	return n, err
}
```

加哨兵与辅助：

```go
var ErrWorkspaceNotFound = errors.New("session: workspace not found")

// nullableString returns "" as NULL (for optional JSON/preference columns).
func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
```

- [ ] **Step 5: pg_store.go 镜像**（7 个方法，`queries[...]` → `s.queries[...]`）

- [ ] **Step 6: 运行测试确认通过**

Run: `go test ./internal/session/ -run TestWorkspacesStore -count=1 -race`

Expected: PASS

- [ ] **Step 7: 提交**

```bash
git add internal/session/sql/queries/workspaces.*.sql internal/session/store.go internal/session/pg_store.go \
        internal/session/workspaces_store_test.go
git commit -m "feat(session): workspaces store（CRUD + per-user 1:1 唯一约束 + 活跃会话计数）"
```

### Task 1.4: invitations store（查询 + 方法）

**Files:**
- Create: `internal/session/sql/queries/invitations.create.sql` 等 5 个
- Modify: `internal/session/store.go`（Invitation 类型 + 方法）
- Modify: `internal/session/pg_store.go`（镜像）

- [ ] **Step 1: 写 5 个查询文件**

`invitations.create.sql`:
```sql
INSERT INTO invitations (id, code, created_by, role, used_by, expires_at, created_at, used_at)
VALUES (?, ?, ?, ?, NULL, ?, ?, NULL)
```

`invitations.get_by_code.sql`:
```sql
SELECT id, code, created_by, role, used_by, expires_at, created_at, used_at
FROM invitations WHERE code = ?
```

`invitations.mark_used.sql`:
```sql
UPDATE invitations SET used_by = ?, used_at = ? WHERE id = ? AND used_by IS NULL
```

`invitations.list.sql`:
```sql
SELECT id, code, created_by, role, used_by, expires_at, created_at, used_at
FROM invitations ORDER BY created_at DESC
```

`invitations.delete.sql`:
```sql
DELETE FROM invitations WHERE id = ?
```

- [ ] **Step 2: 写失败测试**

Create `internal/session/invitations_store_test.go`:

```go
package session

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInvitationsStore_CreateAndMarkUsed(t *testing.T) {
	t.Parallel()
	store, cleanup := newTestStore(t)
	defer cleanup()
	ctx := context.Background()
	require.NoError(t, store.CreateUser(ctx, &securityUser("admin-1", "admin"), 1700000000))

	inv := &Invitation{ID: "inv-1", Code: "CODE123", CreatedBy: "admin-1", Role: "user", ExpiresAt: 1800000000}
	require.NoError(t, store.CreateInvitation(ctx, inv, 1700000000))

	got, err := store.GetInvitationByCode(ctx, "CODE123")
	require.NoError(t, err)
	require.Nil(t, got.UsedBy, "新建邀请 used_by 应为 NULL")

	require.NoError(t, store.MarkInvitationUsed(ctx, "inv-1", "new-user-1", 1750000000))
	got2, _ := store.GetInvitationByCode(ctx, "CODE123")
	require.NotNil(t, got2.UsedBy)
	require.Equal(t, "new-user-1", *got2.UsedBy)

	// 重复 mark 应失败（已使用）
	err = store.MarkInvitationUsed(ctx, "inv-1", "new-user-2", 1750000001)
	require.Error(t, err, "已使用的邀请不可二次使用")
}
```

- [ ] **Step 3: 运行确认失败**

Run: `go test ./internal/session/ -run TestInvitationsStore -count=1`

Expected: 编译失败

- [ ] **Step 4: 加 Invitation 类型与方法**

Modify `internal/session/store.go`:

```go
// Invitation is a one-time invite code. See spec §6.3.
type Invitation struct {
	ID        string
	Code      string
	CreatedBy string
	Role      string
	UsedBy    *string // nil = unused
	ExpiresAt int64
	CreatedAt int64
	UsedAt    *int64 // nil = unused
}

func scanInvitation(sc rowScanner) (*Invitation, error) {
	var inv Invitation
	var usedBy sql.NullString
	var usedAt sql.NullInt64
	var createdAt sql.NullInt64
	err := sc.Scan(&inv.ID, &inv.Code, &inv.CreatedBy, &inv.Role, &usedBy, &inv.ExpiresAt, &createdAt, &usedAt)
	if err != nil {
		return nil, err
	}
	if usedBy.Valid {
		inv.UsedBy = &usedBy.String
	}
	if usedAt.Valid {
		v := usedAt.Int64
		inv.UsedAt = &v
	}
	inv.CreatedAt = createdAt.Int64
	return &inv, nil
}

var ErrInvitationNotFound = errors.New("session: invitation not found")

func (s *SQLiteStore) CreateInvitation(ctx context.Context, inv *Invitation, now int64) error {
	return s.writeMu.WithLock(func() error {
		_, err := s.db.ExecContext(ctx, queries["invitations.create"],
			inv.ID, inv.Code, inv.CreatedBy, inv.Role, inv.ExpiresAt, now)
		return err
	})
}

func (s *SQLiteStore) GetInvitationByCode(ctx context.Context, code string) (*Invitation, error) {
	row := s.db.QueryRowContext(ctx, queries["invitations.get_by_code"], code)
	inv, err := scanInvitation(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrInvitationNotFound
	}
	return inv, err
}

func (s *SQLiteStore) MarkInvitationUsed(ctx context.Context, id, usedBy string, now int64) error {
	return s.writeMu.WithLock(func() error {
		res, err := s.db.ExecContext(ctx, queries["invitations.mark_used"], usedBy, now, id)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return ErrInvitationAlreadyUsed
		}
		return nil
	})
}

func (s *SQLiteStore) ListInvitations(ctx context.Context) ([]*Invitation, error) {
	rows, err := s.db.QueryContext(ctx, queries["invitations.list"])
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Invitation
	for rows.Next() {
		inv, err := scanInvitation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, inv)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) DeleteInvitation(ctx context.Context, id string) error {
	return s.writeMu.WithLock(func() error {
		_, err := s.db.ExecContext(ctx, queries["invitations.delete"], id)
		return err
	})
}

var ErrInvitationAlreadyUsed = errors.New("session: invitation already used")
```

- [ ] **Step 5: pg_store.go 镜像**（5 个方法）

- [ ] **Step 6: 运行测试确认通过**

Run: `go test ./internal/session/ -run TestInvitationsStore -count=1 -race`

Expected: PASS

- [ ] **Step 7: 提交**

```bash
git add internal/session/sql/queries/invitations.*.sql internal/session/store.go internal/session/pg_store.go \
        internal/session/invitations_store_test.go
git commit -m "feat(session): invitations store（一次性邀请码 + CAS 防重放）"
```

---

## Phase 2：IdentityProvider + LocalAccountProvider + bcrypt

### Task 2.1: 引入 bcrypt 依赖

**Files:**
- Modify: `go.mod` / `go.sum`

- [ ] **Step 1: 添加依赖**

Run: `go get golang.org/x/crypto/bcrypt`

- [ ] **Step 2: 验证 tidy**

Run: `go mod tidy && go build ./...`

Expected: 编译通过（golang.org/x/crypto 通常已是间接依赖，tidy 后转直接）

- [ ] **Step 3: 提交**

```bash
git add go.mod go.sum
git commit -m "chore: 引入 golang.org/x/crypto/bcrypt（项目首次使用密码哈希）"
```

### Task 2.2: LocalAccountProvider 实现

**Files:**
- Create: `internal/security/local_account_provider.go`

- [ ] **Step 1: 写失败测试**

Create `internal/security/local_account_provider_test.go`:

```go
package security

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// stubUserStore 是最小 UserStore 测试替身。
type stubUserStore struct {
	byUsername map[string]*User
	byID       map[string]*User
}

func (s *stubUserStore) CreateUser(ctx context.Context, u *User, now int64) error {
	s.byUsername[u.Username] = u
	s.byID[u.ID] = u
	return nil
}
func (s *stubUserStore) GetUserByID(ctx context.Context, id string) (*User, error) {
	u, ok := s.byID[id]
	if !ok {
		return nil, ErrUserNotFoundStub
	}
	return u, nil
}
func (s *stubUserStore) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	u, ok := s.byUsername[username]
	if !ok {
		return nil, ErrUserNotFoundStub
	}
	return u, nil
}

var ErrUserNotFoundStub = errNotFound("user not found")

type errNotFound string

func (e errNotFound) Error() string { return string(e) }

const bcryptCost = 12

func TestLocalAccountProvider_AuthenticateSuccess(t *testing.T) {
	t.Parallel()
	store := &stubUserStore{byUsername: map[string]*User{}, byID: map[string]*User{}}
	prov := NewLocalAccountProvider(store, bcryptCost)

	hash, _ := bcryptHash("s3cret", bcryptCost)
	store.byUsername["alice"] = &User{ID: "u-1", Username: "alice", PasswordHash: hash, Role: "user", Status: "active"}

	uid, err := prov.Authenticate(context.Background(), LoginCredentials{Username: "alice", Password: "s3cret"})
	require.NoError(t, err)
	require.Equal(t, "u-1", uid)
}

func TestLocalAccountProvider_AuthenticateWrongPassword(t *testing.T) {
	t.Parallel()
	store := &stubUserStore{byUsername: map[string]*User{}}
	prov := NewLocalAccountProvider(store, bcryptCost)
	hash, _ := bcryptHash("s3cret", bcryptCost)
	store.byUsername["alice"] = &User{ID: "u-1", Username: "alice", PasswordHash: hash, Status: "active"}

	_, err := prov.Authenticate(context.Background(), LoginCredentials{Username: "alice", Password: "wrong"})
	require.ErrorIs(t, err, errInvalidCredentials)
}

func TestLocalAccountProvider_DisabledUserRejected(t *testing.T) {
	t.Parallel()
	store := &stubUserStore{byUsername: map[string]*User{}}
	prov := NewLocalAccountProvider(store, bcryptCost)
	hash, _ := bcryptHash("s3cret", bcryptCost)
	store.byUsername["bob"] = &User{ID: "u-2", Username: "bob", PasswordHash: hash, Status: "disabled"}

	_, err := prov.Authenticate(context.Background(), LoginCredentials{Username: "bob", Password: "s3cret"})
	require.ErrorIs(t, err, errUserDisabled)
}

func TestLocalAccountProvider_EmptyPasswordHashRejected(t *testing.T) {
	t.Parallel()
	store := &stubUserStore{byUsername: map[string]*User{}}
	prov := NewLocalAccountProvider(store, bcryptCost)
	// API-key provision 用户 password_hash=''，禁止账号登录
	store.byUsername["apikey:x"] = &User{ID: "u-3", Username: "apikey:x", PasswordHash: "", Status: "active"}

	_, err := prov.Authenticate(context.Background(), LoginCredentials{Username: "apikey:x", Password: "anything"})
	require.ErrorIs(t, err, errInvalidCredentials)
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/security/ -run TestLocalAccountProvider -count=1`

Expected: 编译失败（NewLocalAccountProvider/bcryptHash/errInvalidCredentials 未定义）

- [ ] **Step 3: 实现 LocalAccountProvider**

Create `internal/security/local_account_provider.go`:

```go
package security

import (
	"context"
	"errors"

	"golang.org/x/crypto/bcrypt"
)

var (
	errInvalidCredentials = &IdentityError{Code: ErrCodeInvalidCredentials}
	errUserDisabled       = &IdentityError{Code: ErrCodeUserDisabled}
)

// ErrUserNotFound is the sentinel a UserStore returns when no row matches.
// (Defined here so tests can reuse; store implementations should return this.)
var ErrUserNotFound = errors.New("security: user not found")

// LocalAccountProvider authenticates against the users table (bcrypt).
type LocalAccountProvider struct {
	store UserStore
	cost  int
}

func NewLocalAccountProvider(store UserStore, bcryptCost int) *LocalAccountProvider {
	return &LocalAccountProvider{store: store, cost: bcryptCost}
}

func (p *LocalAccountProvider) Authenticate(ctx context.Context, creds Credentials) (string, error) {
	lc, ok := creds.(LoginCredentials)
	if !ok {
		return "", errInvalidCredentials
	}
	u, err := p.store.GetUserByUsername(ctx, lc.Username)
	if errors.Is(err, ErrUserNotFound) || u == nil {
		return "", errInvalidCredentials // 用户不存在也返回 INVALID_CREDENTIALS（防用户枚举）
	}
	if err != nil {
		return "", err
	}
	// 空密码哈希 = API-key provision 用户，禁止账号登录通道
	if u.PasswordHash == "" {
		return "", errInvalidCredentials
	}
	if u.Status == "disabled" {
		return "", errUserDisabled
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(lc.Password)); err != nil {
		return "", errInvalidCredentials
	}
	return u.ID, nil
}

func (p *LocalAccountProvider) Lookup(ctx context.Context, userID string) (*User, error) {
	u, err := p.store.GetUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	return u, nil
}

// HashPassword hashes a plaintext password at the provider's cost.
func (p *LocalAccountProvider) HashPassword(plaintext string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(plaintext), p.cost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
```

> 注意 `ErrUserNotFound` 同时定义在 security 与 session 包。session.store 实现返回 session 自己的 `ErrUserNotFound`；security.LocalAccountProvider 用 `errors.Is` 比较 security 的 `ErrUserNotFound`。**为统一**，session 包的 `GetUserByUsername`/`GetUserByID` 应返回 `security.ErrUserNotFound`（删除 Task 1.2 里定义的 `session.ErrUserNotFound`，改用 `security.ErrUserNotFound`）。执行 Task 1.2 时已定义 session 自己的——在此 Task 改为返回 `security.ErrUserNotFound`，保持单一哨兵。

- [ ] **Step 4: 修正 session store 返回 security.ErrUserNotFound**

Modify `internal/session/store.go`：删除 `var ErrUserNotFound = errors.New(...)`，把 `GetUserByID`/`GetUserByUsername` 的 `return nil, ErrUserNotFound` 改为 `return nil, security.ErrUserNotFound`。同步更新 Task 1.2 测试 `require.ErrorIs(t, err, ErrUserNotFound)` → `require.ErrorIs(t, err, security.ErrUserNotFound)`。

- [ ] **Step 5: 运行测试确认通过**

Run: `go test ./internal/security/ ./internal/session/ -run 'TestLocalAccountProvider|TestUsersStore' -count=1 -race`

Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add internal/security/local_account_provider.go internal/security/local_account_provider_test.go \
        internal/session/store.go
git commit -m "feat(security): LocalAccountProvider（bcrypt + 防用户枚举 + 禁用/空哈希拒绝）"
```

---

## Phase 3：认证通道升级（Cookie TTL + login/logout/me + 邀请 + bootstrap）

### Task 3.1: CookieAuth 升级 — 编码真实 user.id + 7 天 TTL + 滑动刷新

**Files:**
- Modify: `internal/security/cookie.go`

- [ ] **Step 1: 写失败测试**

Create `internal/security/cookie_refresh_test.go`:

```go
package security

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCookieAuth_SetAndVerify(t *testing.T) {
	t.Parallel()
	ca, err := NewCookieAuth()
	require.NoError(t, err)

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	require.NoError(t, ca.SetCookie(w, r, "u-real-123"))

	// 用响应的 cookie 构造新请求
	r2 := httptest.NewRequest(http.MethodGet, "/", nil)
	for _, c := range w.Result().Cookies() {
		r2.AddCookie(c)
	}
	uid, ok := ca.Authenticate(r2)
	require.True(t, ok)
	require.Equal(t, "u-real-123", uid)
}

func TestCookieAuth_SlidingRefreshNearExpiry(t *testing.T) {
	t.Parallel()
	ca, err := NewCookieAuth()
	require.NoError(t, err)

	// 模拟一个快过期的 cookie（签发于 6 天前，TTL 7 天，过半应刷新）
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	ca.setCookieWithIssuedAt(w, r, "u-1", time.Now().Add(-6*24*time.Hour))

	r2 := httptest.NewRequest(http.MethodGet, "/", nil)
	for _, c := range w.Result().Cookies() {
		r2.AddCookie(c)
	}
	w2 := httptest.NewRecorder()
	uid, ok := ca.AuthenticateAndMaybeRefresh(w2, r2)
	require.True(t, ok)
	require.Equal(t, "u-1", uid)
	// 过半 TTL 必须重签
	require.NotEmpty(t, w2.Result().Headers.Get("Set-Cookie"), "接近过期必须滑动刷新")
}

func TestCookieAuth_NoRefreshWhenFresh(t *testing.T) {
	t.Parallel()
	ca, _ := NewCookieAuth()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	require.NoError(t, ca.SetCookie(w, r, "u-1"))

	r2 := httptest.NewRequest(http.MethodGet, "/", nil)
	for _, c := range w.Result().Cookies() {
		r2.AddCookie(c)
	}
	w2 := httptest.NewRecorder()
	_, ok := ca.AuthenticateAndMaybeRefresh(w2, r2)
	require.True(t, ok)
	require.Empty(t, w2.Result().Headers.Get("Set-Cookie"), "新鲜 cookie 不应刷新")
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/security/ -run 'TestCookieAuth_SlidingRefreshNearExpiry|TestCookieAuth_NoRefreshWhenFresh' -count=1`

Expected: 编译失败（AuthenticateAndMaybeRefresh/setCookieWithIssuedAt 未定义）

- [ ] **Step 3: 改 cookie.go**

修改 `internal/security/cookie.go`：

(a) 常量改 7 天：

```go
const (
    cookieName  = "webchat_session"
    cookieMaxAge = 7 * 24 * time.Hour // spec 附录 B：7 天
    refreshAfter = cookieMaxAge / 2   // 过半 TTL 滑动刷新（3.5 天）
    hmacKeyLen   = 32
)
```

(b) 新增导出方法 `AuthenticateAndMaybeRefresh` 和测试辅助 `setCookieWithIssuedAt`。`verify` 已返回 timestamp（cookie 格式 `timestamp|userID|HMAC`），暴露它：

```go
// verify returns (userID, issuedAt, ok).
func (c *CookieAuth) verify(encoded string) (string, time.Time, bool) {
    // 解析 base64 → timestamp|userID|hmac，校验 HMAC 与 timestamp 未过期
    // ...（保持现有实现，额外返回 issuedAt）
}

// AuthenticateAndMaybeRefresh authenticates and, if the cookie is past half its
// TTL, reissues a fresh cookie on w (sliding refresh). See spec §8.3.
func (c *CookieAuth) AuthenticateAndMaybeRefresh(w http.ResponseWriter, r *http.Request) (string, bool) {
    cookie, err := r.Cookie(cookieName)
    if err != nil {
        return "", false
    }
    uid, issuedAt, ok := c.verify(cookie.Value)
    if !ok {
        return "", false
    }
    if time.Since(issuedAt) > refreshAfter {
        _ = c.SetCookie(w, r, uid) // 滑动刷新
    }
    return uid, true
}

// setCookieWithIssuedAt is a test helper to mint a cookie with a custom issue time.
func (c *CookieAuth) setCookieWithIssuedAt(w http.ResponseWriter, r *http.Request, userID string, issuedAt time.Time) {
    c.setCookieAt(w, r, userID, issuedAt)
}

// setCookieAt signs userID with the given issue time (extracted from existing sign()).
func (c *CookieAuth) setCookieAt(w http.ResponseWriter, r *http.Request, userID string, issuedAt time.Time) {
    encoded := c.signAt(userID, issuedAt)
    http.SetCookie(w, &http.Cookie{
        Name:     cookieName,
        Value:    encoded,
        Path:     "/",
        HttpOnly: true,
        SameSite: http.SameSiteStrictMode,
        Secure:   isHTTPS(r),
        MaxAge:   int(cookieMaxAge.Seconds()),
    })
}
```

> 重构 `sign` → `signAt(userID, issuedAt)`，`SetCookie` 调用 `signAt(userID, time.Now())`。保持 `Authenticate(r)`（返回 `(string, bool)`）不变，内部调 `verify` 忽略 issuedAt——向后兼容现有调用方（hub.go 等）。

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/security/ -run TestCookieAuth -count=1 -race`

Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/security/cookie.go internal/security/cookie_refresh_test.go
git commit -m "feat(security): CookieAuth 升级 — 7 天 TTL + 滑动刷新（spec §8.3）"
```

### Task 3.2: Authenticator 持有 IdentityProvider

**Files:**
- Modify: `internal/security/auth.go`

- [ ] **Step 1: 改 Authenticator struct + 构造函数**

```go
type Authenticator struct {
    mu            sync.RWMutex
    cfg           *config.SecurityConfig
    validKey      map[string]bool
    dbKeys        map[string]bool
    keyResolver   APIKeyResolver
    devModeLocked bool
    cookieAuth    *CookieAuth
    idp           IdentityProvider // 新：账号登录通道 + 用户 Lookup
}
```

构造函数 / setter 增加：

```go
// SetIdentityProvider wires the account-login identity provider (LocalAccountProvider
// now, OAuthProvider later). Optional: nil disables account-login.
func (a *Authenticator) SetIdentityProvider(idp IdentityProvider) {
    a.mu.Lock()
    defer a.mu.Unlock()
    a.idp = idp
}

// IdentityProvider exposes the wired provider for handlers (e.g. /api/auth/me Lookup).
func (a *Authenticator) IdentityProvider() IdentityProvider {
    a.mu.RLock()
    defer a.mu.RUnlock()
    return a.idp
}
```

- [ ] **Step 2: 确认 AuthenticateRequest cookie 分支已返回真实 users.id**

`AuthenticateRequest`（auth.go:63-100）的 cookie 分支：`uid, ok := a.cookieAuth.Authenticate(r)` 返回的 uid 现在是真实 users.id（因为 SetCookie 在 login 时编码真实 id）。**无需改 AuthenticateRequest 主体**，只升级 webchat/server.go 的签发端（Task 3.6）。

> 关键不变量：cookie 通道返回的 userID 与 API key 通道（resolver → users.id）、账号登录（idp.Authenticate → users.id）三者统一指向 `users.id`。dev 模式 `"anonymous"` 兜底保留（无任何认证配置时）。

- [ ] **Step 3: 验证编译**

Run: `go build ./internal/security/`

- [ ] **Step 4: 提交**

```bash
git add internal/security/auth.go
git commit -m "feat(security): Authenticator 持有 IdentityProvider（账号登录扩展点）"
```

### Task 3.3: auth_handlers.go — login / logout / me / accept-invite

**Files:**
- Create: `internal/gateway/auth_handlers.go`
- Create: `internal/gateway/auth_handlers_test.go`

- [ ] **Step 1: 写失败测试 — login + me + 跨用户隔离**

Create `internal/gateway/auth_handlers_test.go`:

```go
package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hrygo/hotplex/internal/security"
	"github.com/stretchr/testify/require"
)

func TestLoginHandler_SuccessIssuesCookie(t *testing.T) {
	t.Parallel()
	env := newTestAuthEnv(t) // 构造 Authenticator+LocalAccountProvider+store，seed admin
	body := `{"username":"admin","password":"adminpass"}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(body))
	w := httptest.NewRecorder()
	env.handlers.Login(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Result().Headers.Get("Set-Cookie"), "webchat_session=")
}

func TestLoginHandler_WrongPassword_401(t *testing.T) {
	t.Parallel()
	env := newTestAuthEnv(t)
	body := `{"username":"admin","password":"wrong"}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(body))
	w := httptest.NewRecorder()
	env.handlers.Login(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestMeHandler_RequiresAuth(t *testing.T) {
	t.Parallel()
	env := newTestAuthEnv(t)
	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	w := httptest.NewRecorder()
	env.handlers.Me(w, req) // 无 cookie
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAcceptInvite_CreatesUserAndIssuesCookie(t *testing.T) {
	t.Parallel()
	env := newTestAuthEnv(t)
	// admin 预先生成一个邀请码
	code := env.createInvite(t, "user")

	body := `{"code":"` + code + `","username":"newbie","password":"n00bpass"}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/accept-invite", strings.NewReader(body))
	w := httptest.NewRecorder()
	env.handlers.AcceptInvite(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	require.Contains(t, w.Result().Headers.Get("Set-Cookie"), "webchat_session=")

	// newbie 能用账号密码登录
	u, err := env.store.GetUserByUsername(context.Background(), "newbie")
	require.NoError(t, err)
	require.NotEmpty(t, u.ID)
	_ = json // keep import if unused
}
```

> `newTestAuthEnv(t)` 构造一个临时 SQLiteStore + LocalAccountProvider + Authenticator + AuthHandlers，并 seed 一个 admin 账号。放在测试文件里。

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/gateway/ -run 'TestLoginHandler|TestMeHandler|TestAcceptInvite' -count=1`

Expected: 编译失败（AuthHandlers/newTestAuthEnv 未定义）

- [ ] **Step 3: 实现 auth_handlers.go**

Create `internal/gateway/auth_handlers.go`:

```go
package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/hrygo/hotplex/internal/security"
	"github.com/hrygo/hotplex/internal/session"
)

// AppError is the project error envelope. Reuse existing if defined in gateway;
// otherwise this minimal form.
type AppError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeAppError(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": AppError{Code: code, Message: msg}})
}

// AuthHandlers holds the dependencies for authentication endpoints.
type AuthHandlers struct {
	auth       *security.Authenticator
	cookieAuth *security.CookieAuth
	store      session.UserWorkspaceStore // 见下方接口（store 的子集）
	idp        *security.LocalAccountProvider
	now        func() time.Time
}

// NewAuthHandlers constructs auth handlers.
func NewAuthHandlers(auth *security.Authenticator, cookieAuth *security.CookieAuth,
	store session.UserWorkspaceStore, idp *security.LocalAccountProvider) *AuthHandlers {
	return &AuthHandlers{auth: auth, cookieAuth: cookieAuth, store: store, idp: idp, now: time.Now}
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// Login: POST /api/auth/login
func (h *AuthHandlers) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAppError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid body")
		return
	}
	uid, err := h.idp.Authenticate(r.Context(), security.LoginCredentials{Username: req.Username, Password: req.Password})
	if err != nil {
		var ie *security.IdentityError
		if errors.As(err, &ie) {
			switch ie.Code {
			case security.ErrCodeUserDisabled:
				writeAppError(w, http.StatusForbidden, "USER_DISABLED", "user disabled")
				return
			default:
				writeAppError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "invalid credentials")
				return
			}
		}
		writeAppError(w, http.StatusInternalServerError, "INTERNAL", "auth error")
		return
	}
	if err := h.cookieAuth.SetCookie(w, r, uid); err != nil {
		writeAppError(w, http.StatusInternalServerError, "INTERNAL", "cookie error")
		return
	}
	if err := h.store.TouchUserLastLogin(r.Context(), uid, h.now().Unix()); err != nil {
		// 非关键：登录已成功，记录失败但不阻断
		_ = err
	}
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"user_id": uid})
}

// Logout: POST /api/auth/logout
func (h *AuthHandlers) Logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: "webchat_session", Value: "", Path: "/",
		MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode,
	})
	w.WriteHeader(http.StatusOK)
}

// Me: GET /api/auth/me — 返回当前用户信息
func (h *AuthHandlers) Me(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.currentUserID(w, r)
	if !ok {
		return // 401 已写
	}
	idp := h.auth.IdentityProvider()
	if idp == nil {
		writeAppError(w, http.StatusServiceUnavailable, "NO_IDP", "identity provider not configured")
		return
	}
	u, err := idp.Lookup(r.Context(), uid)
	if err != nil {
		writeAppError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "user not found")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"id": u.ID, "username": u.Username, "role": u.Role, "status": u.Status,
	})
}

// currentUserID 从 cookie 解析当前 user.id，失败写 401。
func (h *AuthHandlers) currentUserID(w http.ResponseWriter, r *http.Request) (string, bool) {
	uid, ok := h.cookieAuth.Authenticate(r)
	if !ok {
		writeAppError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "not authenticated")
		return "", false
	}
	return uid, true
}

type acceptInviteRequest struct {
	Code     string `json:"code"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// AcceptInvite: POST /api/auth/accept-invite
func (h *AuthHandlers) AcceptInvite(w http.ResponseWriter, r *http.Request) {
	var req acceptInviteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAppError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid body")
		return
	}
	ctx := r.Context()
	inv, err := h.store.GetInvitationByCode(ctx, req.Code)
	if err != nil {
		writeAppError(w, http.StatusNotFound, "INVITATION_NOT_FOUND", "invitation not found")
		return
	}
	if inv.UsedBy != nil {
		writeAppError(w, http.StatusBadRequest, "INVITATION_USED", "invitation already used")
		return
	}
	if h.now().Unix() > inv.ExpiresAt {
		writeAppError(w, http.StatusBadRequest, "INVITATION_EXPIRED", "invitation expired")
		return
	}
	// 用户名唯一冲突
	if existing, _ := h.store.GetUserByUsername(ctx, req.Username); existing != nil {
		writeAppError(w, http.StatusConflict, "USERNAME_TAKEN", "username taken")
		return
	}
	hash, err := h.idp.HashPassword(req.Password)
	if err != nil {
		writeAppError(w, http.StatusInternalServerError, "INTERNAL", "hash error")
		return
	}
	uid := uuid.NewString()
	u := &security.User{ID: uid, Username: req.Username, PasswordHash: hash, Role: inv.Role, Status: "active"}
	if err := h.store.CreateUser(ctx, u, h.now().Unix()); err != nil {
		writeAppError(w, http.StatusConflict, "USERNAME_TAKEN", "username taken")
		return
	}
	if err := h.store.MarkInvitationUsed(ctx, inv.ID, uid, h.now().Unix()); err != nil {
		writeAppError(w, http.StatusConflict, "INVITATION_USED", "invitation already used")
		return
	}
	_ = h.cookieAuth.SetCookie(w, r, uid)
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"user_id": uid})
}
```

> 需在 `internal/session` 定义 `UserWorkspaceStore` 接口（store 能力的子集，供 gateway 依赖而不耦合全部 Store）：

在 `internal/session/store.go` 末尾追加：

```go
// UserWorkspaceStore is the store capability surface used by gateway auth/workspace handlers.
// SQLiteStore and pgStore both satisfy it.
type UserWorkspaceStore interface {
	security.UserStore
	GetInvitationByCode(ctx context.Context, code string) (*Invitation, error)
	MarkInvitationUsed(ctx context.Context, id, usedBy string, now int64) error
	CreateInvitation(ctx context.Context, inv *Invitation, now int64) error
	ListInvitations(ctx context.Context) ([]*Invitation, error)
	DeleteInvitation(ctx context.Context, id string) error
	TouchUserLastLogin(ctx context.Context, userID string, now int64) error
	CreateWorkspace(ctx context.Context, w *Workspace, now int64) error
	GetWorkspaceByID(ctx context.Context, id string) (*Workspace, error)
	ListWorkspacesByOwner(ctx context.Context, ownerUserID string) ([]*Workspace, error)
	UpdateWorkspace(ctx context.Context, w *Workspace, now int64) error
	DeleteWorkspace(ctx context.Context, id string) error
	CountActiveSessionsInWorkspace(ctx context.Context, workspaceID string) (int, error)
}
```

并实现 `TouchUserLastLogin`（store.go，调 `users.touch_last_login`）：

```go
func (s *SQLiteStore) TouchUserLastLogin(ctx context.Context, userID string, now int64) error {
	return s.writeMu.WithLock(func() error {
		_, err := s.db.ExecContext(ctx, queries["users.touch_last_login"], now, now, userID)
		return err
	})
}
```

pg_store.go 镜像。

- [ ] **Step 4: 写 newTestAuthEnv 辅助**（测试文件内）

```go
type testAuthEnv struct {
	auth     *security.Authenticator
	cookie   *security.CookieAuth
	store    session.UserWorkspaceStore
	handlers *AuthHandlers
}

func newTestAuthEnv(t *testing.T) *testAuthEnv {
	t.Helper()
	store, cleanup := newTestStore(t)
	t.Cleanup(cleanup)
	ca, err := security.NewCookieAuth()
	require.NoError(t, err)
	idp := security.NewLocalAccountProvider(store.(security.UserStore), 4) // 测试用低 cost 加速
	hash, _ := idp.HashPassword("adminpass")
	require.NoError(t, store.CreateUser(context.Background(), &security.User{
		ID: "u-admin", Username: "admin", PasswordHash: hash, Role: "admin", Status: "active",
	}, 1700000000))
	auth := security.NewAuthenticator(nil) // 按现有构造签名调整
	auth.SetCookieAuth(ca)
	auth.SetIdentityProvider(idp)
	h := NewAuthHandlers(auth, ca, store.(session.UserWorkspaceStore), idp)
	return &testAuthEnv{auth: auth, cookie: ca, store: store, handlers: h}
}

func (e *testAuthEnv) createInvite(t *testing.T, role string) string {
	inv := &session.Invitation{ID: "inv-test", Code: "CODE-" + role, CreatedBy: "u-admin", Role: role, ExpiresAt: time.Now().Add(time.Hour).Unix()}
	require.NoError(t, e.store.CreateInvitation(context.Background(), inv, 1700000000))
	return inv.Code
}
```

> `security.NewAuthenticator` / `SetCookieAuth` 签名需对齐现有代码；若现有构造不同，按实际调整。`newTestStore` 复用 session 包测试辅助——但它在 session 包内（小写不可跨包）。**解决**：把 `newTestStore` 提升为导出的 `NewTestStore`（session 包加一个 `export_test.go` 或直接导出），或在 gateway 测试里用 `session.NewSQLiteStore(tempDB)` 自行构造。推荐后者（见 Task 3.3 备注实现 `newTestStore` 等价物）。

- [ ] **Step 5: 运行测试确认通过**

Run: `go test ./internal/gateway/ -run 'TestLoginHandler|TestMeHandler|TestAcceptInvite' -count=1 -race`

Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add internal/gateway/auth_handlers.go internal/gateway/auth_handlers_test.go \
        internal/session/store.go internal/session/pg_store.go
git commit -m "feat(gateway): 认证端点 login/logout/me/accept-invite（spec §8.6 邀请制）"
```

### Task 3.4: admin invitations handler + admin users handler

**Files:**
- Modify: `internal/gateway/auth_handlers.go`（追加 admin 端点）

- [ ] **Step 1: 追加 admin 端点**

```go
type createInvitationRequest struct {
	Role string `json:"role"` // 'user' | 'admin'
	TTL  int    `json:"ttl"`  // 秒；0 = 默认 7 天
}

const defaultInvitationTTL = 7 * 24 * 3600

// AdminCreateInvitation: POST /api/admin/invitations（需 admin 角色）
func (h *AuthHandlers) AdminCreateInvitation(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}
	var req createInvitationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAppError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid body")
		return
	}
	if req.Role != "user" && req.Role != "admin" {
		req.Role = "user"
	}
	ttl := req.TTL
	if ttl <= 0 {
		ttl = defaultInvitationTTL
	}
	uid, _ := h.cookieAuth.Authenticate(r)
	// 邀请码：32 字节密码学随机，base64url
	code := security.GenerateInviteCode()
	inv := &session.Invitation{
		ID: uuid.NewString(), Code: code, CreatedBy: uid,
		Role: req.Role, ExpiresAt: h.now().Unix() + int64(ttl),
	}
	if err := h.store.CreateInvitation(r.Context(), inv, h.now().Unix()); err != nil {
		writeAppError(w, http.StatusInternalServerError, "INTERNAL", "create invitation failed")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"code": code, "role": inv.Role, "expires_at": inv.ExpiresAt})
}

// AdminListInvitations: GET /api/admin/invitations
func (h *AuthHandlers) AdminListInvitations(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}
	invs, err := h.store.ListInvitations(r.Context())
	if err != nil {
		writeAppError(w, http.StatusInternalServerError, "INTERNAL", "list failed")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"invitations": invs})
}

// AdminDeleteInvitation: DELETE /api/admin/invitations/{id}
func (h *AuthHandlers) AdminDeleteInvitation(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}
	id := r.PathValue("id")
	if err := h.store.DeleteInvitation(r.Context(), id); err != nil {
		writeAppError(w, http.StatusInternalServerError, "INTERNAL", "delete failed")
		return
	}
	w.WriteHeader(http.StatusOK)
}

// AdminListUsers: GET /api/admin/users
func (h *AuthHandlers) AdminListUsers(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}
	users, err := h.store.ListUsers(r.Context(), 1000, 0)
	if err != nil {
		writeAppError(w, http.StatusInternalServerError, "INTERNAL", "list failed")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"users": users})
}

type updateUserStatusRequest struct {
	Status string `json:"status"` // 'active' | 'disabled'
}

// AdminUpdateUserStatus: PATCH /api/admin/users/{id}
func (h *AuthHandlers) AdminUpdateUserStatus(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}
	id := r.PathValue("id")
	var req updateUserStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAppError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid body")
		return
	}
	if req.Status != "active" && req.Status != "disabled" {
		writeAppError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid status")
		return
	}
	if err := h.store.UpdateUserStatus(r.Context(), id, req.Status, h.now().Unix()); err != nil {
		writeAppError(w, http.StatusInternalServerError, "INTERNAL", "update failed")
		return
	}
	w.WriteHeader(http.StatusOK)
}

// requireAdmin 校验当前用户是 admin，否则写 403。返回是否继续。
func (h *AuthHandlers) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	uid, ok := h.cookieAuth.Authenticate(r)
	if !ok {
		writeAppError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "not authenticated")
		return false
	}
	idp := h.auth.IdentityProvider()
	if idp == nil {
		writeAppError(w, http.StatusServiceUnavailable, "NO_IDP", "no identity provider")
		return false
	}
	u, err := idp.Lookup(r.Context(), uid)
	if err != nil || u.Status != "active" {
		writeAppError(w, http.StatusForbidden, "USER_DISABLED", "user disabled")
		return false
	}
	if u.Role != "admin" {
		writeAppError(w, http.StatusForbidden, "FORBIDDEN", "admin only")
		return false
	}
	return true
}
```

> 需在 store 加 `ListUsers` / `UpdateUserStatus`（调 `users.list` / `users.update_status`），pg_store 镜像。`security.GenerateInviteCode` 加到 `internal/security`：

```go
// GenerateInviteCode returns a 32-byte cryptographically random invite code (base64url).
func GenerateInviteCode() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
```

- [ ] **Step 2: 加 store 的 ListUsers / UpdateUserStatus**（store.go + pg_store.go）

```go
func (s *SQLiteStore) ListUsers(ctx context.Context, limit, offset int) ([]*security.User, error) {
	rows, err := s.db.QueryContext(ctx, queries["users.list"], limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*security.User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) UpdateUserStatus(ctx context.Context, id, status string, now int64) error {
	return s.writeMu.WithLock(func() error {
		_, err := s.db.ExecContext(ctx, queries["users.update_status"], status, now, id)
		return err
	})
}
```

并把 `ListUsers`/`UpdateUserStatus` 加入 `UserWorkspaceStore` 接口。

- [ ] **Step 3: 运行测试确认编译 + 现有测试不破**

Run: `go test ./internal/gateway/ ./internal/session/ -count=1 -race`

- [ ] **Step 4: 提交**

```bash
git add internal/gateway/auth_handlers.go internal/security/identity_provider.go \
        internal/session/store.go internal/session/pg_store.go
git commit -m "feat(gateway): admin 端点 invitations/users CRUD（spec §8.6 + §11.2）"
```

### Task 3.5: bootstrap admin CLI — `hotplex admin create`

**Files:**
- Create: `cmd/hotplex/admin_cmd.go`
- Modify: `cmd/hotplex/main.go`（注册 admin 子命令）

- [ ] **Step 1: 实现 CLI**

Create `cmd/hotplex/admin_cmd.go`:

```go
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/hrygo/hotplex/internal/security"
	"github.com/hrygo/hotplex/internal/session"
	"github.com/spf13/cobra"
	"golang.org/x/crypto/ssh/terminal"
)

const bcryptCostCLI = 12

func newAdminCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "admin",
		Short: "用户/账号管理（bootstrap admin、密码重置等）",
	}
	create := &cobra.Command{
		Use:   "create",
		Short: "创建首个 admin 账号（仅当 users 表为空或调用者为 admin）",
		RunE:  runAdminCreate,
	}
	create.Flags().String("username", "", "用户名（必填）")
	create.Flags().String("password", "", "密码（省略则交互式提示，不回显）")
	create.Flags().Bool("admin", true, "创建为 admin 角色")
	_ = create.MarkFlagRequired("username")
	cmd.AddCommand(create)
	return cmd
}

func runAdminCreate(cmd *cobra.Command, args []string) error {
	username, _ := cmd.Flags().GetString("username")
	password, _ := cmd.Flags().GetString("password")
	isAdmin, _ := cmd.Flags().GetBool("admin")

	if password == "" {
		fmt.Fprint(cmd.OutOrStdout(), "Enter password: ")
		pwd, err := terminal.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err != nil {
			return fmt.Errorf("read password: %w", err)
		}
		password = string(pwd)
	}
	if len(password) < 8 {
		return fmt.Errorf("password too short (min 8 chars)")
	}

	deps, err := loadGatewayDeps(cmd) // 复用现有依赖加载（store + config）
	if err != nil {
		return err
	}
	defer deps.Close()

	store := deps.SessionStore().(security.UserStore)
	role := "user"
	if isAdmin {
		role = "admin"
	}
	now := time.Now().Unix()
	idp := security.NewLocalAccountProvider(store, bcryptCostCLI)
	hash, err := idp.HashPassword(password)
	if err != nil {
		return fmt.Errorf("hash: %w", err)
	}
	u := &security.User{ID: uuid.NewString(), Username: username, PasswordHash: hash, Role: role, Status: "active"}
	if err := store.CreateUser(context.Background(), u, now); err != nil {
		return fmt.Errorf("create user: %w", err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "created %s user %q (id=%s)\n", role, username, u.ID)
	return nil
}
```

> `loadGatewayDeps` / `deps.SessionStore()` / `deps.Close()` 按现有 main.go 的依赖装配调整（参考 `gateway_run.go` 的 store 构造）。需加 `"os"` import 与 `"io"` 如果用到。

- [ ] **Step 2: 注册到 root cmd**

Modify `cmd/hotplex/main.go`：在 root 命令组装处加 `rootCmd.AddCommand(newAdminCmd())`。

- [ ] **Step 3: 手动验证**

Run: `make build && ./bin/hotplex admin create --username bootstrap --password testpass123 --admin`

Expected: 输出 `created admin user "bootstrap" (id=<uuid>)`，users 表新增一行。

- [ ] **Step 4: 提交**

```bash
git add cmd/hotplex/admin_cmd.go cmd/hotplex/main.go
git commit -m "feat(cli): hotplex admin create — bootstrap admin（spec §8.5）"
```

### Task 3.6: webchat/server.go 移除固定 webchat_user cookie

**Files:**
- Modify: `internal/webchat/server.go:75-80`

- [ ] **Step 1: 移除自动签发固定身份**

把 `internal/webchat/server.go:75-80` 的：

```go
if cookieAuth != nil {
    // TODO(security): support real user identity via login/OAuth.
    // Currently all webchat visitors share "webchat_user" identity.
    _ = cookieAuth.SetCookie(w, r, "webchat_user")
}
```

改为（不再自动签发；webchat 生产模式访问需先经 /api/auth/login 拿 cookie）：

```go
// WebChat 不再自动签发固定 cookie。生产模式下未登录访问由 /api/auth/login 引导；
// dev 模式（无认证配置）由 Authenticator 的 anonymous 兜底处理。
// 登录 UI 归 spec ⑥；本 spec 期间 webchat 生产前端断开（见 spec §8.4）。
```

- [ ] **Step 2: 验证编译 + 现有 webchat 测试**

Run: `go build ./internal/webchat/ && go test ./internal/webchat/ -count=1`

> 预期：若现有 webchat 测试依赖自动 cookie，会失败。这些测试需更新为显式 login 或走 dev 模式（anonymous）。逐一修正（保留 dev 模式匿名路径）。

- [ ] **Step 3: 提交**

```bash
git add internal/webchat/server.go
git commit -m "refactor(webchat): 移除固定 webchat_user cookie 自动签发（spec §8.4 过渡状态）"
```

---

## Phase 4：workspace CRUD API

### Task 4.1: workspace_handlers.go

**Files:**
- Create: `internal/gateway/workspace_handlers.go`
- Create: `internal/gateway/workspace_handlers_test.go`

- [ ] **Step 1: 写失败测试**

Create `internal/gateway/workspace_handlers_test.go`:

```go
package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hrygo/hotplex/internal/session"
	"github.com/stretchr/testify/require"
)

func TestWorkspaceCRUD_CreateListGetUpdateDelete(t *testing.T) {
	t.Parallel()
	env := newTestAuthEnv(t)
	// 以 admin 身份登录拿 cookie
	cookie := env.loginAs(t, "admin", "adminpass")

	// 创建
	body := `{"name":"proj","work_dir":"/tmp/proj"}`
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces", bytes.NewBufferString(body))
	req.Header.Set("Cookie", cookie)
	w := httptest.NewRecorder()
	env.wsHandlers.Create(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var created session.Workspace
	require.NoError(t, json.NewDecoder(w.Body).Decode(&created))
	require.NotEmpty(t, created.ID)
	require.Equal(t, "/tmp/proj", created.WorkDir)

	// 列出
	req2 := httptest.NewRequest(http.MethodGet, "/api/workspaces", nil)
	req2.Header.Set("Cookie", cookie)
	w2 := httptest.NewRecorder()
	env.wsHandlers.List(w2, req2)
	require.Equal(t, http.StatusOK, w2.Code)

	// 更新 name（work_dir 不可变）
	upd := `{"name":"proj-renamed","work_dir":"/tmp/other"}`
	req3 := httptest.NewRequest(http.MethodPatch, "/api/workspaces/"+created.ID, bytes.NewBufferString(upd))
	req3.SetPathValue("id", created.ID)
	req3.Header.Set("Cookie", cookie)
	w3 := httptest.NewRecorder()
	env.wsHandlers.Update(w3, req3)
	require.Equal(t, http.StatusBadRequest, w3.Code, "work_dir 不可变必须拒绝")
}

func TestWorkspace_Isolation_AcrossUsers(t *testing.T) {
	t.Parallel()
	env := newTestAuthEnv(t)
	// user A 创建 workspace
	cookieA := env.createUserAndLogin(t, "alice", "alicepass1")
	body := `{"name":"alice-proj","work_dir":"/tmp/alice"}`
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces", bytes.NewBufferString(body))
	req.Header.Set("Cookie", cookieA)
	w := httptest.NewRecorder()
	env.wsHandlers.Create(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// user B 列出，不应看到 alice 的 workspace
	cookieB := env.createUserAndLogin(t, "bob", "bobpass1234")
	req2 := httptest.NewRequest(http.MethodGet, "/api/workspaces", nil)
	req2.Header.Set("Cookie", cookieB)
	w2 := httptest.NewRecorder()
	env.wsHandlers.List(w2, req2)
	var resp struct {
		Workspaces []*session.Workspace `json:"workspaces"`
	}
	require.NoError(t, json.NewDecoder(w2.Body).Decode(&resp))
	require.Empty(t, resp.Workspaces, "B 不应看到 A 的 workspace")
	_ = context.Background
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/gateway/ -run 'TestWorkspace' -count=1`

Expected: 编译失败

- [ ] **Step 3: 实现 workspace_handlers.go**

Create `internal/gateway/workspace_handlers.go`:

```go
package gateway

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/security"
	"github.com/hrygo/hotplex/internal/session"
)

type WorkspaceHandlers struct {
	store session.UserWorkspaceStore
	ca    *security.CookieAuth
	auth  *security.Authenticator
	cfg   *config.Config
}

func NewWorkspaceHandlers(store session.UserWorkspaceStore, ca *security.CookieAuth,
	auth *security.Authenticator, cfg *config.Config) *WorkspaceHandlers {
	return &WorkspaceHandlers{store: store, ca: ca, auth: auth, cfg: cfg}
}

func (h *WorkspaceHandlers) currentUser(r *http.Request) (string, bool) {
	uid, ok := h.ca.Authenticate(r)
	return uid, ok
}

type createWorkspaceRequest struct {
	Name    string `json:"name"`
	WorkDir string `json:"work_dir"`
}

// Create: POST /api/workspaces
func (h *WorkspaceHandlers) Create(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.currentUser(r)
	if !ok {
		writeAppError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "not authenticated")
		return
	}
	var req createWorkspaceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAppError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid body")
		return
	}
	if req.Name == "" || req.WorkDir == "" {
		writeAppError(w, http.StatusBadRequest, "BAD_REQUEST", "name and work_dir required")
		return
	}
	// 安全双校验（与 SwitchWorkDir 同标准，见 .agents/rules/security.md）
	abs, err := config.ExpandAndAbs(req.WorkDir)
	if err != nil {
		writeAppError(w, http.StatusBadRequest, "INVALID_WORK_DIR", err.Error())
		return
	}
	if err := security.ValidateWorkDir(abs, h.cfg); err != nil {
		writeAppError(w, http.StatusForbidden, "WORK_DIR_FORBIDDEN", err.Error())
		return
	}
	ws := &session.Workspace{
		ID: uuid.NewString(), OwnerUserID: uid, Name: req.Name, WorkDir: abs, Status: "active",
	}
	if err := h.store.CreateWorkspace(r.Context(), ws, nowUnix()); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			writeAppError(w, http.StatusConflict, "WORK_DIR_TAKEN", "work_dir already used by you")
			return
		}
		writeAppError(w, http.StatusInternalServerError, "INTERNAL", "create failed")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(ws)
}

// List: GET /api/workspaces
func (h *WorkspaceHandlers) List(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.currentUser(r)
	if !ok {
		writeAppError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "not authenticated")
		return
	}
	wss, err := h.store.ListWorkspacesByOwner(r.Context(), uid)
	if err != nil {
		writeAppError(w, http.StatusInternalServerError, "INTERNAL", "list failed")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"workspaces": wss})
}

// Get: GET /api/workspaces/{id}
func (h *WorkspaceHandlers) Get(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.currentUser(r)
	if !ok {
		writeAppError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "not authenticated")
		return
	}
	ws, err := h.store.GetWorkspaceByID(r.Context(), r.PathValue("id"))
	if err != nil {
		writeAppError(w, http.StatusNotFound, "WORKSPACE_NOT_FOUND", "not found")
		return
	}
	if ws.OwnerUserID != uid && !h.isAdmin(r) {
		writeAppError(w, http.StatusForbidden, "WORKSPACE_FORBIDDEN", "not your workspace")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(ws)
}

type updateWorkspaceRequest struct {
	Name                 string `json:"name"`
	AgentConfigOverrides string `json:"agent_config_overrides"` // JSON string（spec ② 填充，本 spec 接受但忽略）
	WorkerPreference     string `json:"worker_preference"`     // spec ③ 填充
	WorkDir              string `json:"work_dir"`              // 必须拒绝（不可变）
}

// Update: PATCH /api/workspaces/{id}
func (h *WorkspaceHandlers) Update(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.currentUser(r)
	if !ok {
		writeAppError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "not authenticated")
		return
	}
	var req updateWorkspaceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAppError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid body")
		return
	}
	if req.WorkDir != "" {
		writeAppError(w, http.StatusBadRequest, "WORK_DIR_IMMUTABLE", "work_dir is immutable")
		return
	}
	ws, err := h.store.GetWorkspaceByID(r.Context(), r.PathValue("id"))
	if err != nil {
		writeAppError(w, http.StatusNotFound, "WORKSPACE_NOT_FOUND", "not found")
		return
	}
	if ws.OwnerUserID != uid && !h.isAdmin(r) {
		writeAppError(w, http.StatusForbidden, "WORKSPACE_FORBIDDEN", "not your workspace")
		return
	}
	if req.Name != "" {
		ws.Name = req.Name
	}
	if req.AgentConfigOverrides != "" {
		ws.AgentConfigOverrides = req.AgentConfigOverrides
	}
	if req.WorkerPreference != "" {
		ws.WorkerPreference = req.WorkerPreference
	}
	if err := h.store.UpdateWorkspace(r.Context(), ws, nowUnix()); err != nil {
		writeAppError(w, http.StatusInternalServerError, "INTERNAL", "update failed")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(ws)
}

// Delete: DELETE /api/workspaces/{id}（校验无活跃会话）
func (h *WorkspaceHandlers) Delete(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.currentUser(r)
	if !ok {
		writeAppError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "not authenticated")
		return
	}
	ws, err := h.store.GetWorkspaceByID(r.Context(), r.PathValue("id"))
	if err != nil {
		writeAppError(w, http.StatusNotFound, "WORKSPACE_NOT_FOUND", "not found")
		return
	}
	if ws.OwnerUserID != uid && !h.isAdmin(r) {
		writeAppError(w, http.StatusForbidden, "WORKSPACE_FORBIDDEN", "not your workspace")
		return
	}
	n, err := h.store.CountActiveSessionsInWorkspace(r.Context(), ws.ID)
	if err != nil {
		writeAppError(w, http.StatusInternalServerError, "INTERNAL", "count failed")
		return
	}
	if n > 0 {
		writeAppError(w, http.StatusConflict, "WORKSPACE_NOT_EMPTY", "workspace has active sessions")
		return
	}
	if err := h.store.DeleteWorkspace(r.Context(), ws.ID); err != nil {
		writeAppError(w, http.StatusInternalServerError, "INTERNAL", "delete failed")
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *WorkspaceHandlers) isAdmin(r *http.Request) bool {
	idp := h.auth.IdentityProvider()
	if idp == nil {
		return false
	}
	uid, ok := h.ca.Authenticate(r)
	if !ok {
		return false
	}
	u, err := idp.Lookup(r.Context(), uid)
	return err == nil && u.Role == "admin"
}

func nowUnix() int64 { return timeNow().Unix() }
```

> `timeNow()` 用可注入的时间函数（便于测试），生产为 `time.Now`；`config.ExpandAndAbs` / `security.ValidateWorkDir` 是现有函数（确认签名；若 `ValidateWorkDir` 签名不同，按实际调整）。

- [ ] **Step 4: 在 newTestAuthEnv 补 wsHandlers 构造 + loginAs/createUserAndLogin 辅助**

```go
// 在 testAuthEnv 加字段 wsHandlers *WorkspaceHandlers，newTestAuthEnv 末尾：
env.wsHandlers = NewWorkspaceHandlers(store.(session.UserWorkspaceStore), ca, auth, testConfig())

func (e *testAuthEnv) loginAs(t *testing.T, user, pass string) string {
	body := `{"username":"` + user + `","password":"` + pass + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	e.handlers.Login(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	return w.Result().Headers.Get("Set-Cookie")
}

func (e *testAuthEnv) createUserAndLogin(t *testing.T, name, pass string) string {
	hash, _ := e.handlers.idp.HashPassword(pass)
	require.NoError(t, e.store.CreateUser(context.Background(), &security.User{
		ID: uuid.NewString(), Username: name, PasswordHash: hash, Role: "user", Status: "active",
	}, 1700000000))
	return e.loginAs(t, name, pass)
}
```

- [ ] **Step 5: 运行测试确认通过**

Run: `go test ./internal/gateway/ -run 'TestWorkspace' -count=1 -race`

Expected: PASS（含跨用户隔离断言）

- [ ] **Step 6: 提交**

```bash
git add internal/gateway/workspace_handlers.go internal/gateway/workspace_handlers_test.go \
        internal/gateway/auth_handlers_test.go
git commit -m "feat(gateway): workspace CRUD + work_dir 不可变 + 跨用户隔离（spec §9.1）"
```

---

## Phase 5：session key 改造 + CreateSession 打通

### Task 5.1: DeriveSessionKey 加 workspaceID 参数

**Files:**
- Modify: `internal/session/key.go:25-30`
- Modify: 所有 `DeriveSessionKey` 调用点

- [ ] **Step 1: 写失败测试**

Create `internal/session/key_workspace_test.go`:

```go
package session

import (
	"testing"

	"github.com/hrygo/hotplex/internal/worker"
	"github.com/stretchr/testify/require"
)

func TestDeriveSessionKey_WorkspaceIDParticipatesInHash(t *testing.T) {
	t.Parallel()
	k1 := DeriveSessionKey("u1", worker.TypeClaudeCode, "client-1", "ws-1", "/tmp/p")
	k2 := DeriveSessionKey("u1", worker.TypeClaudeCode, "client-1", "ws-2", "/tmp/p")
	require.NotEqual(t, k1, k2, "不同 workspace_id 必须产生不同 session key")
}

func TestDeriveSessionKey_EmptyWorkspaceIDBackwardCompat(t *testing.T) {
	t.Parallel()
	// 空 workspaceID 不参与 hash（向后兼容未来非 webchat 调用者）
	k1 := DeriveSessionKey("u1", worker.TypeClaudeCode, "c1", "", "/tmp/p")
	k2 := DeriveSessionKey("u1", worker.TypeClaudeCode, "c1", "", "/tmp/p")
	require.Equal(t, k1, k2)
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/session/ -run TestDeriveSessionKey_WorkspaceID -count=1`

Expected: 编译失败（参数个数不对）

- [ ] **Step 3: 改 DeriveSessionKey 签名**

Modify `internal/session/key.go:25-30`:

```go
// DeriveSessionKey generates a deterministic server-side session ID using UUIDv5.
// Same (ownerID, workerType, clientKey, workspaceID, workDir) always maps to the same session.
// workspaceID is the WebChat multitenancy anchor (spec §7 方案3); empty string excludes it
// from the hash for backward compatibility with non-webchat callers.
func DeriveSessionKey(ownerID string, wt worker.WorkerType, clientKey, workspaceID, workDir string) string {
	name := ownerID + "|" + string(wt) + "|" + clientKey + "|" + workspaceID + "|" + workDir
	id := uuid.NewHash(sha1.New(), hotplexNamespace, []byte(name), 5)
	return id.String()
}
```

- [ ] **Step 4: 修正所有调用点**

Run: `grep -rn 'DeriveSessionKey(' --include='*.go' internal/ cmd/`

为每个调用点补 `workspaceID` 参数：
- `internal/gateway/api.go` CreateSession（Task 5.2 一并改，传 workspace.id）
- 其他内部调用（若有）：非 webchat 路径传空串 `""`，webchat 路径传 workspace.id。

> 关键：Slack/Feishu 用 `DerivePlatformSessionKey`（**不变**），cron 用 `DeriveCronSessionKey`（**不变**）。只有 webchat 路径的 `DeriveSessionKey` 调用受影响。

- [ ] **Step 5: 运行测试确认通过**

Run: `go test ./internal/session/ -run TestDeriveSessionKey -count=1 -race && go build ./...`

Expected: PASS + 全项目编译通过

- [ ] **Step 6: 提交**

```bash
git add internal/session/key.go internal/session/key_workspace_test.go
git commit -m "feat(session): DeriveSessionKey 加 workspaceID（spec §7 方案3）"
```

### Task 5.2: CreateSession 改造 — workspace_id 入参 + 归属校验

**Files:**
- Modify: `internal/gateway/api.go:151-235`（CreateSession handler）
- Modify: `internal/worker/worker.go:94-107`（SessionStartParams 加 WorkspaceID，可选）

- [ ] **Step 1: 写失败测试 — CreateSession 绑定 workspace**

Create `internal/gateway/api_workspace_session_test.go`:

```go
package gateway

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCreateSession_RequiresWorkspaceID(t *testing.T) {
	t.Parallel()
	env := newTestAuthEnv(t)
	cookie := env.loginAs(t, "admin", "adminpass")
	// 先建 workspace
	ws := env.createWorkspace(t, cookie, "proj", "/tmp/proj")

	// 不带 workspace_id 应 400
	req := httptest.NewRequest(http.MethodPost, "/api/sessions", bytes.NewBufferString(`{"client_session_id":"c1"}`))
	req.Header.Set("Cookie", cookie)
	w := httptest.NewRecorder()
	env.gatewayAPI.CreateSession(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateSession_WorkspaceForbiddenForOtherUser(t *testing.T) {
	t.Parallel()
	env := newTestAuthEnv(t)
	cookieA := env.createUserAndLogin(t, "alice", "alicepass1")
	ws := env.createWorkspace(t, cookieA, "alice-proj", "/tmp/alice")

	// bob 用 alice 的 workspace_id 应 403 WORKSPACE_FORBIDDEN
	cookieB := env.createUserAndLogin(t, "bob", "bobpass1234")
	body := `{"workspace_id":"` + ws.ID + `","client_session_id":"c1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/sessions", bytes.NewBufferString(body))
	req.Header.Set("Cookie", cookieB)
	w := httptest.NewRecorder()
	env.gatewayAPI.CreateSession(w, req)
	require.Equal(t, http.StatusForbidden, w.Code)
	require.Contains(t, w.Body.String(), "WORKSPACE_FORBIDDEN")
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/gateway/ -run TestCreateSession -count=1`

Expected: 编译失败（gatewayAPI/env.gatewayAPI 未在 env 暴露）

- [ ] **Step 3: 改 CreateSession handler**

Modify `internal/gateway/api.go` 的 `CreateSession`（行 151-235）。核心改动：

```go
func (a *GatewayAPI) CreateSession(w http.ResponseWriter, r *http.Request) {
	userID, botID := a.auth.AuthenticateRequest(r) // 现有签名

	var req struct {
		WorkspaceID     string `json:"workspace_id"`
		ClientSessionID string `json:"client_session_id"`
		WorkerType      string `json:"worker_type"`
	}
	// webchat 路径用 JSON body；保留对 query param 的兼容（client_session_id）。
	// 解析 body（若 Content-Type=JSON）否则回退 query（现有行为）
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.ClientSessionID == "" {
		req.ClientSessionID = r.URL.Query().Get("client_session_id")
	}
	if req.WorkspaceID == "" {
		writeAppError(w, http.StatusBadRequest, "BAD_REQUEST", "workspace_id required")
		return
	}

	// 归属校验
	ws, err := a.store.GetWorkspaceByID(r.Context(), req.WorkspaceID)
	if err != nil {
		writeAppError(w, http.StatusNotFound, "WORKSPACE_NOT_FOUND", "workspace not found")
		return
	}
	if ws.OwnerUserID != userID && !a.isAdmin(r, userID) {
		writeAppError(w, http.StatusForbidden, "WORKSPACE_FORBIDDEN", "not your workspace")
		return
	}

	// work_dir 从 workspace 取（不可变）
	workDir := ws.WorkDir

	// worker_type: body > workspace.worker_preference > 配置 fallback
	wt := resolveWorkerType(req.WorkerType, ws.WorkerPreference, a.cfg)

	// session key 方案3
	id := session.DeriveSessionKey(userID, wt, req.ClientSessionID, ws.ID, workDir)

	// 幂等：已存在且非 deleted 直接返回（现有逻辑保留）
	...（现有 Get + 返回逻辑）

	// 启动 session，带 workspace_id
	startParams := worker.SessionStartParams{
		ID: id, UserID: userID, WorkerType: wt, WorkDir: workDir,
		Platform: platformWebChat, ClientKey: req.ClientSessionID,
		WorkspaceID: ws.ID, // 新
	}
	info, err := a.bridge.StartSession(ctx, startParams)
	...
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"session_id": id})
}
```

> 关键点：
> 1. `workspace_id` 必填（webchat 轨）；现有 query-param `work_dir` 入参废弃。
> 2. work_dir 从 workspace 取，不再从请求读。
> 3. `resolveWorkerType` fallback：`body > ws.WorkerPreference > 配置默认（claude_code）`。spec ③ 填充 preference，本 spec 占位。
> 4. `SessionStartParams` 加 `WorkspaceID string` 字段，bridge 创建 SessionInfo 时写入（见 Step 4）。

- [ ] **Step 4: SessionStartParams + bridge 写入 workspace_id**

Modify `internal/worker/worker.go:94-107` SessionStartParams 加：

```go
type SessionStartParams struct {
	...（现有字段）
	WorkspaceID string // WebChat 多租户（spec ①）；平台/cron 会话为空
}
```

Modify `internal/gateway/bridge.go`（或 bridge_worker.go）StartSession 内构造 SessionInfo 处，把 `params.WorkspaceID` 写入 `info.WorkspaceID`（传给 `Manager.CreateWithBot` / Upsert）。具体定位：搜索 `CreateWithBot` 调用，补传 workspaceID。

> Manager.CreateWithBot 当前签名无 workspaceID（manager.go:252-297）。**选项**：在 CreateWithBot 加 workspaceID 参数，或 Create 后设 info.WorkspaceID 再 Upsert。推荐前者（更干净）——改 CreateWithBot 签名加 `workspaceID string`，内部赋给 info。所有现有调用点（Slack/Feishu/cron）传空串 `""`。

- [ ] **Step 5: 在 testAuthEnv 暴露 gatewayAPI + createWorkspace 辅助**

```go
// testAuthEnv 加 gatewayAPI *GatewayAPI；newTestAuthEnv 构造它（用 nil bridge 或 mock）。
// createWorkspace: 调 wsHandlers.Create 拿回 *session.Workspace。
func (e *testAuthEnv) createWorkspace(t *testing.T, cookie, name, dir string) *session.Workspace {
	body := `{"name":"` + name + `","work_dir":"` + dir + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces", bytes.NewBufferString(body))
	req.Header.Set("Cookie", cookie)
	w := httptest.NewRecorder()
	e.wsHandlers.Create(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var ws session.Workspace
	require.NoError(t, json.NewDecoder(w.Body).Decode(&ws))
	return &ws
}
```

> CreateSession 测试需 mock `bridge.StartSession`（避免真启 worker）。用现有 GatewayAPI 的 bridge 接口注入 fake。若 GatewayAPI 构造复杂，最小化：让测试聚焦"workspace_id 校验 + key 派生"路径，在 StartSession 之前就返回 403/404/400（这些不需要真 bridge）。

- [ ] **Step 6: 运行测试确认通过**

Run: `go test ./internal/gateway/ -run TestCreateSession -count=1 -race`

Expected: PASS

- [ ] **Step 7: 提交**

```bash
git add internal/gateway/api.go internal/gateway/api_workspace_session_test.go \
        internal/gateway/auth_handlers_test.go internal/worker/worker.go \
        internal/session/manager.go internal/gateway/bridge*.go
git commit -m "feat(gateway): CreateSession 绑定 workspace（归属校验 + work_dir 取自 workspace + key 方案3）"
```

---

## Phase 6：会话隔离校验

### Task 6.1: ListSessions 按 workspace 归属过滤

**Files:**
- Modify: `internal/gateway/api.go`（ListSessions）
- Modify: `internal/session/sql/queries/store.list_sessions.sql`（按 workspace_id 过滤）
- Modify: `internal/session/store.go` / `pg_store.go`（List 签名加 workspace 维度）

- [ ] **Step 1: 写失败测试**

Create `internal/gateway/session_isolation_test.go`:

```go
package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestListSessions_UserOnlySeesOwnWorkspaceSessions(t *testing.T) {
	t.Parallel()
	env := newTestAuthEnv(t)
	cookieA := env.createUserAndLogin(t, "alice", "alicepass1")
	cookieB := env.createUserAndLogin(t, "bob", "bobpass1234")
	wsA := env.createWorkspace(t, cookieA, "a-proj", "/tmp/a")
	wsB := env.createWorkspace(t, cookieB, "b-proj", "/tmp/b")

	// alice 在 wsA 建会话，bob 在 wsB 建会话
	env.createSessionIn(t, cookieA, wsA.ID)
	env.createSessionIn(t, cookieB, wsB.ID)

	// alice 列会话，只看到 wsA 的
	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	req.Header.Set("Cookie", cookieA)
	w := httptest.NewRecorder()
	env.gatewayAPI.ListSessions(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	// 断言列表只含 alice 的 session（具体数量取决于 seed，至少不含 bob 的）
	sessions := env.parseSessionList(t, w.Body.String())
	for _, s := range sessions {
		require.Equal(t, wsA.ID, s.WorkspaceID, "alice 不应看到 bob workspace 的会话")
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/gateway/ -run TestListSessions_UserOnlySeesOwnWorkspaceSessions -count=1`

Expected: 失败（ListSessions 尚未按 workspace 归属过滤，或测试辅助未实现）

- [ ] **Step 3: 改 ListSessions handler**

Modify `internal/gateway/api.go` 的 `ListSessions`：

```go
func (a *GatewayAPI) ListSessions(w http.ResponseWriter, r *http.Request) {
	userID, _ := a.auth.AuthenticateRequest(r)
	// 列出当前用户作为 owner 的会话；workspace_id 可选过滤
	workspaceFilter := r.URL.Query().Get("workspace_id")
	if workspaceFilter != "" {
		// 归属校验：filter 的 workspace 必须属于当前用户
		ws, err := a.store.GetWorkspaceByID(r.Context(), workspaceFilter)
		if err != nil || ws.OwnerUserID != userID {
			writeAppError(w, http.StatusForbidden, "WORKSPACE_FORBIDDEN", "not your workspace")
			return
		}
	}
	sessions, err := a.sm.ListForUser(r.Context(), userID, workspaceFilter) // 新方法
	if err != nil {
		writeAppError(w, http.StatusInternalServerError, "INTERNAL", "list failed")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"sessions": sessions})
}
```

> 现有 `Store.List(ctx, userID, platform, limit, offset)` 按 userID 过滤。需确认 List 的 SELECT 是否已按 `owner_id`/`user_id` 过滤——若已有，加 workspace_id 可选条件即可。

- [ ] **Step 4: 改 store List 加 workspace_id 可选过滤**

Modify `internal/session/sql/queries/store.list_sessions.sql`：增加 workspace_id 过滤条件。由于可选过滤，用动态构造（Go 侧拼 SQL）或固定 `AND (workspace_id = ? OR ? = '')`：

```sql
SELECT id, user_id, owner_id, worker_session_id, worker_type, state, bot_id, bot_name,
       platform, platform_key_json, work_dir, title, created_at, updated_at, expires_at,
       idle_expires_at, context_json, source, client_key, workspace_id
FROM sessions
WHERE state != 'deleted'
  AND (owner_id = ? OR user_id = ?)
  AND (? = '' OR workspace_id = ?)
ORDER BY created_at DESC
LIMIT ? OFFSET ?
```

store List 签名加 `workspaceID string` 参数，传两次（占位符重复）。`Manager.ListForUser` 封装。

- [ ] **Step 5: 运行测试确认通过**

Run: `go test ./internal/gateway/ -run TestListSessions -count=1 -race`

Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add internal/gateway/api.go internal/gateway/session_isolation_test.go \
        internal/session/manager.go internal/session/store.go internal/session/pg_store.go \
        internal/session/sql/queries/store.list_sessions.sql
git commit -m "feat(gateway): ListSessions 按 workspace 归属过滤（spec §9.3 隔离）"
```

### Task 6.2: 单 session 操作的 workspace 归属二次校验

**Files:**
- Modify: `internal/session/manager.go`（ValidateOwnership 扩展）
- Modify: `internal/gateway/api.go`（GetSession/DeleteSession/history/events/cd 加归属校验）

- [ ] **Step 1: 扩展 ValidateOwnership**

Modify `internal/session/manager.go:913-938`，在现有 UserID 比较基础上，webchat 会话（workspace_id 非空）校验 workspace 归属：

```go
// ValidateOwnership checks that userID may access sessionID. For webchat sessions
// (workspace_id set), additionally verifies the workspace belongs to userID.
// adminUserID non-empty bypasses (admin). See spec §9.3.
func (m *Manager) ValidateOwnership(ctx context.Context, sessionID, userID, adminUserID string) error {
	si, err := m.Get(ctx, sessionID)
	if err != nil {
		return err
	}
	if adminUserID != "" {
		return nil // admin bypass
	}
	if si.UserID != userID && si.OwnerID != userID {
		return ErrOwnershipMismatch
	}
	// webchat 会话：workspace 归属二次校验
	if si.WorkspaceID != "" {
		ws, err := m.store.GetWorkspaceByID(ctx, si.WorkspaceID)
		if err != nil {
			return ErrOwnershipMismatch
		}
		if ws.OwnerUserID != userID {
			return ErrOwnershipMismatch
		}
	}
	return nil
}
```

> 需给 Manager 加 store 字段访问（Manager 已有 `store Store` 字段，但 `GetWorkspaceByID` 不在 `Store` 接口里）。把 workspace 查询能力加进 Manager 依赖的 store 接口，或让 ValidateOwnership 调用注入的 workspace 查询函数。最小改动：给 `Manager` 加一个可选 `workspaceLookup func(ctx, id) (ownerID string, err error)` 字段，由 gateway 在组装时注入。

- [ ] **Step 2: 在 gateway 单 session 端点加归属校验**

对 `GetSession`/`DeleteSession`/`history`/`events`/`cd`（api.go 现有端点），在执行前调 `sm.ValidateOwnership(ctx, id, userID, adminID)`，失败返回 `WORKSPACE_FORBIDDEN`/403。

```go
// 各端点开头（以 GetSession 为例）
userID, _ := a.auth.AuthenticateRequest(r)
id := r.PathValue("id")
if err := a.sm.ValidateOwnership(r.Context(), id, userID, adminID(r, userID)); err != nil {
    writeAppError(w, http.StatusForbidden, "WORKSPACE_FORBIDDEN", "not your session")
    return
}
// ...现有逻辑
```

- [ ] **Step 3: 写测试 — 跨用户访问 session 返回 403**

追加到 `session_isolation_test.go`：

```go
func TestGetSession_CrossUserForbidden(t *testing.T) {
	t.Parallel()
	env := newTestAuthEnv(t)
	cookieA := env.createUserAndLogin(t, "alice", "alicepass1")
	cookieB := env.createUserAndLogin(t, "bob", "bobpass1234")
	wsA := env.createWorkspace(t, cookieA, "a-proj", "/tmp/a")
	sessA := env.createSessionIn(t, cookieA, wsA.ID)

	// bob 访问 alice 的 session
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/"+sessA, nil)
	req.SetPathValue("id", sessA)
	req.Header.Set("Cookie", cookieB)
	w := httptest.NewRecorder()
	env.gatewayAPI.GetSession(w, req)
	require.Equal(t, http.StatusForbidden, w.Code)
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/gateway/ -run 'TestGetSession_CrossUser|TestListSessions' -count=1 -race`

Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/session/manager.go internal/gateway/api.go internal/gateway/session_isolation_test.go
git commit -m "feat(session): ValidateOwnership 扩展 workspace 归属二次校验（spec §9.3）"
```

### Task 6.3: SwitchWorkDir 语义重映射（work_dir → workspace）

**Files:**
- Modify: `internal/gateway/bridge.go`（handleSwitchWorkDir）

- [ ] **Step 1: 改 handleSwitchWorkDir**

现有 `POST /api/sessions/{id}/cd`（`bridge.go handleSwitchWorkDir`）切 work_dir。workspace 模型下：切换 work_dir = 切到该 work_dir 对应的 workspace（per-user 1:1，查找无则自动创建）。

Modify `handleSwitchWorkDir` 流程：

```go
// 1. 安全双校验（不变）
abs, err := config.ExpandAndAbs(newWorkDir)
if err != nil { ... }
if err := security.ValidateWorkDir(abs, cfg); err != nil { ... }

// 2. 查找/创建 owner+workDir 的 workspace（webchat 轨）
ws, err := store.GetWorkspaceByOwnerAndWorkDir(ctx, userID, abs)
if errors.Is(err, session.ErrWorkspaceNotFound) {
    ws = &session.Workspace{ID: uuid.NewString(), OwnerUserID: userID, Name: filepath.Base(abs), WorkDir: abs, Status: "active"}
    if err := store.CreateWorkspace(ctx, ws, now); err != nil { ... }
} else if err != nil { ... }

// 3. 在新 workspace 下 resume/创建会话（用 workspace.id 派生 key）
// 复用现有 DeriveSessionKey + GetOrCreate 逻辑，传 ws.ID
```

> 平台会话（Slack/Feishu，无 workspace）维持原 work_dir 切换路径——判断 `si.WorkspaceID == ""` 走原逻辑。

- [ ] **Step 2: 写测试**

```go
func TestSwitchWorkDir_AutoCreatesWorkspace(t *testing.T) {
	t.Parallel()
	env := newTestAuthEnv(t)
	cookie := env.createUserAndLogin(t, "alice", "alicepass1")
	// cd 到一个新目录 → 应自动创建对应 workspace
	... // 断言 workspaces 列表新增该 work_dir
}
```

- [ ] **Step 3: 运行 + 提交**

Run: `go test ./internal/gateway/ -run TestSwitchWorkDir -count=1 -race`

```bash
git add internal/gateway/bridge.go internal/gateway/session_isolation_test.go
git commit -m "feat(gateway): SwitchWorkDir 重映射到 workspace（自动创建 + key 派生，spec §9.4）"
```

---

## Phase 7：配额三层（per-workspace 并发）

### Task 7.1: PoolManager 加 per-workspace 并发层

**Files:**
- Modify: `internal/session/pool.go`
- Modify: `internal/config/config_types.go`（QuotaConfig）

- [ ] **Step 1: 写失败测试**

Create `internal/session/pool_workspace_test.go`:

```go
package session

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPool_PerWorkspaceConcurrency(t *testing.T) {
	t.Parallel()
	// 全局 100，per-user 5，per-workspace 2
	p := NewPoolManagerWithWorkspace(slog.Default(), 100, 5, 0, 2)

	ctx := context.Background()
	// ws-1 占满 2 个槽
	require.NoError(t, p.AcquireForWorkspace(ctx, "u1", "ws-1"))
	require.NoError(t, p.AcquireForWorkspace(ctx, "u1", "ws-1"))
	// ws-1 第 3 个应被拒（workspace 配额）
	err := p.AcquireForWorkspace(ctx, "u1", "ws-1")
	require.Error(t, err)

	// ws-2 仍可获取（workspace 维度独立）
	require.NoError(t, p.AcquireForWorkspace(ctx, "u1", "ws-2"))

	// 释放后 ws-1 恢复
	p.ReleaseForWorkspace(ctx, "u1", "ws-1")
	require.NoError(t, p.AcquireForWorkspace(ctx, "u1", "ws-1"))
}

func TestPool_PlatformSessionSkipsWorkspaceLayer(t *testing.T) {
	t.Parallel()
	p := NewPoolManagerWithWorkspace(slog.Default(), 100, 5, 0, 2)
	ctx := context.Background()
	// 平台会话 workspaceID="" 不受 per-workspace 限制
	for i := 0; i < 5; i++ {
		require.NoError(t, p.AcquireForWorkspace(ctx, "u1", ""))
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/session/ -run 'TestPool_PerWorkspace|TestPool_Platform' -count=1`

Expected: 编译失败（AcquireForWorkspace/NewPoolManagerWithWorkspace 未定义）

- [ ] **Step 3: 改 PoolManager**

Modify `internal/session/pool.go`：

(a) struct 加字段：

```go
type PoolManager struct {
	...（现有字段）
	workspaceCount map[string]int // workspaceID → active session count
	maxPerWorkspace int            // 0 = unlimited
}
```

(b) 构造函数加重载：

```go
func NewPoolManagerWithWorkspace(log *slog.Logger, maxSize, maxIdlePerUser int, maxMemoryPerUser int64, maxPerWorkspace int) *PoolManager {
	p := NewPoolManager(log, maxSize, maxIdlePerUser, maxMemoryPerUser)
	p.workspaceCount = make(map[string]int)
	p.maxPerWorkspace = maxPerWorkspace
	return p
}
```

(c) 新增 per-workspace 感知的 Acquire/Release（保留现有 Acquire/Release 给平台会话用）：

```go
const poolErrKindWorkspaceQuotaExceeded = "workspace_quota_exceeded"

// AcquireForWorkspace reserves a slot honoring global + per-user + per-workspace limits.
// workspaceID == "" (platform/cron sessions) skips the per-workspace layer.
func (p *PoolManager) AcquireForWorkspace(ctx context.Context, userID, workspaceID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.maxSize > 0 && p.totalCount >= p.maxSize {
		return &PoolError{Kind: poolErrKindExhausted, Current: p.totalCount, Max: p.maxSize}
	}
	if p.maxIdlePerUser > 0 && p.userCount[userID] >= p.maxIdlePerUser {
		return &PoolError{Kind: poolErrKindUserQuotaExceeded, UserID: userID, Current: p.userCount[userID], Max: p.maxIdlePerUser}
	}
	if workspaceID != "" && p.maxPerWorkspace > 0 && p.workspaceCount[workspaceID] >= p.maxPerWorkspace {
		return &PoolError{Kind: poolErrKindWorkspaceQuotaExceeded, Current: p.workspaceCount[workspaceID], Max: p.maxPerWorkspace}
	}

	p.userCount[userID]++
	p.totalCount++
	if workspaceID != "" {
		p.workspaceCount[workspaceID]++
	}
	if p.maxSize > 0 {
		setPoolUtilization(float64(p.totalCount) / float64(p.maxSize))
	}
	return nil
}

// ReleaseForWorkspace releases a slot acquired via AcquireForWorkspace.
func (p *PoolManager) ReleaseForWorkspace(ctx context.Context, userID, workspaceID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.userCount[userID] <= 0 || p.totalCount <= 0 {
		p.log.Error("pool: release without acquire", "user_id", userID)
		return
	}
	p.userCount[userID]--
	if p.userCount[userID] <= 0 {
		delete(p.userCount, userID)
	}
	p.totalCount--
	if workspaceID != "" && p.workspaceCount[workspaceID] > 0 {
		p.workspaceCount[workspaceID]--
		if p.workspaceCount[workspaceID] <= 0 {
			delete(p.workspaceCount, workspaceID)
		}
	}
	if p.maxSize > 0 {
		setPoolUtilization(float64(p.totalCount) / float64(p.maxSize))
	}
}
```

(d) `UpdateLimits` 加 `maxPerWorkspace` 参数（或新增 `UpdateWorkspaceLimit`）。

- [ ] **Step 4: 加 QuotaConfig**

Modify `internal/config/config_types.go`：

```go
// QuotaConfig holds per-workspace concurrency limits (spec §10.2).
type QuotaConfig struct {
	MaxConcurrentPerWorkspace int `mapstructure:"max_concurrent_per_workspace"` // 默认 3
}
```

并在 `PoolConfig` 加 `MaxConcurrentPerWorkspace` 字段（或单独 QuotaConfig 挂到 config 顶层），default 3（`config_defaults.go`）。

- [ ] **Step 5: 运行测试确认通过**

Run: `go test ./internal/session/ -run 'TestPool_' -count=1 -race`

Expected: PASS

- [ ] **Step 6: 接入 — CreateSession 用 AcquireForWorkspace**

Modify bridge/gateway 启动 webchat session 时，用 `pool.AcquireForWorkspace(ctx, userID, workspaceID)` 替代现有 `Acquire`；释放用 `ReleaseForWorkspace`。平台会话（workspaceID=""）仍可用原 `Acquire` 或统一用 `AcquireForWorkspace(ctx, userID, "")`（空串跳过 workspace 层）。

- [ ] **Step 7: 提交**

```bash
git add internal/session/pool.go internal/session/pool_workspace_test.go \
        internal/config/config_types.go internal/config/config_defaults.go \
        internal/gateway/bridge*.go
git commit -m "feat(session): PoolManager per-workspace 并发配额层（spec §10）"
```

---

## Phase 8：api_key_users 迁移验证 + 旧 webchat 会话清理

### Task 8.1: 迁移 017/018 端到端测试

**Files:**
- Create: `internal/session/migration_multitenancy_test.go`

- [ ] **Step 1: 写迁移测试（SQLite）**

```go
package session

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMigration017_CreatesTables(t *testing.T) {
	t.Parallel()
	store, cleanup := newTestStore(t) // 跑完所有迁移（含 017）
	defer cleanup()
	ctx := context.Background()

	// users/workspaces/invitations 可写
	require.NoError(t, store.CreateUser(ctx, &securityUser("u1", "alice"), 1700000000))
	require.NoError(t, store.CreateWorkspace(ctx, &Workspace{ID: "ws1", OwnerUserID: "u1", Name: "p", WorkDir: "/tmp/p"}, 1700000000))

	got, err := store.GetUserByUsername(ctx, "alice")
	require.NoError(t, err)
	require.Equal(t, "u1", got.ID)

	// sessions.workspace_id 列存在且可写（Task 0.4 已覆盖）
}

func TestMigration018_ApiKeyUsersProvisioned(t *testing.T) {
	t.Parallel()
	store, cleanup := newTestStore(t)
	defer cleanup()
	ctx := context.Background()

	// 预置 010 表的一个 api_key_users 行（user_id 为任意字符串）
	// 需直接 Exec（api_key_users 表的写入不在本 store 接口）
	db := testStoreDB(t, store) // 暴露底层 *sql.DB 的测试辅助
	_, err := db.ExecContext(ctx, "INSERT INTO api_key_users (api_key, user_id, created_at, updated_at) VALUES (?, ?, ?, ?)",
		"sk-test", "legacy-user", 1700000000, 1700000000)
	require.NoError(t, err)

	// 手动跑 018 provision（newTestStore 已跑过，此处用独立函数重跑或验证）
	// 验证：users 表存在 username='apikey:legacy-user' 的行，api_key_users.user_id 指向它
	u, err := store.GetUserByUsername(ctx, "apikey:legacy-user")
	require.NoError(t, err)
	require.NotEmpty(t, u.ID)
	require.Equal(t, "", u.PasswordHash, "API key 用户无密码，禁止账号登录")

	// api_key_users.user_id 现在指向 users.id
	var mappedID string
	require.NoError(t, db.QueryRowContext(ctx, "SELECT user_id FROM api_key_users WHERE api_key = ?", "sk-test").Scan(&mappedID))
	require.Equal(t, u.ID, mappedID)
}
```

> `newTestStore` 需在创建后跑全部迁移（含 018）。若 018 依赖已有 api_key_users 数据，测试需在迁移前插入——这需要可控的迁移顺序。**简化**：018 的 provision 测试改为独立函数 `runMigration018(t, db)` 显式调用，验证 provision 逻辑。或：测试先用 db.Exec 手动插入 legacy 行，再手动执行 018 SQL 文件内容。

- [ ] **Step 2: 运行测试确认通过**

Run: `go test ./internal/session/ -run 'TestMigration017|TestMigration018' -count=1 -race`

Expected: PASS

- [ ] **Step 3: 提交**

```bash
git add internal/session/migration_multitenancy_test.go
git commit -m "test(session): 迁移 017/018 端到端验证（建表 + api_key provision）"
```

### Task 8.2: 旧 webchat 共享会话清理

**Files:**
- Modify: `internal/session/store.go`（加清理方法）
- Modify: `internal/session/manager.go`（GC 周期清理 或 启动时一次性清理）

- [ ] **Step 1: 加清理查询**

Create `internal/session/sql/queries/store.cleanup_legacy_webchat.sql`:

```sql
DELETE FROM sessions WHERE user_id IN ('webchat_user', 'anonymous')
```

- [ ] **Step 2: store 方法 + 启动时调用**

```go
func (s *SQLiteStore) CleanupLegacyWebChatSessions(ctx context.Context) (int64, error) {
	res, err := s.writeMu.WithLock(func() (sql.Result, error) {
		return s.db.ExecContext(ctx, queries["store.cleanup_legacy_webchat"])
	})
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
```

在 `Manager.Start`（或 gateway 启动序列）迁移完成后调一次：

```go
if n, err := store.CleanupLegacyWebChatSessions(ctx); err == nil && n > 0 {
    m.log.Info("session: cleaned up legacy webchat shared sessions", "count", n)
}
```

- [ ] **Step 3: 测试 + 提交**

```go
func TestCleanupLegacyWebChatSessions(t *testing.T) {
	// 插入 user_id='webchat_user' 的会话 → 清理 → 验证删除
}
```

```bash
git add internal/session/store.go internal/session/pg_store.go internal/session/manager.go \
        internal/session/sql/queries/store.cleanup_legacy_webchat.sql
git commit -m "feat(session): 启动时清理旧 webchat 共享会话（spec §13.3）"
```

---

## Phase 9：路由注册 + 端到端集成测试

### Task 9.1: 路由注册（routes.go）

**Files:**
- Modify: `cmd/hotplex/routes.go`

- [ ] **Step 1: 注册新路由**

Modify `cmd/hotplex/routes.go` 的 `setupRoutes`，在现有 session 路由后追加：

```go
// 认证（Gateway API 端口 8888）
authH := NewAuthHandlers(deps.Auth, deps.CookieAuth, deps.SessionStore, deps.LocalAccountProvider)
mux.Handle("POST /api/auth/login", corsMw(http.HandlerFunc(authH.Login)))
mux.Handle("POST /api/auth/logout", corsMw(http.HandlerFunc(authH.Logout)))
mux.Handle("GET /api/auth/me", corsMw(http.HandlerFunc(authH.Me)))
mux.Handle("POST /api/auth/accept-invite", corsMw(http.HandlerFunc(authH.AcceptInvite)))

// admin（需 admin 角色）
mux.Handle("POST /api/admin/invitations", corsMw(http.HandlerFunc(authH.AdminCreateInvitation)))
mux.Handle("GET /api/admin/invitations", corsMw(http.HandlerFunc(authH.AdminListInvitations)))
mux.Handle("DELETE /api/admin/invitations/{id}", corsMw(http.HandlerFunc(authH.AdminDeleteInvitation)))
mux.Handle("GET /api/admin/users", corsMw(http.HandlerFunc(authH.AdminListUsers)))
mux.Handle("PATCH /api/admin/users/{id}", corsMw(http.HandlerFunc(authH.AdminUpdateUserStatus)))

// workspace CRUD
wsH := NewWorkspaceHandlers(deps.SessionStore, deps.CookieAuth, deps.Auth, deps.Config)
mux.Handle("GET /api/workspaces", corsMw(http.HandlerFunc(wsH.List)))
mux.Handle("POST /api/workspaces", corsMw(http.HandlerFunc(wsH.Create)))
mux.Handle("GET /api/workspaces/{id}", corsMw(http.HandlerFunc(wsH.Get)))
mux.Handle("PATCH /api/workspaces/{id}", corsMw(http.HandlerFunc(wsH.Update)))
mux.Handle("DELETE /api/workspaces/{id}", corsMw(http.HandlerFunc(wsH.Delete)))

// OPTIONS preflight（CORS，对每组新路径）
mux.Handle("OPTIONS /api/auth/{path...}", corsMw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })))
mux.Handle("OPTIONS /api/workspaces/{path...}", corsMw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })))
```

> `GatewayDeps` 需加 `SessionStore`/`LocalAccountProvider` 字段（在 `gateway_run.go` 组装时注入）。`NewAuthHandlers`/`NewWorkspaceHandlers` 在 gateway 包内。

- [ ] **Step 2: 组装依赖（gateway_run.go）**

Modify `cmd/hotplex/gateway_run.go`：在 `GatewayDeps` 构造处，创建 `LocalAccountProvider` 并 `auth.SetIdentityProvider(idp)`：

```go
idp := security.NewLocalAccountProvider(store.(security.UserStore), bcryptCost)
auth.SetIdentityProvider(idp)
// PoolManager 用 NewPoolManagerWithWorkspace
pool := session.NewPoolManagerWithWorkspace(log, cfg.Pool.MaxSize, cfg.Pool.MaxIdlePerUser, cfg.Pool.MaxMemoryPerUser, cfg.Pool.MaxConcurrentPerWorkspace)
```

- [ ] **Step 3: 全量构建 + 验证路由**

Run: `make build && ./bin/hotplex gateway start &; sleep 2; curl -s localhost:8888/api/auth/me; kill %1`

Expected: `{"error":{"code":"INVALID_CREDENTIALS",...}}`（未登录 401，证明路由注册成功）

- [ ] **Step 4: 提交**

```bash
git add cmd/hotplex/routes.go cmd/hotplex/gateway_run.go
git commit -m "feat(routes): 注册 auth/admin/workspace 路由 + 组装 IdentityProvider（spec §11）"
```

### Task 9.2: 端到端隔离集成测试（最高优先级，spec §14.2）

**Files:**
- Create: `internal/gateway/multitenancy_e2e_test.go`

- [ ] **Step 1: 写完整隔离场景**

```go
package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hrygo/hotplex/internal/security"
	"github.com/hrygo/hotplex/internal/session"
	"github.com/stretchr/testify/require"
)

// TestMultitenancy_IsolationE2E 是 spec §14.2 的最高优先级隔离测试：
// 两个用户各自两个 workspace，断言互不可见、配额独立。
func TestMultitenancy_IsolationE2E(t *testing.T) {
	t.Parallel()
	env := newTestAuthEnv(t)

	alice := env.createUserAndLogin(t, "alice", "alicepass1")
	bob := env.createUserAndLogin(t, "bob", "bobpass1234")

	aliceWS1 := env.createWorkspace(t, alice, "a1", "/tmp/a1")
	aliceWS2 := env.createWorkspace(t, alice, "a2", "/tmp/a2")
	bobWS1 := env.createWorkspace(t, bob, "b1", "/tmp/b1")

	// 各自建会话
	env.createSessionIn(t, alice, aliceWS1.ID)
	env.createSessionIn(t, bob, bobWS1.ID)

	// 1. alice 列 workspace 只见 a1/a2
	wsList := env.listWorkspaces(t, alice)
	require.Len(t, wsList, 2)
	for _, ws := range wsList {
		require.NotEqual(t, bobWS1.ID, ws.ID)
	}

	// 2. bob 列会话只见自己的
	for _, s := range env.listSessions(t, bob) {
		require.NotEqual(t, aliceWS1.ID, s.WorkspaceID)
	}

	// 3. alice 用 bob 的 workspace_id 建 session → 403
	body := `{"workspace_id":"` + bobWS1.ID + `","client_session_id":"x"}`
	req := httptest.NewRequest(http.MethodPost, "/api/sessions", bytes.NewBufferString(body))
	req.Header.Set("Cookie", alice)
	w := httptest.NewRecorder()
	env.gatewayAPI.CreateSession(w, req)
	require.Equal(t, http.StatusForbidden, w.Code)

	// 4. work_dir 1:1：alice 再建同 work_dir 的 workspace → 409
	req2 := httptest.NewRequest(http.MethodPost, "/api/workspaces", bytes.NewBufferString(`{"name":"dup","work_dir":"/tmp/a1"}`))
	req2.Header.Set("Cookie", alice)
	w2 := httptest.NewRecorder()
	env.wsHandlers.Create(w2, req2)
	require.Equal(t, http.StatusConflict, w.Code)

	// 5. 不同用户可指向同一 work_dir（协作场景，spec §2.2）
	wsBobSameDir := env.createWorkspace(t, bob, "bob-at-a1", "/tmp/a1")
	require.NotEmpty(t, wsBobSameDir.ID)

	_ = context.Background
	_ = json.NewEncoder
	_ = http.MethodPost
	_ = security.UserStore(nil) // keep imports
	_ = session.ErrWorkspaceNotFound
}
```

> `createSessionIn`/`listWorkspaces`/`listSessions`/`parseSessionList` 是测试辅助，在测试文件内实现（调用对应 handler + 解析响应）。

- [ ] **Step 2: 运行 + 修至通过**

Run: `go test ./internal/gateway/ -run TestMultitenancy_IsolationE2E -count=1 -race`

Expected: PASS

- [ ] **Step 3: 全量质量门禁**

Run: `make check`

Expected: fmt + vet + verify + lint + build + test 全绿

- [ ] **Step 4: 提交**

```bash
git add internal/gateway/multitenancy_e2e_test.go
git commit -m "test(gateway): 多租户隔离端到端测试（spec §14.2 最高优先级）"
```

---

## Self-Review 自检清单（执行前最后核对）

实现全部 task 后，对照 spec 逐项核对覆盖：

| spec 章节 | 覆盖 task |
|---|---|
| §2 双轨定位 | 贯穿（非目标表 + 配置继承层级；Message Channel 路径全部保持不变） |
| §6.1 users 表 | Task 0.1/0.2 + Task 1.1/1.2 |
| §6.2 workspaces 表 | Task 0.1/0.2 + Task 1.3 |
| §6.3 invitations 表 | Task 0.1/0.2 + Task 1.4 |
| §6.4 sessions.workspace_id | Task 0.4 |
| §6.5 api_key_users 迁移 | Task 0.3 + Task 8.1 |
| §7 session key 方案3 | Task 5.1 |
| §8.1 IdentityProvider 接口 | Task 1.2（接口）+ Task 2.2 |
| §8.2 三认证通道 | Task 3.2（cookie 返真实 id）+ Task 3.3（账号登录）+ Task 0.3（API key 指向 users.id） |
| §8.3 Cookie TTL/刷新 | Task 3.1 |
| §8.4 过渡状态 | Task 3.6 |
| §8.5 bootstrap admin | Task 3.5 |
| §8.6 邀请制 | Task 3.3 + Task 3.4 |
| §9.1 workspace CRUD | Task 4.1 |
| §9.2 CreateSession 打通 | Task 5.2 |
| §9.3 会话隔离查询 | Task 6.1 + Task 6.2 |
| §9.4 SwitchWorkDir 重映射 | Task 6.3 |
| §10 配额三层 | Task 7.1 |
| §11 API 端点 | Task 3.3/3.4/4.1/5.2/6.1/9.1 |
| §12 错误码 | 散布各 handler（AppError code 字段） |
| §13 迁移策略 | Task 0.1-0.3 + Task 8.1/8.2 |
| §14 测试策略 | 各 task 内 TDD + Task 9.2 端到端 |
| §3.2 非目标 | 不实现：前端 UI（⑥）、per-ws agent-configs（②）、worker 选择（③）、OAuth（④） |

**未在本计划实现（确认留待后续 spec）**：
- workspace.agent_config_overrides 实际消费（spec ②）
- workspace.worker_preference 实际消费（spec ③，本 spec 仅落库占位）
- webchat 前端登录 UI（spec ⑥）
- OAuth provider（spec ④）

**类型一致性检查**：
- `security.User`（security 包定义）vs `session.Workspace`/`session.Invitation`（session 包定义）—— User 跨包，UserStore 接口在 security。
- `DeriveSessionKey(ownerID, wt, clientKey, workspaceID, workDir)` —— 全计划统一此签名。
- `PoolManager.AcquireForWorkspace(ctx, userID, workspaceID)` / `ReleaseForWorkspace` —— 统一。
- store 哨兵：`security.ErrUserNotFound`（统一，session 实现返回此）、`session.ErrWorkspaceNotFound`、`session.ErrInvitationNotFound`、`session.ErrInvitationAlreadyUsed`。

**已知执行风险**（执行者注意）：
1. `newTestStore` 是 session 包私有测试辅助，gateway 测试需自建临时 SQLiteStore（用 `session.NewSQLiteStore` + 临时 db 路径 + 跑迁移）。需在 gateway 测试包实现等价辅助。
2. `GatewayDeps` 字段扩展、`Manager.CreateWithBot` 签名变更会影响现有调用点（Slack/Feishu/cron），需全量编译修正。
3. `security.NewAuthenticator` / `SetCookieAuth` 现有签名需对齐（本计划假设存在 setter）。
4. Go 1.22 ServeMux 的 `{id}` 路径参数用 `r.PathValue("id")`；CORS preflight 每组新路径需补 OPTIONS。
5. bridge.StartSession mock：CreateSession 测试聚焦校验路径（403/404/400 在 StartSession 之前返回），避免依赖真 worker。

---

## 执行顺序建议

按 Phase 顺序执行（依赖链：0 → 1 → 2 → 3 → 4 → 5 → 6 → 7 → 8 → 9）。每个 task 内严格 TDD（先写失败测试 → 实现 → 通过 → commit）。

**关键里程碑**：
- Phase 2 完成：账号登录可用（bootstrap admin + login）。
- Phase 5 完成：CreateSession 绑定 workspace（隔离核心打通）。
- Phase 9 完成：端到端隔离验证通过，spec ① 交付。

