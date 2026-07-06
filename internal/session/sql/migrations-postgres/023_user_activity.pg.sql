-- +goose Up
-- Issue #833 (User Behavior Audit System, Phase 1) — Spec §5.1 / §5.5 / §11.2.
-- See SQLite companion for full rationale. PG differences:
--   * id is BIGSERIAL (vs SQLite INTEGER AUTOINCREMENT)
--   * ts / pruned_at are BIGINT (Unix ms survives past 2038; PG has no native INTEGER alias)
--   * immutability is enforced via a plpgsql trigger FUNCTION, not an inline RAISE()
-- StatementBegin/End wrap the function body because plpgsql contains internal semicolons.

CREATE TABLE "user_activity" (
    "id"            BIGSERIAL PRIMARY KEY,
    "ts"            BIGINT  NOT NULL,                  -- UTC ms
    "user_id"       TEXT    NOT NULL,                  -- 平台原生 ID(ou_/U.../UUID)
    "user_id_type"  TEXT    NOT NULL,                  -- 'platform' | 'registered' | 'anonymous' | 'system'
    "platform"      TEXT    NOT NULL,                  -- webchat|feishu|slack|cron|admin|api
    "session_id"    TEXT,                              -- nullable (admin/api have no session)
    "action"        TEXT    NOT NULL,
    "resource_type" TEXT,
    "resource_id"   TEXT,
    "outcome"       TEXT    NOT NULL,                  -- success|failure|denied
    "detail_json"   TEXT    NOT NULL,
    "event_ref"     TEXT,
    "ip"            TEXT,
    "user_agent"    TEXT,
    "prev_hash"     TEXT    NOT NULL,
    "self_hash"     TEXT    NOT NULL
);

CREATE INDEX "idx_ua_user_ts"   ON "user_activity"("user_id", "ts");
CREATE INDEX "idx_ua_ts"        ON "user_activity"("ts");
CREATE INDEX "idx_ua_action_ts" ON "user_activity"("action", "ts");

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION "fn_ua_no_update"() RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'audit: rows are immutable';
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER "trg_ua_no_update"
BEFORE UPDATE ON "user_activity"
FOR EACH ROW EXECUTE FUNCTION "fn_ua_no_update"();

CREATE TABLE "audit_chain_checkpoints" (
    "id"              BIGSERIAL PRIMARY KEY,
    "pruned_at"       BIGINT  NOT NULL,                 -- UTC ms when the prefix was deleted
    "last_self_hash"  TEXT    NOT NULL,                 -- self_hash of the last pruned row (new genesis)
    "next_id"         BIGINT  NOT NULL                  -- id of the first surviving row
);

-- Spec §11.2: dead views from pre-009 era. DROP IF EXISTS is a no-op on fresh DBs and
-- cleans up dev/test DBs that still carry them.
DROP VIEW IF EXISTS "v_turns";
DROP VIEW IF EXISTS "v_turns_user";
DROP VIEW IF EXISTS "v_turns_assistant";

-- +goose Down
DROP TRIGGER IF EXISTS "trg_ua_no_update" ON "user_activity";
DROP FUNCTION IF EXISTS "fn_ua_no_update"();
DROP TABLE    IF EXISTS "user_activity";
DROP TABLE    IF EXISTS "audit_chain_checkpoints";
