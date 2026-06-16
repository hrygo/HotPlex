-- users.get_by_username: load a user by username (login lookup).
SELECT id, username, password_hash, role, display_name, status, created_at, updated_at, last_login_at
FROM users WHERE username = ?
