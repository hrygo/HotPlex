-- +goose Up
-- WebChat 多租户地基（spec ①）：users / workspaces / invitations + sessions.workspace_id
-- PostgreSQL 镜像；标识符加双引号，时间用 BIGINT epoch（与 sessions 表现有列一致）。

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

-- sessions.workspace_id 不加 DB 级 FK：与 sessions.user_id 无 FK 的既有模式一致（见 SQLite 版注释）。
ALTER TABLE "sessions" ADD COLUMN "workspace_id" TEXT;
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
