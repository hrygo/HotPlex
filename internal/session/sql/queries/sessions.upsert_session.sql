INSERT INTO sessions (id, user_id, owner_id, bot_id, bot_name, worker_session_id, worker_type, state, platform, platform_key_json, work_dir, title, created_at, updated_at, expires_at, idle_expires_at, context_json, source, client_key)
 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
 ON CONFLICT(id) DO UPDATE SET
-- worker_session_id intentionally excluded: managed by UpdateWorkerSessionIDSQL
  state=excluded.state,
  owner_id=CASE WHEN excluded.owner_id != '' THEN excluded.owner_id ELSE sessions.owner_id END,
  bot_name=CASE WHEN excluded.bot_name != '' THEN excluded.bot_name ELSE sessions.bot_name END,
  updated_at=excluded.updated_at,
  expires_at=excluded.expires_at,
  idle_expires_at=excluded.idle_expires_at,
  title=CASE WHEN excluded.title != '' THEN excluded.title ELSE sessions.title END,
  context_json=excluded.context_json,
  source=CASE WHEN excluded.source != '' THEN excluded.source ELSE sessions.source END,
  client_key=CASE WHEN excluded.client_key != '' THEN excluded.client_key ELSE sessions.client_key END;
