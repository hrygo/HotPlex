-- +goose Up
CREATE TABLE session_cleanup_tasks (
    id                TEXT PRIMARY KEY,
    session_id        TEXT NOT NULL UNIQUE,
    worker_type       TEXT NOT NULL,
    worker_session_id TEXT NOT NULL,
    attempts          INTEGER NOT NULL DEFAULT 0,
    next_attempt_at   TIMESTAMPTZ NOT NULL,
    lease_until       TIMESTAMPTZ NULL,
	lease_token       TEXT NULL,
    last_error        TEXT NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ NOT NULL,
    updated_at        TIMESTAMPTZ NOT NULL
);
CREATE INDEX idx_session_cleanup_tasks_due
    ON session_cleanup_tasks(next_attempt_at, lease_until);

-- +goose Down
DROP INDEX IF EXISTS idx_session_cleanup_tasks_due;
DROP TABLE IF EXISTS session_cleanup_tasks;
