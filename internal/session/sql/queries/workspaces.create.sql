-- workspaces.create: insert a new workspace. agent_config_overrides/worker_preference start NULL (spec ②③ fill later).
INSERT INTO workspaces (id, owner_user_id, name, work_dir, agent_config_overrides, worker_preference, status, created_at, updated_at)
VALUES (?, ?, ?, ?, NULL, NULL, 'active', ?, ?)
