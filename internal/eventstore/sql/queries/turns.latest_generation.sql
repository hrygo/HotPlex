SELECT COALESCE(MAX(generation), 0) FROM turns WHERE session_id = ?
