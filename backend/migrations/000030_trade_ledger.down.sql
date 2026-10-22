-- Do not discard captured exchange history during an application rollback.
DO $$ BEGIN
  IF EXISTS(SELECT 1 FROM trade_reports) OR EXISTS(SELECT 1 FROM trades) THEN
    RAISE EXCEPTION 'trade ledger contains source evidence; use a forward migration';
  END IF;
END $$;
DROP TABLE trade_sync_runs, trade_side_reports, trade_sides, trade_reports, trades;
