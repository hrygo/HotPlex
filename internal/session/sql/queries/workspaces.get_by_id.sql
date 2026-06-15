-- workspaces.get_by_id: load a workspace by ID.
SELECT id, owner_user_id, name, work_dir, agent_config_overrides, worker_preference, status, created_at, updated_at
FROM workspaces WHERE id = ?
