WITH latest_gen AS (
    SELECT COALESCE(MAX(generation), 0) AS gen FROM turns WHERE session_id = ?
)
SELECT lg.gen, t.turn_num, t.seq, t.success, t.source,
       t.tools_json, t.tool_count,
       t.tokens_input, t.tokens_cache_write, t.tokens_cache_read,
       (t.tokens_input + t.tokens_cache_write + t.tokens_cache_read) AS tokens_in,
       t.tokens_out, t.duration_ms, t.cost_usd, t.model, t.created_at
FROM turns t, latest_gen lg
WHERE t.session_id = ? AND t.generation = lg.gen AND t.role = 'assistant'
ORDER BY t.id ASC
