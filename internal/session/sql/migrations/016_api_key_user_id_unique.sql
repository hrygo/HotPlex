-- +goose Up
-- Enforce 1:1 mapping: each user_id can only have one API key.
-- Prerequisite: ensure no duplicate user_id before applying.
--   Check:  SELECT user_id, COUNT(*) FROM api_key_users GROUP BY user_id HAVING COUNT(*) > 1;
--   Dedup:  Use scripts/dedup-api-key-users.sh (SQLite) or scripts/dedup-api-key-users.pg.sh (PostgreSQL).
DROP INDEX IF EXISTS idx_api_key_users_user_id;
CREATE UNIQUE INDEX IF NOT EXISTS idx_api_key_users_user_id_unique ON api_key_users(user_id);

-- +goose Down
DROP INDEX IF EXISTS idx_api_key_users_user_id_unique;
CREATE INDEX IF NOT EXISTS idx_api_key_users_user_id ON api_key_users(user_id);
