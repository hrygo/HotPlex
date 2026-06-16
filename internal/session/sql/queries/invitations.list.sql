-- invitations.list: list all invitations (admin only), newest first, paginated (spec §11.2).
SELECT id, code, created_by, role, used_by, expires_at, created_at, used_at
FROM invitations ORDER BY created_at DESC LIMIT ? OFFSET ?
