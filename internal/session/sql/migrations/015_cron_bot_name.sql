-- +goose Up
-- Add bot_name column for YAML config name based agent-config path resolution.
-- Default '' (empty string) means platform-level fallback: existing cron jobs
-- created before this migration will resolve agent-configs from the platform
-- directory (e.g. agent-configs/feishu/) rather than per-bot directory.
-- This is intentional: upgrade preserves existing behavior until users explicitly
-- set bot_name on their cron jobs. Document this in release notes.
ALTER TABLE cron_jobs ADD COLUMN bot_name TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE cron_jobs DROP COLUMN bot_name;
