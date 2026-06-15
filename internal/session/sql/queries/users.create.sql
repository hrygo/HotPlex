-- users.create: insert a new user. last_login_at starts NULL.
INSERT INTO users (id, username, password_hash, role, display_name, status, created_at, updated_at, last_login_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, NULL)
