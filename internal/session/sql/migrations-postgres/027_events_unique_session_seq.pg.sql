-- +goose Up
-- issue #879 problem 2: enforce (session_id, seq) uniqueness so seq collisions
-- surface as insert errors instead of silent corruption.
--
-- Dedup first: per (session_id, seq) pair keep only the newest row (MAX(id)) so
-- CREATE UNIQUE INDEX succeeds on existing data; MAX(id) keeps the most recently
-- inserted (post-reset, newest) row for each colliding pair.
DELETE FROM "events"
WHERE "id" NOT IN (
    SELECT MAX("id") FROM "events" GROUP BY "session_id", "seq"
);

CREATE UNIQUE INDEX IF NOT EXISTS "idx_events_session_seq_unique"
    ON "events"("session_id", "seq");

-- +goose Down
DROP INDEX IF EXISTS "idx_events_session_seq_unique";
