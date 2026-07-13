SELECT id FROM sessions WHERE state = ? AND (
    (source = 'cron' AND updated_at <= ?) OR
    (source != 'cron' AND updated_at <= ?)
);
