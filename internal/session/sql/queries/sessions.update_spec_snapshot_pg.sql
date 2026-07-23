UPDATE sessions
SET context_json = jsonb_set(
  COALESCE(NULLIF(context_json, ''), '{}')::jsonb,
  '{_agent_spec}',
  ?::jsonb,
  true
)::text
WHERE id = ?;
