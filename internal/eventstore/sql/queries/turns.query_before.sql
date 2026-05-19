SELECT id, session_id, generation, turn_num, seq, role, content,
       platform, user_id, model, success, source, tools_json, tool_count,
       tokens_input, tokens_cache_write, tokens_cache_read,
       (tokens_input + tokens_cache_write + tokens_cache_read) AS tokens_in,
       tokens_out, duration_ms, cost_usd, created_at
FROM turns
WHERE session_id = ? AND id < ?
ORDER BY id DESC
LIMIT ?
