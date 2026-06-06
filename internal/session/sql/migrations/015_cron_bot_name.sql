-- +goose Up
ALTER TABLE cron_jobs ADD COLUMN bot_name TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE cron_jobs DROP COLUMN bot_name;
