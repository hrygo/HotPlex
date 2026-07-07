-- +goose Up
-- Issue #833 final follow-ups: explicit cross-channel audit identity links.
-- Audit rows keep their original platform-native user_id; this table lets an
-- admin query one principal across multiple native subjects without rewriting
-- immutable audit rows.

CREATE TABLE audit_identity_links (
    id                TEXT PRIMARY KEY,
    principal_user_id TEXT NOT NULL,
    provider          TEXT NOT NULL,
    subject           TEXT NOT NULL,
    subject_type      TEXT NOT NULL DEFAULT 'platform',
    display_name      TEXT NOT NULL DEFAULT '',
    email             TEXT NOT NULL DEFAULT '',
    created_at        INTEGER NOT NULL,
    updated_at        INTEGER NOT NULL,
    UNIQUE(provider, subject)
);
CREATE INDEX idx_audit_identity_links_principal ON audit_identity_links(principal_user_id);
CREATE INDEX idx_audit_identity_links_lookup ON audit_identity_links(provider, subject);

-- +goose Down
DROP INDEX IF EXISTS idx_audit_identity_links_lookup;
DROP INDEX IF EXISTS idx_audit_identity_links_principal;
DROP TABLE IF EXISTS audit_identity_links;
