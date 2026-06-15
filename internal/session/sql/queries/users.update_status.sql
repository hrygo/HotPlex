-- users.update_status: enable/disable a user (admin only).
UPDATE users SET status = ?, updated_at = ? WHERE id = ?
