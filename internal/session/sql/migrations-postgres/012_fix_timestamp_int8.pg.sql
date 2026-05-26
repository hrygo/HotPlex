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

ALTER TABLE "events"             ALTER COLUMN "created_at"      TYPE INTEGER;
ALTER TABLE "cron_jobs"          ALTER COLUMN "created_at"      TYPE INTEGER;
ALTER TABLE "cron_jobs"          ALTER COLUMN "updated_at"      TYPE INTEGER;
ALTER TABLE "chat_access_events" ALTER COLUMN "created_at"      TYPE INTEGER;
ALTER TABLE "chat_access_events" ALTER COLUMN "last_message_at" TYPE INTEGER;
ALTER TABLE "turns"              ALTER COLUMN "created_at"      TYPE INTEGER;
