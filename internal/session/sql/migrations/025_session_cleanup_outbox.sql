-- +goose Up
CREATE TABLE session_cleanup_tasks (
    id                TEXT PRIMARY KEY,
    session_id        TEXT NOT NULL UNIQUE,
    worker_type       TEXT NOT NULL,
    worker_session_id TEXT NOT NULL,
    attempts          INTEGER NOT NULL DEFAULT 0,
    next_attempt_at   TIMESTAMP NOT NULL,
    lease_until       TIMESTAMP NULL,
	lease_token       TEXT NULL,
    last_error        TEXT NOT NULL DEFAULT '',
    created_at        TIMESTAMP NOT NULL,
    updated_at        TIMESTAMP NOT NULL
);
CREATE INDEX idx_session_cleanup_tasks_due
    ON session_cleanup_tasks(next_attempt_at, lease_until);

-- +goose Down
DROP INDEX IF EXISTS idx_session_cleanup_tasks_due;
DROP TABLE IF EXISTS session_cleanup_tasks;
