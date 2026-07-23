UPDATE sessions
SET context_json = json_set(
  COALESCE(NULLIF(context_json, ''), '{}'),
  '$._agent_spec',
  json(?)
)
WHERE id = ?;
