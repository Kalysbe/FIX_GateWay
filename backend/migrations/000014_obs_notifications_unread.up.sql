CREATE INDEX IF NOT EXISTS idx_obs_notifications_unread
    ON obs_notifications (last_seen_at DESC)
    WHERE read_at IS NULL;
