-- +goose Up

CREATE TABLE IF NOT EXISTS "cron_jobs" (
    "id"               TEXT PRIMARY KEY,
    "name"             TEXT NOT NULL UNIQUE,
    "description"      TEXT NOT NULL DEFAULT '',
    "enabled"          BOOLEAN NOT NULL DEFAULT TRUE,
    "schedule_kind"    TEXT NOT NULL CHECK(schedule_kind IN ('at', 'every', 'cron')),
    "schedule_data"    TEXT NOT NULL,
    "payload_kind"     TEXT NOT NULL DEFAULT 'isolated_session' CHECK(payload_kind IN ('isolated_session', 'system_event', 'attached_session')),
    "payload_data"     TEXT NOT NULL,
    "work_dir"         TEXT NOT NULL DEFAULT '',
    "bot_id"           TEXT NOT NULL DEFAULT '',
    "owner_id"         TEXT NOT NULL DEFAULT '',
    "platform"         TEXT NOT NULL DEFAULT '',
    "platform_key"     TEXT NOT NULL DEFAULT '{}',
    "timeout_sec"      INTEGER NOT NULL DEFAULT 0,
    "delete_after_run" BOOLEAN NOT NULL DEFAULT FALSE,
    "silent"           BOOLEAN NOT NULL DEFAULT FALSE,
    "max_retries"      INTEGER NOT NULL DEFAULT 0,
    "max_runs"         INTEGER NOT NULL DEFAULT 0,
    "expires_at"       TEXT NOT NULL DEFAULT '',
    "state"            TEXT NOT NULL DEFAULT '{}',
    "created_at"       BIGINT NOT NULL,
    "updated_at"       BIGINT NOT NULL
);

CREATE INDEX IF NOT EXISTS "idx_cron_jobs_enabled" ON "cron_jobs"("enabled");
CREATE INDEX IF NOT EXISTS "idx_cron_jobs_next_run" ON "cron_jobs"("enabled", (((state::jsonb)->>'next_run_at_ms')::bigint));

-- +goose Down
DROP TABLE IF EXISTS "cron_jobs";
