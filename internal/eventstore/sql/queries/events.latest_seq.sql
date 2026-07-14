-- Latest event seq for a session, or 0 if none. Used to hydrate the in-memory
-- SeqGen on reconnect so seq continues monotonically instead of restarting
-- from 1 (issue #879: seq reset → collision + new events buried by
-- ORDER BY seq DESC queries).
SELECT COALESCE(MAX(seq), 0) FROM events WHERE session_id = ?
