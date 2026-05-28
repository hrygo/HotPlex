SELECT COALESCE(MAX(turn_num), 0) FROM turns WHERE session_id = ? AND generation = ?
