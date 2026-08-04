-- +goose Up
-- Issue #833 (User Behavior Audit System, Phase 1) — Spec §5.1 / §5.5 / §11.2.
-- Append-only audit log with per-row sha256 hash chain. UPDATE is blocked at the DB
-- layer via BEFORE UPDATE trigger (migration 030 later added trg_ua_no_delete,
-- which blocks every DELETE that is not anchored by a checkpoint written in the
-- same transaction — the audit GC loop's prune path). v_turns* are
-- dead views from before migration 009 (Turns-Materialized-Table) — DROP IF EXISTS is
-- a harmless no-op for fresh DBs and cleans up dev/test DBs that still carry them.

CREATE TABLE user_activity (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    ts            INTEGER NOT NULL,                  -- UTC ms
    user_id       TEXT    NOT NULL,                  -- 平台原生 ID(ou_/U.../UUID)
    user_id_type  TEXT    NOT NULL,                  -- 'platform' | 'registered' | 'anonymous' | 'system'
    platform      TEXT    NOT NULL,                  -- webchat|feishu|slack|cron|admin|api
    session_id    TEXT,                              -- nullable (admin/api have no session)
    action        TEXT    NOT NULL,
    resource_type TEXT,
    resource_id   TEXT,
    outcome       TEXT    NOT NULL,                  -- success|failure|denied
    detail_json   TEXT    NOT NULL,                  -- whitelisted fields (see spec §5.3/§5.9)
    event_ref     TEXT,                              -- points to events.id or turns.id
    ip            TEXT,
    user_agent    TEXT,
    prev_hash     TEXT    NOT NULL,                  -- hash chain
    self_hash     TEXT    NOT NULL                   -- sha256(prev_hash || canonical(rest))
);

CREATE INDEX idx_ua_user_ts   ON user_activity(user_id, ts);
CREATE INDEX idx_ua_ts        ON user_activity(ts);
CREATE INDEX idx_ua_action_ts ON user_activity(action, ts);

-- +goose StatementBegin
CREATE TRIGGER trg_ua_no_update
BEFORE UPDATE ON user_activity
BEGIN
    SELECT RAISE(ABORT, 'audit: rows are immutable');
END;
-- +goose StatementEnd

CREATE TABLE audit_chain_checkpoints (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    pruned_at       INTEGER NOT NULL,                 -- UTC ms when the prefix was deleted
    last_self_hash  TEXT    NOT NULL,                 -- self_hash of the last pruned row (new genesis)
    next_id         INTEGER NOT NULL                  -- id of the first surviving row (for tie-break)
);

-- Spec §11.2: dead views from pre-009 era. DROP IF EXISTS is a no-op on fresh DBs and
-- cleans up dev/test DBs that still carry them.
DROP VIEW IF EXISTS v_turns;
DROP VIEW IF EXISTS v_turns_user;
DROP VIEW IF EXISTS v_turns_assistant;

-- +goose Down
DROP TRIGGER  IF EXISTS trg_ua_no_update;
DROP TABLE    IF EXISTS user_activity;
DROP TABLE    IF EXISTS audit_chain_checkpoints;
-- Spec §11.2: dead views from pre-009 era. Recreating them on downgrade is out of scope
-- (the v_turns* definitions live in archive/specs only — never as live SQL), so Down
-- intentionally does not re-CREATE the views.
