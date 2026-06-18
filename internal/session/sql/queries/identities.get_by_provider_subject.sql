-- identities.get_by_provider_subject: lookup OAuth identity by (provider, sub) for SSO login.
SELECT id, user_id, provider, subject, display_name, email, created_at, updated_at
FROM user_identities WHERE provider = ? AND subject = ?
