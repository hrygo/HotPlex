-- +goose Up

CREATE TABLE IF NOT EXISTS "chat_access_events" (
    "id"                BIGSERIAL PRIMARY KEY,
    "event_id"          TEXT NOT NULL UNIQUE,
    "platform"          TEXT NOT NULL CHECK(platform IN ('feishu', 'slack')),
    "chat_id"           TEXT NOT NULL,
    "user_id"           TEXT NOT NULL,
    "bot_id"            TEXT NOT NULL DEFAULT '',
    "last_message_at"   BIGINT NOT NULL DEFAULT 0,
    "welcome_sent"      BOOLEAN NOT NULL DEFAULT FALSE,
    "created_at"        BIGINT NOT NULL
);

CREATE INDEX IF NOT EXISTS "idx_ca_event" ON "chat_access_events"("event_id");
CREATE INDEX IF NOT EXISTS "idx_ca_chat_bot" ON "chat_access_events"("platform", "chat_id", "bot_id");
CREATE INDEX IF NOT EXISTS "idx_ca_user_bot" ON "chat_access_events"("platform", "user_id", "bot_id");

-- +goose Down

DROP TABLE IF EXISTS "chat_access_events";
