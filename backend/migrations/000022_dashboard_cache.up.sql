-- Persist the expensive daily aggregate so application restarts remain fast.
ALTER FUNCTION gateway_dashboard_snapshot() RENAME TO gateway_dashboard_compute;
CREATE TABLE gateway_dashboard_cache (
 id boolean PRIMARY KEY DEFAULT true CHECK (id),
 payload jsonb NOT NULL,
 refreshed_at timestamptz NOT NULL
);

CREATE FUNCTION gateway_refresh_dashboard_snapshot() RETURNS void
LANGUAGE plpgsql AS $$
DECLARE snapshot jsonb;
BEGIN
 -- At most one refresh across all gateway processes. Never queue refreshes.
 IF NOT pg_try_advisory_xact_lock(2147483000,21) THEN RETURN; END IF;
 IF EXISTS (SELECT 1 FROM gateway_dashboard_cache WHERE refreshed_at > clock_timestamp()-interval '60 seconds') THEN RETURN; END IF;
 snapshot:=gateway_dashboard_compute();
 INSERT INTO gateway_dashboard_cache(id,payload,refreshed_at)
 VALUES (true,snapshot,clock_timestamp())
 ON CONFLICT(id) DO UPDATE SET payload=excluded.payload,refreshed_at=excluded.refreshed_at;
END;
$$;

CREATE FUNCTION gateway_dashboard_snapshot() RETURNS jsonb
LANGUAGE sql STABLE AS $$
 SELECT payload FROM gateway_dashboard_cache
 WHERE refreshed_at > now()-interval '5 minutes';
$$;

-- Warm once during migration, rather than making the first visitor wait.
SELECT gateway_refresh_dashboard_snapshot();
