-- +goose Up
-- Fix: v_turns_assistant tokens_in now sums raw usage cache fields for Claude Code events.
-- Anthropic API: total_input = input_tokens + cache_creation_input_tokens + cache_read_input_tokens.
-- OCS events (no stats.usage.*) fall through to ELSE branch (already correct).
-- Idempotent: always computes from immutable raw fields, not from the _session snapshot.

DROP VIEW IF EXISTS v_turns;
DROP VIEW IF EXISTS v_turns_assistant;

CREATE VIEW v_turns_assistant AS
SELECT
  d.session_id,
  d.seq,
  'assistant' AS role,
  COALESCE(m.content, '') AS content,
  COALESCE(s.platform, '') AS platform,
  COALESCE(s.owner_id, '') AS user_id,
  COALESCE(json_extract(d.data, '$.stats._session.model_name'), '') AS model,
  json_extract(d.data, '$.success') AS success,
  d.source,
  json_extract(d.data, '$.stats._session.tool_names') AS tools_json,
  COALESCE(json_extract(d.data, '$.stats._session.tool_call_count'), 0) AS tool_call_count,
  CASE
    WHEN json_extract(d.data, '$.stats.usage.input_tokens') IS NOT NULL THEN
      COALESCE(json_extract(d.data, '$.stats.usage.input_tokens'), 0)
      + COALESCE(json_extract(d.data, '$.stats.usage.cache_creation_input_tokens'), 0)
      + COALESCE(json_extract(d.data, '$.stats.usage.cache_read_input_tokens'), 0)
    ELSE COALESCE(json_extract(d.data, '$.stats._session.turn_input_tok'), 0)
  END AS tokens_in,
  COALESCE(json_extract(d.data, '$.stats._session.turn_output_tok'), 0) AS tokens_out,
  COALESCE(json_extract(d.data, '$.stats._session.turn_duration_ms'), 0) AS duration_ms,
  COALESCE(json_extract(d.data, '$.stats._session.turn_cost_usd'), 0.0) AS cost_usd,
  d.created_at
FROM events d
LEFT JOIN sessions s ON s.id = d.session_id
LEFT JOIN (
  SELECT grouped.session_id, grouped.next_done_id,
    group_concat(json_extract(grouped.data, '$.content'), char(10)) AS content
  FROM (
    SELECT id, session_id, type, data,
      MIN(CASE WHEN type = 'done' THEN id END) OVER (
        PARTITION BY session_id ORDER BY id ROWS BETWEEN CURRENT ROW AND UNBOUNDED FOLLOWING
      ) AS next_done_id
    FROM events
    WHERE type IN ('message', 'done')
  ) grouped
  WHERE grouped.type = 'message' AND grouped.next_done_id IS NOT NULL
  GROUP BY grouped.session_id, grouped.next_done_id
) m ON m.session_id = d.session_id AND m.next_done_id = d.id
WHERE d.type = 'done' AND d.direction = 'outbound';

CREATE VIEW v_turns AS
SELECT * FROM v_turns_user
UNION ALL
SELECT * FROM v_turns_assistant
ORDER BY session_id, created_at, role DESC;

-- +goose Down

DROP VIEW IF EXISTS v_turns;
DROP VIEW IF EXISTS v_turns_assistant;

CREATE VIEW v_turns_assistant AS
SELECT
  d.session_id,
  d.seq,
  'assistant' AS role,
  COALESCE(m.content, '') AS content,
  COALESCE(s.platform, '') AS platform,
  COALESCE(s.owner_id, '') AS user_id,
  COALESCE(json_extract(d.data, '$.stats._session.model_name'), '') AS model,
  json_extract(d.data, '$.success') AS success,
  d.source,
  json_extract(d.data, '$.stats._session.tool_names') AS tools_json,
  COALESCE(json_extract(d.data, '$.stats._session.tool_call_count'), 0) AS tool_call_count,
  COALESCE(json_extract(d.data, '$.stats._session.turn_input_tok'), 0) AS tokens_in,
  COALESCE(json_extract(d.data, '$.stats._session.turn_output_tok'), 0) AS tokens_out,
  COALESCE(json_extract(d.data, '$.stats._session.turn_duration_ms'), 0) AS duration_ms,
  COALESCE(json_extract(d.data, '$.stats._session.turn_cost_usd'), 0.0) AS cost_usd,
  d.created_at
FROM events d
LEFT JOIN sessions s ON s.id = d.session_id
LEFT JOIN (
  SELECT grouped.session_id, grouped.next_done_id,
    group_concat(json_extract(grouped.data, '$.content'), char(10)) AS content
  FROM (
    SELECT id, session_id, type, data,
      MIN(CASE WHEN type = 'done' THEN id END) OVER (
        PARTITION BY session_id ORDER BY id ROWS BETWEEN CURRENT ROW AND UNBOUNDED FOLLOWING
      ) AS next_done_id
    FROM events
    WHERE type IN ('message', 'done')
  ) grouped
  WHERE grouped.type = 'message' AND grouped.next_done_id IS NOT NULL
  GROUP BY grouped.session_id, grouped.next_done_id
) m ON m.session_id = d.session_id AND m.next_done_id = d.id
WHERE d.type = 'done' AND d.direction = 'outbound';

CREATE VIEW v_turns AS
SELECT * FROM v_turns_user
UNION ALL
SELECT * FROM v_turns_assistant
ORDER BY session_id, created_at, role DESC;
