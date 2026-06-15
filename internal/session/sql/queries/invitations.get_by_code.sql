-- invitations.get_by_code: lookup by invite code (accept-invite flow).
SELECT id, code, created_by, role, used_by, expires_at, created_at, used_at
FROM invitations WHERE code = ?
