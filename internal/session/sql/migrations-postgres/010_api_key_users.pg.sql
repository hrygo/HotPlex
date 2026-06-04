-- +goose Up
-- API key → user identity mapping table for enterprise multi-user isolation.
-- Managed via Admin API CRUD endpoints. Queried by DBResolver.
CREATE TABLE IF NOT EXISTS "api_key_users" (
    "id" BIGSERIAL PRIMARY KEY,
    "api_key" TEXT NOT NULL UNIQUE,
    "user_id" TEXT NOT NULL,
    "description" TEXT NOT NULL DEFAULT '',
    "created_at" TIMESTAMP NOT NULL DEFAULT NOW(),
    "updated_at" TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS "idx_api_key_users_user_id" ON "api_key_users"("user_id");

-- Composite index for enterprise session list query pattern:
-- WHERE state != 'deleted' AND user_id = ? ORDER BY created_at DESC LIMIT ?
CREATE INDEX IF NOT EXISTS "idx_sessions_user_created"
    ON "sessions"("user_id", "created_at" DESC);

-- +goose Down
DROP INDEX IF EXISTS "idx_sessions_user_created";
DROP INDEX IF EXISTS "idx_api_key_users_user_id";
DROP TABLE IF EXISTS "api_key_users";
