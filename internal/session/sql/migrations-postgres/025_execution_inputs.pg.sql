-- +goose Up
-- Durable, content-free ingress ledger for idempotent user input delivery.
CREATE TABLE execution_inputs (
    execution_id      TEXT PRIMARY KEY,
    session_id        TEXT NOT NULL,
    client_message_id TEXT NOT NULL,
    payload_hash      TEXT NOT NULL,
    status            TEXT NOT NULL CHECK(status IN ('accepted', 'delivered', 'unknown', 'failed')),
    error_code        TEXT NOT NULL DEFAULT '',
    created_at        BIGINT NOT NULL,
    updated_at        BIGINT NOT NULL,
    delivered_at      BIGINT,
    UNIQUE(session_id, client_message_id),
    FOREIGN KEY(session_id) REFERENCES sessions(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_execution_inputs_session_created ON execution_inputs(session_id, created_at);
CREATE INDEX IF NOT EXISTS idx_execution_inputs_status_updated ON execution_inputs(status, updated_at);

-- +goose Down
DROP INDEX IF EXISTS idx_execution_inputs_status_updated;
DROP INDEX IF EXISTS idx_execution_inputs_session_created;
DROP TABLE IF EXISTS execution_inputs;
