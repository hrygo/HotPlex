-- users.touch_last_login: record last login timestamp (non-critical on login success).
UPDATE users SET last_login_at = ?, updated_at = ? WHERE id = ?
