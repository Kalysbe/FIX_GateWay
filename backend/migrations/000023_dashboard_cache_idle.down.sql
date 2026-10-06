CREATE OR REPLACE FUNCTION gateway_dashboard_snapshot() RETURNS jsonb
LANGUAGE sql STABLE AS $$
 SELECT payload FROM gateway_dashboard_cache
 WHERE refreshed_at > now()-interval '5 minutes';
$$;
