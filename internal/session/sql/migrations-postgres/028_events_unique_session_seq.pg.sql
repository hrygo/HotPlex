-- +goose Up
-- Preserve every legacy row, including valid events that share a seq because
-- an older gateway restarted its in-memory counter on reconnect. Each legacy
-- duplicate row gets a distinct guard value while one canonical row per
-- (session_id, seq) keeps guard 0. New inserts also use guard 0, so they collide
-- with every sequence already present as well as with other new inserts.
ALTER TABLE "events" ADD COLUMN "seq_guard_id" BIGINT NOT NULL DEFAULT 0;

UPDATE "events" SET "seq_guard_id" = "id";
UPDATE "events" SET "seq_guard_id" = 0
WHERE "id" IN (
    SELECT MIN("id") FROM "events" GROUP BY "session_id", "seq"
);

CREATE UNIQUE INDEX IF NOT EXISTS "idx_events_session_seq_unique"
    ON "events"("session_id", "seq_guard_id", "seq");

-- +goose Down
DROP INDEX IF EXISTS "idx_events_session_seq_unique";
ALTER TABLE "events" DROP COLUMN "seq_guard_id";
