DROP FUNCTION IF EXISTS gateway_dashboard_snapshot();
DROP FUNCTION IF EXISTS gateway_refresh_dashboard_snapshot();
DROP TABLE IF EXISTS gateway_dashboard_cache;
ALTER FUNCTION gateway_dashboard_compute() RENAME TO gateway_dashboard_snapshot;
