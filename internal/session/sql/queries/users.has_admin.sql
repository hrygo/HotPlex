-- users.has_admin: returns 1 if any admin-role user exists (bootstrap detection).
-- Public, parameterless — used by GET /api/auth/bootstrap-status to guide first-time setup.
SELECT 1 FROM users WHERE role = 'admin' LIMIT 1
