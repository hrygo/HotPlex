-- workspaces.list_by_owner: list a user's active workspaces (private; spec §9.1).
-- LIMIT/OFFSET 下推到 store 层（PR #773 P2）：跨通道租户接入后 api-key 通道可能程序化
-- 批量创建 workspace，内存分页会退化为无界查询。
SELECT id, owner_user_id, name, work_dir, agent_config_overrides, worker_preference, status, created_at, updated_at, permission_mode
FROM workspaces WHERE owner_user_id = ? AND status = 'active' ORDER BY created_at ASC LIMIT ? OFFSET ?
