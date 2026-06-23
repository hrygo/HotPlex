-- workspaces.update: update mutable fields. work_dir is NOT updatable (immutable, spec §6.2).
-- The `AND updated_at = ?` clause is an optimistic-concurrency guard (review P3-1):
-- concurrent updates race on the same row, and the loser (whose cached updated_at
-- no longer matches) affects 0 rows and surfaces as ErrWorkspaceConflict → 409,
-- preventing a silent lost update. The last bind arg is the caller's cached updated_at.
UPDATE workspaces SET name = ?, agent_config_overrides = ?, worker_preference = ?, updated_at = ? WHERE id = ? AND updated_at = ?
