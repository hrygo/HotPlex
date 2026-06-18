-- +goose Up
-- WebChat 多租户 spec ④：企业 SSO（OIDC 统一认证）
-- user_identities 将 OAuth 身份与 users 解耦：一个用户可关联多个 IdP。
-- UNIQUE(provider, subject) 保证 SSO 登录确定性映射到唯一 user_id。
-- users 表不增加字段；密码账号无 user_identities 行，完全向后兼容。

CREATE TABLE user_identities (
    id           TEXT PRIMARY KEY,              -- UUID
    user_id      TEXT NOT NULL REFERENCES users(id),
    provider     TEXT NOT NULL,                 -- provider name（config key）
    subject      TEXT NOT NULL,                 -- IdP subject（OIDC "sub" claim）
    display_name TEXT NOT NULL DEFAULT '',       -- 从 IdP 同步
    email        TEXT NOT NULL DEFAULT '',       -- 从 IdP 同步（仅记录，不用于自动合并）
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL,
    UNIQUE(provider, subject)
);
CREATE INDEX idx_user_identities_user_id ON user_identities(user_id);
CREATE INDEX idx_user_identities_lookup ON user_identities(provider, subject);

-- +goose Down
DROP INDEX IF EXISTS idx_user_identities_lookup;
DROP INDEX IF EXISTS idx_user_identities_user_id;
DROP TABLE IF EXISTS user_identities;
