-- Match the notification inbox ordering; the old time-only index forced a
-- scan and sort of the entire unread inbox for every eight-row bell refresh.
CREATE INDEX IF NOT EXISTS idx_obs_notifications_priority_unread
    ON obs_notifications ((CASE severity WHEN 'CRITICAL' THEN 0 WHEN 'ERROR' THEN 1 WHEN 'WARNING' THEN 2 ELSE 3 END), last_seen_at DESC)
    WHERE read_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_obs_notifications_priority
    ON obs_notifications ((CASE severity WHEN 'CRITICAL' THEN 0 WHEN 'ERROR' THEN 1 WHEN 'WARNING' THEN 2 ELSE 3 END), last_seen_at DESC);
