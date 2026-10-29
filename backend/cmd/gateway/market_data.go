package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/fix-gateway/backend/internal/domain"
)

type marketDataManager interface {
	SubscribeMarketData(context.Context, domain.MarketDataQuery) (string, string, []domain.MarketQuote, error)
	MarketDataSubscription() domain.MarketDataSubscription
}
type marketDataReference interface {
	ListActiveInstruments(context.Context) ([]domain.SecurityRef, error)
}
type marketDataPublisher interface{ Broadcast(string, any) }

// The supervisor belongs to the gateway, never to a browser request. Each pass
// reconciles the dictionary, rejects and connection state; healthy streams are
// retained by the adapter, including quiet or closed markets. Clients receive
// market data from QUIK Params, not from this FIX subscription, and FIX
// snapshots are not stored in the database.
func syncMarketDataLoop(ctx context.Context, log *slog.Logger, manager marketDataManager, refs marketDataReference, interval time.Duration, boards []string) {
	if interval <= 0 || interval > 5*time.Second {
		interval = 5 * time.Second
	}
	var lastError string
	run := func() {
		work, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		list, err := refs.ListActiveInstruments(work)
		if err == nil && len(list) == 0 {
			err = fmt.Errorf("instrument dictionary is not ready")
		}
		if err == nil {
			_, _, _, err = manager.SubscribeMarketData(work, domain.MarketDataQuery{Boards: boards, Instruments: list})
		}
		if err != nil && err.Error() != lastError && ctx.Err() == nil {
			log.Warn("FIX market data automatic recovery", "error", err)
		}
		lastError = ""
		if err != nil {
			lastError = err.Error()
		}
	}
	run()
	ticker := time.NewTicker(interval)
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
