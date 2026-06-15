-- +goose Up
-- api_key_users 统一指向 users.id（spec §13.2）。
-- 为每个 api_key_users.user_id（任意字符串）在 users 表 provision 一行：
--   username = 'apikey:<原user_id>'（确定性前缀，避免与真人 username 冲突）
--   password_hash = ''（禁止账号登录通道，仅 API key 通道有效）
--   role = 'user', status = 'active'
-- 然后把 api_key_users.user_id 改为指向新 users.id。
-- id 用 32 字符 hex（randomblob），TEXT PRIMARY KEY 唯一即可。

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

UPDATE api_key_users
SET user_id = (SELECT u.id FROM users u WHERE u.username = 'apikey:' || api_key_users.user_id)
WHERE EXISTS (SELECT 1 FROM users u WHERE u.username = 'apikey:' || api_key_users.user_id);

-- +goose Down
-- 不可逆还原（原 user_id 字符串已被覆盖）。Down 仅清空 provision 的 users 行。
DELETE FROM users WHERE username LIKE 'apikey:%';
