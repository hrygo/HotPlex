-- identities.update_profile: sync display_name/email from IdP on each login.
UPDATE user_identities SET display_name = ?, email = ?, updated_at = ? WHERE id = ?
