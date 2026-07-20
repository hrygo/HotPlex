-- sessions.set_permission_ceiling_if_empty atomically captures the first
-- effective Worker permission ceiling and never overwrites it.
UPDATE sessions
SET permission_ceiling = ?
WHERE id = ?
  AND permission_ceiling = '';
