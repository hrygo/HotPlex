-- +goose Up
-- Operator fence actions (#877): fencing token for conditional fence updates.
-- fence_version increments monotonically each time an execution ENTERS the
-- fenced state (from a non-fenced state); operator actions (resolve/abandon)
-- must present the version they read, so concurrent operators conflict
-- instead of double-migrating. fence_created_at records when the current
-- fence was raised. Old rows keep version 0 / NULL created_at.
-- Spec: docs/superpowers/specs/2026-08-05-runtime-operations-next-iteration-design.md
ALTER TABLE execution_inputs ADD COLUMN fence_version    INTEGER NOT NULL DEFAULT 0;
ALTER TABLE execution_inputs ADD COLUMN fence_created_at INTEGER;

CREATE INDEX idx_execution_fenced
    ON execution_inputs(fence_created_at)
    WHERE fence_reason <> '';

-- +goose Down
DROP INDEX IF EXISTS idx_execution_fenced;
ALTER TABLE execution_inputs DROP COLUMN fence_created_at;
ALTER TABLE execution_inputs DROP COLUMN fence_version;
