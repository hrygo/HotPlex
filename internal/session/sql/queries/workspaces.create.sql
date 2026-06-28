-- workspaces.create: insert a new workspace. agent_config_overrides/worker_preference/permission_mode start NULL
-- (spec ②③ fill later; permission_mode NULL scans as "" = "worker default"; bridge injects only explicit overrides, issue #789).
INSERT INTO workspaces (id, owner_user_id, name, work_dir, agent_config_overrides, worker_preference, status, created_at, updated_at, permission_mode)
VALUES (?, ?, ?, ?, NULL, NULL, 'active', ?, ?, ?)
