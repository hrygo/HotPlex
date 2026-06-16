-- invitations.mark_used: CAS consume — only succeeds if used_by IS NULL (prevents replay, spec §8.6).
-- Returns 0 rows affected if already used → caller maps to INVITATION_USED.
UPDATE invitations SET used_by = ?, used_at = ? WHERE id = ? AND used_by IS NULL
