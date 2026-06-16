-- +goose Up
-- users.list 按 created_at 排序（spec §11.2 admin 列表），加索引避免全表 filesort。
CREATE INDEX IF NOT EXISTS idx_users_created_at ON users(created_at);

-- +goose Down
DROP INDEX IF EXISTS idx_users_created_at;
