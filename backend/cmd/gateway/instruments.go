package main

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/fix-gateway/backend/internal/domain"
	"github.com/fix-gateway/backend/internal/refdata"
)

type instrumentManager interface {
	AvailableCount() int
	FetchSecurities(context.Context) (string, []domain.SecurityRef, error)
	MarketDataSubscription() domain.MarketDataSubscription
}

type instrumentReference interface {
	SyncFromFIX(context.Context, []domain.SecurityRef) (refdata.SyncResult, error)
}

func syncInstrumentsAfterLogon(ctx context.Context, log *slog.Logger, manager instrumentManager, store instrumentReference) {
	syncInstrumentLoop(ctx, log, manager, store, 5*time.Second, 5*time.Minute, time.Minute)
}

// Run separately from market-data supervision so a slow dictionary response
// does not delay quotes or recovery of healthy subscriptions.
func syncInstrumentLoop(ctx context.Context, log *slog.Logger, manager instrumentManager, store instrumentReference, poll, refresh, cooldown time.Duration) {
	var lastAttempt, lastSuccess time.Time
	var syncedAdapter string
	run := func() {
		if ctx.Err() != nil || manager.AvailableCount() == 0 {
			return
		}
		now := time.Now()
		if !lastAttempt.IsZero() && now.Sub(lastAttempt) < cooldown {
			return
		}
		state := manager.MarketDataSubscription()
		unknownClass := strings.Contains(strings.ToLower(state.Error), "unknown exdestination")
		ownerChanged := state.AdapterID != "" && state.AdapterID != syncedAdapter
		if !lastSuccess.IsZero() && now.Sub(lastSuccess) < refresh && !unknownClass && !ownerChanged {
			return
		}
		lastAttempt = now
		defer func() { lastAttempt = time.Now() }()
		work, cancel := context.WithTimeout(ctx, 90*time.Second)
		defer cancel()
		adapterID, items, err := manager.FetchSecurities(work)
		if err != nil || len(items) == 0 {
			log.Warn("FIX instrument sync failed; keeping existing dictionary", "adapter_id", adapterID, "error", err, "instruments", len(items))
			return
		}
		n, err := store.SyncFromFIX(work, items)
		if err != nil {
			log.Warn("FIX instrument upsert failed", "error", err)
			return
		}
		lastSuccess, syncedAdapter = time.Now(), adapterID
		log.Info("instruments synced via FIX Security Definition", "adapter_id", adapterID,
			"upserted", n.InstrumentsUpserted, "removed", n.InstrumentsRemoved, "inactive", n.InstrumentsInactive)
	}
	run()
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}
