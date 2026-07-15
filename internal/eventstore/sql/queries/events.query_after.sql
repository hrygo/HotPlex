SELECT id, session_id, seq, type, data, direction, source, created_at
FROM events
WHERE session_id = ? AND id > ?
ORDER BY id ASC
LIMIT ?
