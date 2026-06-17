-- identities.create: insert a new OAuth identity binding.
INSERT INTO user_identities (id, user_id, provider, subject, display_name, email, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
