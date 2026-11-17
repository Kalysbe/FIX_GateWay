-- Logs are kept in daily files, not in the database (поручение пользователя
-- 06.11.2026): <LOG_DIR>/YYYY-MM-DD.log, one file per Bishkek date, 7 days.
-- The dashboard counts today's activity from the file, so its SQL snapshot goes too.
DROP FUNCTION IF EXISTS gateway_dashboard_snapshot();
DROP FUNCTION IF EXISTS gateway_refresh_dashboard_snapshot();
DROP FUNCTION IF EXISTS gateway_dashboard_compute();
DROP TABLE IF EXISTS gateway_dashboard_cache;
DROP TABLE IF EXISTS obs_events CASCADE;
DROP TABLE IF EXISTS audit_logs;
