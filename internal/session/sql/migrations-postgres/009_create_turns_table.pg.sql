-- +goose Up

-- Turns materialized table: replaces v_turns / v_turns_assistant / v_turns_user views.
-- Written by Collector at done (assistant) and input (user) time.
-- id is BIGSERIAL: strictly ordered, used for pagination cursor and frontend replay.

CREATE TABLE IF NOT EXISTS "turns" (
    "id"                  BIGSERIAL PRIMARY KEY,
    "session_id"          TEXT    NOT NULL,
    "generation"          INTEGER NOT NULL DEFAULT 1,
    "turn_num"            INTEGER NOT NULL,           -- generation-scoped (1,2,3...)
    "seq"                 INTEGER NOT NULL DEFAULT 0, -- AEP seq (informational)
    "role"                TEXT    NOT NULL,            -- 'user' | 'assistant'
    "content"             TEXT    NOT NULL DEFAULT '',
    "platform"            TEXT    NOT NULL DEFAULT '',
    "user_id"             TEXT    NOT NULL DEFAULT '',
    "model"               TEXT    NOT NULL DEFAULT '',
    "success"             BOOLEAN,                    -- NULL for user turns
    "source"              TEXT    NOT NULL DEFAULT 'normal',
    "tools_json"          TEXT,                       -- {"Read":2,"Bash":1}
    "tool_count"          INTEGER NOT NULL DEFAULT 0,
    "tokens_input"        INTEGER NOT NULL DEFAULT 0,
    "tokens_cache_write"  INTEGER NOT NULL DEFAULT 0,
    "tokens_cache_read"   INTEGER NOT NULL DEFAULT 0,
    "tokens_out"          INTEGER NOT NULL DEFAULT 0,
    "duration_ms"         INTEGER NOT NULL DEFAULT 0,
    "cost_usd"            NUMERIC(18,8) NOT NULL DEFAULT 0.0,
    "created_at"          BIGINT NOT NULL               -- Unix ms
);

CREATE INDEX "idx_turns_session_gen_id"
    ON "turns"("session_id", "generation", "id");

CREATE INDEX "idx_turns_created"
    ON "turns"("created_at");

-- +goose Down
DROP TABLE IF EXISTS "turns";
