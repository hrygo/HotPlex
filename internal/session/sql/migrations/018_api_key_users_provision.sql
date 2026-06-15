-- +goose Up
-- api_key_users 统一指向 users.id（spec §13.2）。
-- 为每个 api_key_users.user_id（任意字符串）在 users 表 provision 一行：
--   username = 'apikey:<原user_id>'（确定性前缀，避免与真人 username 冲突）
--   password_hash = ''（禁止账号登录通道，仅 API key 通道有效）
--   role = 'user', status = 'active'
-- 然后：
--   1) remap sessions.user_id（旧字符串 → 新 users.id）— 必须在改写 api_key_users 之前
--   2) 把 api_key_users.user_id 改为指向新 users.id
-- id 用 32 字符 hex（randomblob），TEXT PRIMARY KEY 唯一即可。
--
-- P1 fix: 原迁移遗漏 sessions.user_id remap，导致升级后 DBResolver 返回新 UUID
-- 而旧会话仍持有旧字符串 user_id，造成 ListSessions 查不到、ValidateOwnership 失败、
-- DeriveSessionKey 派生键断裂。以下 UPDATE sessions 必须在 api_key_users 改写之前执行。

INSERT INTO users (id, username, password_hash, role, display_name, status, created_at, updated_at)
SELECT
    lower(hex(randomblob(16))),
    'apikey:' || user_id,
    '',
    'user',
    '',
    'active',
    strftime('%s','now'),
    strftime('%s','now')
FROM (SELECT DISTINCT user_id FROM api_key_users) AS d
WHERE NOT EXISTS (SELECT 1 FROM users WHERE username = 'apikey:' || d.user_id);

-- Remap existing sessions from old string user_id to new users.id (P1 fix).
UPDATE sessions
SET user_id = (SELECT u.id FROM users u WHERE u.username = 'apikey:' || sessions.user_id)
WHERE EXISTS (
    SELECT 1 FROM api_key_users a
    JOIN users u ON u.username = 'apikey:' || a.user_id
    WHERE sessions.user_id = a.user_id
);

UPDATE api_key_users
SET user_id = (SELECT u.id FROM users u WHERE u.username = 'apikey:' || api_key_users.user_id)
WHERE EXISTS (SELECT 1 FROM users u WHERE u.username = 'apikey:' || api_key_users.user_id);

-- Post-migration: restart gateway or call DBResolver.InvalidateAll() to clear
-- the 60s in-memory cache, which would otherwise serve stale old-string user_id
-- values and create sessions with the wrong owner (review P2 fix).

-- +goose Down
-- 不可逆还原（原 user_id 字符串已被覆盖）。
-- 先置空 api_key_users.user_id 保持引用完整性，再清空 provision 的 users 行。
UPDATE api_key_users SET user_id = '' WHERE user_id IN (SELECT id FROM users WHERE username LIKE 'apikey:%');
DELETE FROM users WHERE username LIKE 'apikey:%' AND password_hash = '';
