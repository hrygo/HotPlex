-- users.get_by_id: load a user by ID.
SELECT id, username, password_hash, role, display_name, status, created_at, updated_at, last_login_at
FROM users WHERE id = ?
