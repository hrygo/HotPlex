-- workspaces.update: update mutable fields. work_dir is updatable (workspace-level),
-- BUT only when no active session is bound to the workspace — work_dir enters
-- DeriveSessionKey (key.go), so shifting it with a live session orphans that
-- session's history. The handler pre-checks via CountActiveSessionsInWorkspace,
-- but that read and this write are separate calls; a concurrent WS handshake can
-- insert a 'running' session in between, and that insert doesn't touch updated_at,
-- so the CAS below wouldn't catch it. The `AND (? = work_dir OR NOT EXISTS <active>)`
-- clause closes that TOCTOU atomically (mirrors workspaces.delete_if_empty):
--   - ? = work_dir  → work_dir unchanged (new bind == old column value): allow regardless
--   - NOT EXISTS    → work_dir is changing: require zero active sessions
-- The `AND updated_at = ?` is the optimistic-concurrency guard (review P3-1): the
-- last bind arg is the caller's cached updated_at. Args (in order): name,
-- agent_config_overrides, worker_preference, work_dir(new), updated_at(now),
-- id, updated_at(cached), work_dir(new, for the ?=work_dir compare), workspace_id.
UPDATE workspaces SET
  name = ?, agent_config_overrides = ?, worker_preference = ?, work_dir = ?, updated_at = ?
WHERE id = ? AND updated_at = ?
  AND (? = work_dir OR NOT EXISTS (
    SELECT 1 FROM sessions WHERE workspace_id = ? AND state IN ('created','running','idle')
  ))
