-- store.get_permission_ceiling returns the immutable permission ceiling.
SELECT COALESCE(permission_ceiling, '')
FROM sessions
WHERE id = ?;
