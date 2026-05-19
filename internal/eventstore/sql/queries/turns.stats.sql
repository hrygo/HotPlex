SELECT turn_num, seq, success, source,
       tools_json, tool_count,
       tokens_input, tokens_cache_write, tokens_cache_read,
       (tokens_input + tokens_cache_write + tokens_cache_read) AS tokens_in,
       tokens_out, duration_ms, cost_usd, model, created_at
FROM turns
WHERE session_id = ? AND generation = ? AND role = 'assistant'
ORDER BY id ASC
