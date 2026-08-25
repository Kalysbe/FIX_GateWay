DROP TABLE IF EXISTS obs_export_jobs;
DROP TABLE IF EXISTS obs_notifications;
DROP TABLE IF EXISTS obs_events;

ALTER TABLE orders
    DROP COLUMN IF EXISTS request_id,
    DROP COLUMN IF EXISTS trace_id;
