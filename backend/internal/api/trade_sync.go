package api

import (
	"context"
	"time"

	"github.com/fix-gateway/backend/internal/metrics"
	"github.com/fix-gateway/backend/internal/quikexport"
)

// RunTradeReconciliation restores local evidence, then compares with the QUIK
// trade table public."Trades", read directly over the read-only QUIK database
// connection, on startup and every minute.
// Failures remain visible in trade_sync_runs and the admin coverage response.
// Without the QUIK connection each run fails visibly (ErrNotConfigured).
func (s *Server) RunTradeReconciliation(ctx context.Context, quik *quikexport.Store) {
	if s.tradeLedger == nil || s.tradeLedger.Pool == nil {
		return
	}
	metrics.TradeReconciliationEnabled.Set(1)
	go s.runTradeSafetyMonitor(ctx)
	init, cancel := context.WithTimeout(ctx, 10*time.Minute)
	n, bad, err := s.tradeLedger.Backfill(init)
	cancel()
	if err != nil || bad > 0 {
		s.log.Error("trade ledger backfill needs reconciliation", "reports", n, "errors", bad, "error", err)
	}
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		work, stop := context.WithTimeout(ctx, 4*time.Minute)
		_, problem := s.tradeLedger.Sync(work, quik)
		stop()
		if problem != nil {
			s.log.Error("QUIK trade reconciliation failed; source evidence retained", "error", problem)
		}
		if s.hub != nil {
			s.hub.Broadcast("trade.created", map[string]any{"source": "quik_reconciliation"})
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
