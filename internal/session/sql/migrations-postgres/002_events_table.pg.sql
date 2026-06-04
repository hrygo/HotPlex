-- +goose Up
CREATE TABLE IF NOT EXISTS "events" (
    "id" BIGSERIAL PRIMARY KEY,
    "session_id" TEXT NOT NULL,
    "seq" INTEGER NOT NULL,
    "type" TEXT NOT NULL,
    "data" TEXT NOT NULL,
    "direction" TEXT NOT NULL DEFAULT 'outbound',
    "source" TEXT NOT NULL DEFAULT 'normal'
      CHECK(source IN ('normal', 'crash', 'timeout', 'fresh_start')),
    "created_at" BIGINT NOT NULL
);

CREATE INDEX IF NOT EXISTS "idx_events_session_seq" ON "events"("session_id", "seq");
CREATE INDEX IF NOT EXISTS "idx_events_created" ON "events"("created_at");

-- +goose Down
DROP TABLE IF EXISTS "events";
