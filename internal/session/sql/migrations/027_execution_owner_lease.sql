-- +goose Up
-- Durable ingress reliability closure: owner lease, runtime status, active gate.
-- Spec: docs/superpowers/specs/2026-07-14-durable-ingress-reliability-closure-design.md
ALTER TABLE execution_inputs ADD COLUMN owner_instance_id  TEXT    NOT NULL DEFAULT '';
ALTER TABLE execution_inputs ADD COLUMN worker_run_id      TEXT    NOT NULL DEFAULT '';
ALTER TABLE execution_inputs ADD COLUMN lease_until        INTEGER NOT NULL DEFAULT 0;
ALTER TABLE execution_inputs ADD COLUMN runtime_status     TEXT    NOT NULL DEFAULT 'unknown'
    CHECK(runtime_status IN ('pending', 'running', 'completed', 'failed', 'unknown'));
ALTER TABLE execution_inputs ADD COLUMN runtime_error_code TEXT    NOT NULL DEFAULT '';
ALTER TABLE execution_inputs ADD COLUMN started_at         INTEGER;
ALTER TABLE execution_inputs ADD COLUMN finished_at        INTEGER;
ALTER TABLE execution_inputs ADD COLUMN fence_reason       TEXT    NOT NULL DEFAULT '';

-- Old-data migration (spec lines 175-188):
-- accepted → delivery=unknown, runtime=unknown, error=MIGRATION_RECOVERY
UPDATE execution_inputs
SET status = 'unknown',
    error_code = 'MIGRATION_RECOVERY',
    runtime_status = 'unknown',
    runtime_error_code = 'MIGRATION_RECOVERY'
WHERE status = 'accepted';
-- delivered → runtime=unknown (cannot infer completion)
-- (runtime_status defaults to 'unknown', no update needed)
-- failed → runtime=failed
UPDATE execution_inputs SET runtime_status = 'failed' WHERE status = 'failed';
-- unknown → runtime=unknown (already default)

CREATE INDEX idx_execution_owner_runtime
    ON execution_inputs(owner_instance_id, runtime_status, lease_until);

CREATE UNIQUE INDEX idx_execution_one_active_per_session
    ON execution_inputs(session_id)
    WHERE runtime_status IN ('pending', 'running') OR fence_reason <> '';

-- +goose Down
DROP INDEX IF EXISTS idx_execution_one_active_per_session;
DROP INDEX IF EXISTS idx_execution_owner_runtime;
ALTER TABLE execution_inputs DROP COLUMN fence_reason;
ALTER TABLE execution_inputs DROP COLUMN finished_at;
ALTER TABLE execution_inputs DROP COLUMN started_at;
ALTER TABLE execution_inputs DROP COLUMN runtime_error_code;
ALTER TABLE execution_inputs DROP COLUMN runtime_status;
ALTER TABLE execution_inputs DROP COLUMN lease_until;
ALTER TABLE execution_inputs DROP COLUMN worker_run_id;
ALTER TABLE execution_inputs DROP COLUMN owner_instance_id;
