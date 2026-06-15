-- workspaces.count_active_sessions: guard for delete (spec §9.1 — reject if > 0).
SELECT COUNT(*) FROM sessions WHERE workspace_id = ? AND state IN ('created','running','idle')
