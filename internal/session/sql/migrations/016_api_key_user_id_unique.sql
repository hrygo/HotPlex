-- +goose Up
-- Enforce 1:1 mapping: each user_id can only have one API key.
-- Advisory: before running in production, execute:
--   SELECT user_id, COUNT(*) FROM api_key_users GROUP BY user_id HAVING COUNT(*) > 1;
-- Deduplicate: keep the latest row per user_id (largest rowid), older rows are deleted.
-- Affected API keys will be permanently invalidated.
DELETE FROM api_key_users
WHERE rowid NOT IN (
    SELECT MAX(rowid) FROM api_key_users GROUP BY user_id
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_api_key_users_user_id_unique ON api_key_users(user_id);

-- +goose Down
DROP INDEX IF EXISTS idx_api_key_users_user_id_unique;
