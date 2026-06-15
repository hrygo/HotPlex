-- workspaces.list_by_owner: list a user's active workspaces (private; spec §9.1).
SELECT id, owner_user_id, name, work_dir, agent_config_overrides, worker_preference, status, created_at, updated_at
FROM workspaces WHERE owner_user_id = ? AND status = 'active' ORDER BY created_at ASC
