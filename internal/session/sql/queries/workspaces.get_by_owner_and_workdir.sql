-- workspaces.get_by_owner_and_workdir: per-user 1:1 lookup (SwitchWorkDir remap, spec §9.4).
SELECT id, owner_user_id, name, work_dir, agent_config_overrides, worker_preference, status, created_at, updated_at, permission_mode
FROM workspaces WHERE owner_user_id = ? AND work_dir = ? AND status = 'active'
