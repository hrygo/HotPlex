-- +goose Up
-- issue #879 problem 2: enforce (session_id, seq) uniqueness so seq collisions
-- (left by pre-hydration WS reconnects, before PR1's SeqGen hydration shipped)
-- surface as insert errors instead of silent corruption.
--
-- Dedup first: per (session_id, seq) pair keep only the newest row (MAX(id)).
-- Without this, CREATE UNIQUE INDEX fails on the pre-existing duplicates. The
-- MAX(id) keeps the most recently inserted row for each colliding pair, which
-- is the post-reset (newest) event once PR1's hydration prevents future resets.
DELETE FROM events
WHERE id NOT IN (
    SELECT MAX(id) FROM events GROUP BY session_id, seq
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_events_session_seq_unique
    ON events(session_id, seq);

-- +goose Down
DROP INDEX IF EXISTS idx_events_session_seq_unique;
