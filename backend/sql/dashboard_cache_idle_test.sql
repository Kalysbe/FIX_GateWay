-- Regression: five minutes without visitors must not turn the snapshot into NULL.
BEGIN;
UPDATE gateway_dashboard_cache SET refreshed_at=now()-interval '10 minutes';
DO $$
DECLARE snapshot jsonb;
BEGIN
 snapshot:=gateway_dashboard_snapshot();
 IF snapshot IS NULL OR snapshot->>'stale'<>'true' OR snapshot->'stats' IS NULL THEN
  RAISE EXCEPTION 'idle cache lost its saved data: %',snapshot;
 END IF;
END $$;
UPDATE gateway_dashboard_cache SET refreshed_at=now();
DO $$
BEGIN
 IF gateway_dashboard_snapshot()->>'stale'<>'false' THEN
  RAISE EXCEPTION 'fresh snapshot incorrectly marked stale';
 END IF;
END $$;
ROLLBACK;
