-- users.delete: permanently remove a user by ID (used for orphan cleanup).
DELETE FROM users WHERE id = ?
