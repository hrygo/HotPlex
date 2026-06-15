-- users.list: list users with pagination (admin only).
SELECT id, username, password_hash, role, display_name, status, created_at, updated_at, last_login_at
FROM users ORDER BY created_at ASC LIMIT ? OFFSET ?
