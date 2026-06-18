-- +goose Up
-- WebChat 多租户 spec ④：企业 SSO（OIDC 统一认证）
-- user_identities 将 OAuth 身份与 users 解耦：一个用户可关联多个 IdP。
-- UNIQUE(provider, subject) 保证 SSO 登录确定性映射到唯一 user_id。
-- users 表不增加字段；密码账号无 user_identities 行，完全向后兼容。

CREATE TABLE user_identities (
    id           TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users(id),
    provider     TEXT NOT NULL,
    subject      TEXT NOT NULL,
    display_name TEXT NOT NULL DEFAULT '',
    email        TEXT NOT NULL DEFAULT '',
    created_at   BIGINT NOT NULL,
    updated_at   BIGINT NOT NULL,
    UNIQUE(provider, subject)
);
CREATE INDEX IF NOT EXISTS idx_user_identities_user_id ON user_identities(user_id);
CREATE INDEX IF NOT EXISTS idx_user_identities_lookup ON user_identities(provider, subject);

-- +goose Down
DROP INDEX IF EXISTS idx_user_identities_lookup;
DROP INDEX IF EXISTS idx_user_identities_user_id;
DROP TABLE IF EXISTS user_identities;
