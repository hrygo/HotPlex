-- workspaces.list_with_owner: admin console global view (spec §3.1, issue #807).
-- Joins users for readable owner identity (display_name + username) so admins
-- scanning the global list don't face raw UUIDs. LEFT JOIN tolerates a raced
-- owner-user deletion window (workspace stays visible, owner fields → '');
-- the FK normally guarantees the owner row exists. COALESCE keeps scan types as
-- plain string (no NullString needed). Shared by SQLiteStore and pgStore (no
-- bind params, so dialect-identical).
SELECT w.id, w.owner_user_id, w.name, w.work_dir, w.agent_config_overrides,
       w.worker_preference, w.status, w.created_at, w.updated_at, w.permission_mode,
       COALESCE(u.display_name, ''), COALESCE(u.username, '')
FROM workspaces w
LEFT JOIN users u ON u.id = w.owner_user_id
WHERE w.status = 'active'
ORDER BY u.display_name, w.name
