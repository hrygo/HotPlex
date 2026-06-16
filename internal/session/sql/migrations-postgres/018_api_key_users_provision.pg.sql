-- +goose Up
-- api_key_users 统一指向 users.id（spec §13.2）— PostgreSQL 版。
-- id 用 gen_random_uuid()（标准 UUIDv4），与应用层 uuid.NewString() 一致。

INSERT INTO "users" ("id", "username", "password_hash", "role", "display_name", "status", "created_at", "updated_at")
SELECT
    gen_random_uuid(),
    'apikey:' || user_id,
    '',
    'user',
    '',
    'active',
    EXTRACT(EPOCH FROM NOW())::BIGINT,
    EXTRACT(EPOCH FROM NOW())::BIGINT
FROM (SELECT DISTINCT user_id FROM "api_key_users") AS d
WHERE NOT EXISTS (SELECT 1 FROM "users" u WHERE u.username = 'apikey:' || d.user_id);

-- Remap existing sessions from old string user_id to new users.id (P1 fix).
UPDATE "sessions"
SET user_id = (SELECT u.id FROM "users" u WHERE u.username = 'apikey:' || "sessions".user_id)
WHERE EXISTS (
    SELECT 1 FROM "api_key_users" a
    JOIN "users" u ON u.username = 'apikey:' || a.user_id
    WHERE "sessions".user_id = a.user_id
);

UPDATE "api_key_users"
SET user_id = (SELECT u.id FROM "users" u WHERE u.username = 'apikey:' || "api_key_users".user_id)
WHERE EXISTS (SELECT 1 FROM "users" u WHERE u.username = 'apikey:' || "api_key_users".user_id);

-- Post-migration: restart gateway or call DBResolver.InvalidateAll() to clear
-- the 60s in-memory cache (review P2 fix).

-- +goose Down
-- 先置空 api_key_users.user_id 保持引用完整性，再清空 provision 的 users 行（与 SQLite Down 对称）。
UPDATE "api_key_users" SET user_id = '' WHERE user_id IN (SELECT id FROM "users" WHERE username LIKE 'apikey:%');
DELETE FROM "users" WHERE username LIKE 'apikey:%' AND password_hash = '';
