-- Apply only to the production FIX_GateWay database after its migrations.
-- Session ports, CompIDs and credentials require the operator's venue contract.
BEGIN;
UPDATE adapters
SET host = '172.23.10.84', reset_on_logon = false, updated_at = now()
WHERE type = 'arqa';
UPDATE adapters
SET enabled = false, updated_at = now()
WHERE type = 'simulator';
COMMIT;
