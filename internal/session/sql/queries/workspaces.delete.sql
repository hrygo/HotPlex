-- workspaces.delete: hard delete (spec §9.1, after verifying no active sessions).
DELETE FROM workspaces WHERE id = ?
