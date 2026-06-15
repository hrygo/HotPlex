-- +goose Up
-- WebChat 多租户地基（spec ①）：users / workspaces / invitations 三表 + sessions.workspace_id
-- 双轨完全隔离：这些多租户实体仅服务 WebChat 轨；Message Channel 轨会话 workspace_id 保持 NULL。

CREATE TABLE users (
    id            TEXT PRIMARY KEY,              -- UUID，权威用户标识
    username      TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL DEFAULT '',      -- bcrypt；空串 = API key provision 用户（禁止账号登录）
    role          TEXT NOT NULL DEFAULT 'user',  -- 'admin' | 'user'
    display_name  TEXT NOT NULL DEFAULT '',
    status        TEXT NOT NULL DEFAULT 'active', -- 'active' | 'disabled'
    created_at    INTEGER NOT NULL,
    updated_at    INTEGER NOT NULL,
    last_login_at INTEGER
);
CREATE INDEX idx_users_status ON users(status);

CREATE TABLE workspaces (
    id                     TEXT PRIMARY KEY,     -- UUID
    owner_user_id          TEXT NOT NULL REFERENCES users(id),
    name                   TEXT NOT NULL,
    work_dir               TEXT NOT NULL,        -- 创建后不可变（应用层强制）
    agent_config_overrides TEXT,                 -- JSON；spec ② 填充，spec ① 留 NULL
    worker_preference      TEXT,                 -- 'claude_code'|'opencode_server'|'codex_cli'|'acp'|NULL；spec ③ 填充
    status                 TEXT NOT NULL DEFAULT 'active',
    created_at             INTEGER NOT NULL,
    updated_at             INTEGER NOT NULL,
    UNIQUE(owner_user_id, work_dir)              -- per-user 内 1:1；不同用户可指向同一 work_dir 协作
);
CREATE INDEX idx_workspaces_owner ON workspaces(owner_user_id);

CREATE TABLE invitations (
    id         TEXT PRIMARY KEY,                 -- UUID
    code       TEXT NOT NULL UNIQUE,             -- 一次性邀请码（密码学随机）
    created_by TEXT NOT NULL REFERENCES users(id),
    role       TEXT NOT NULL DEFAULT 'user',     -- 被邀请人角色
    used_by    TEXT REFERENCES users(id),        -- NULL = 未使用
    expires_at INTEGER NOT NULL,
    created_at INTEGER NOT NULL,
    used_at    INTEGER
);
CREATE INDEX idx_invitations_code ON invitations(code);

-- sessions.workspace_id 不加 DB 级 FK：与 sessions.user_id 无 FK 的既有模式一致。
-- 归属隔离由应用层 ValidateOwnership 校验（spec §9.3），workspace 硬删除（spec §9.1）不被历史 session 引用阻止。
ALTER TABLE sessions ADD COLUMN workspace_id TEXT;
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
