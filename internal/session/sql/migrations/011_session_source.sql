-- +goose Up
ALTER TABLE sessions ADD COLUMN source TEXT NOT NULL DEFAULT '' CHECK(source IN ('', 'cron'));
CREATE INDEX IF NOT EXISTS idx_sessions_source_state_updated ON sessions(source, state, updated_at);

-- +goose Down
DROP INDEX IF EXISTS idx_sessions_source_state_updated;
-- Note: ALTER TABLE DROP COLUMN is not supported before SQLite 3.35.0 (2020-12-01).
-- To fully revert, recreate the sessions table without the source column.
