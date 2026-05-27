-- +goose Up

-- Fix: millisecond timestamps exceed int4 max (2,147,483,647).
-- All *_at columns storing Unix ms must be BIGINT (int8).

ALTER TABLE "events"             ALTER COLUMN "created_at"      TYPE BIGINT;
ALTER TABLE "cron_jobs"          ALTER COLUMN "created_at"      TYPE BIGINT;
ALTER TABLE "cron_jobs"          ALTER COLUMN "updated_at"      TYPE BIGINT;
ALTER TABLE "chat_access_events" ALTER COLUMN "created_at"      TYPE BIGINT;
ALTER TABLE "chat_access_events" ALTER COLUMN "last_message_at" TYPE BIGINT;
ALTER TABLE "turns"              ALTER COLUMN "created_at"      TYPE BIGINT;

-- +goose Down

-- No-op: reverting to INTEGER would silently truncate Unix ms timestamps
-- (current ~1.7×10¹² exceeds int4 max 2,147,483,647). Keep BIGINT.
