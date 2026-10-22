package api

import (
	"context"
	"time"

	"github.com/fix-gateway/backend/internal/metrics"
)

// RunTradeReconciliation restores local evidence, then compares with the
// explicitly configured, read-only QUIK trade source on startup and every minute.
// Failures remain visible in trade_sync_runs and the admin coverage response.
func (s *Server) RunTradeReconciliation(ctx context.Context) {
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
		base, _, problem := s.quikAPI.Settings(work)
		if problem == nil {
			_, problem = s.tradeLedger.Sync(work, base)
		}
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
