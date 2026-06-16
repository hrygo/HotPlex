-- invitations.create: insert a one-time invite code.
INSERT INTO invitations (id, code, created_by, role, used_by, expires_at, created_at, used_at)
VALUES (?, ?, ?, ?, NULL, ?, ?, NULL)
