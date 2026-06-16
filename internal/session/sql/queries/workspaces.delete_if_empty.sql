-- workspaces.delete_if_empty: 原子删除——仅当无活跃会话时成功（防 Count↔Delete TOCTOU，spec §9.1）。
-- 调用方先 GetWorkspaceByID 确认存在 + 归属，故 RowsAffected=0 即 TOCTOU 命中（期间有新活跃会话）。
DELETE FROM workspaces WHERE id = ? AND NOT EXISTS (
  SELECT 1 FROM sessions WHERE workspace_id = ? AND state IN ('created','running','idle')
)
