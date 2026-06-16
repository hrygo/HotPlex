-- workspaces.update: update mutable fields. work_dir is NOT updatable (immutable, spec §6.2).
UPDATE workspaces SET name = ?, agent_config_overrides = ?, worker_preference = ?, updated_at = ? WHERE id = ?
