-- delete_terminated removes terminated sessions older than the respective cutoffs.
-- cronCutoff applies to source='cron'; defaultCutoff applies to all other sessions.
-- Events lifecycle is managed independently — session deletion does not cascade.
DELETE FROM sessions WHERE state = ? AND (
    (source = 'cron' AND updated_at <= ?) OR
    (source != 'cron' AND updated_at <= ?)
);
