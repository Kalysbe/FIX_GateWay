-- Retain the last successful snapshot while idle or temporarily unavailable.
CREATE OR REPLACE FUNCTION gateway_dashboard_snapshot() RETURNS jsonb
LANGUAGE sql STABLE AS $$
 SELECT payload || jsonb_build_object('stale', refreshed_at <= now()-interval '5 minutes')
 FROM gateway_dashboard_cache;
$$;
