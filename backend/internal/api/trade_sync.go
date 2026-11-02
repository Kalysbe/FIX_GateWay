package api

import (
	"context"
	"time"

	"github.com/fix-gateway/backend/internal/metrics"
	"github.com/fix-gateway/backend/internal/quikexport"
)

// RunTradeReconciliation loads trades from the QUIK table public."Trades", read
// directly over the read-only QUIK database connection, on startup and every
// minute. It is the only source of the trade ledger.
// Failures remain visible in trade_sync_runs and the admin coverage response.
// Without the QUIK connection each run fails visibly (ErrNotConfigured).
func (s *Server) RunTradeReconciliation(ctx context.Context, quik *quikexport.Store) {
	if s.tradeLedger == nil || s.tradeLedger.Pool == nil {
		return
	}
	metrics.TradeReconciliationEnabled.Set(1)
	go s.runTradeSafetyMonitor(ctx)
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
