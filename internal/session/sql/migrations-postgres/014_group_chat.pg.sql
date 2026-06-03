-- +goose Up
-- Group chat collaboration sessions.
CREATE TABLE IF NOT EXISTS group_sessions (
    id TEXT PRIMARY KEY,
    topic TEXT NOT NULL,
    platform TEXT NOT NULL,
    channel_id TEXT NOT NULL DEFAULT '',
    thread_ts TEXT NOT NULL DEFAULT '',
    owner_id TEXT NOT NULL,
    initiator TEXT NOT NULL DEFAULT '',
    bot_ids TEXT NOT NULL DEFAULT '[]',
    state TEXT NOT NULL DEFAULT 'active',
    max_turns INTEGER NOT NULL DEFAULT 15,
    turn_count INTEGER NOT NULL DEFAULT 0,
    cost_limit_usd DOUBLE PRECISION NOT NULL DEFAULT 0,
    cost_accumulated DOUBLE PRECISION NOT NULL DEFAULT 0,
    turn_timeout_sec INTEGER NOT NULL DEFAULT 120,
    cooldown_ms INTEGER NOT NULL DEFAULT 5000,
    end_reason TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ended_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_group_sessions_channel ON group_sessions(channel_id);
CREATE INDEX IF NOT EXISTS idx_group_sessions_state ON group_sessions(state);
CREATE INDEX IF NOT EXISTS idx_group_sessions_owner ON group_sessions(owner_id);

-- Per-turn records for group chat discussions.
CREATE TABLE IF NOT EXISTS group_turns (
    id TEXT PRIMARY KEY,
    group_session_id TEXT NOT NULL REFERENCES group_sessions(id),
    bot_id TEXT NOT NULL,
    bot_name TEXT NOT NULL,
    turn_num INTEGER NOT NULL,
    content TEXT NOT NULL DEFAULT '',
    skipped INTEGER NOT NULL DEFAULT 0,
    sanitized INTEGER NOT NULL DEFAULT 1,
    sanitize_reason TEXT NOT NULL DEFAULT '',
    timeout_count INTEGER NOT NULL DEFAULT 0,
    cost_usd DOUBLE PRECISION NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_group_turns_session ON group_turns(group_session_id);

-- Audit log for group chat events.
CREATE TABLE IF NOT EXISTS group_chat_audit (
    id SERIAL PRIMARY KEY,
    event_type TEXT NOT NULL,
    session_id TEXT NOT NULL,
    bot_id TEXT NOT NULL DEFAULT '',
    initiator TEXT NOT NULL DEFAULT '',
    turn_num INTEGER NOT NULL DEFAULT 0,
    detail TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_group_chat_audit_session ON group_chat_audit(session_id);

-- +goose Down
DROP TABLE IF EXISTS group_chat_audit;
DROP TABLE IF EXISTS group_turns;
DROP TABLE IF EXISTS group_sessions;
