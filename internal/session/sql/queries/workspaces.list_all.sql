-- workspaces.list_all: list all active workspaces (for startup validation scan, spec ② #749).
SELECT id, owner_user_id, name, work_dir, agent_config_overrides, worker_preference, status, created_at, updated_at
FROM workspaces WHERE status = 'active' ORDER BY created_at ASC
