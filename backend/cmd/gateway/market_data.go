package main

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
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
type marketDataPersistence interface {
	BeginSnapshot(context.Context, string, string) (int64, error)
	InsertQuotes(context.Context, int64, []domain.MarketQuote) error
	CompleteSnapshot(context.Context, int64, string, string, int) error
}
type marketDataPublisher interface{ Broadcast(string, any) }

// The supervisor belongs to the gateway, never to a browser request. Each pass
// reconciles the dictionary, rejects and connection state; healthy streams are
// retained by the adapter, including quiet or closed markets.
func syncMarketDataLoop(ctx context.Context, log *slog.Logger, manager marketDataManager, store marketDataPersistence, refs marketDataReference, hub marketDataPublisher, interval time.Duration, boards []string) {
	if interval <= 0 || interval > 5*time.Second {
		interval = 5 * time.Second
	}
	var lastError, lastSaved, lastState string
	run := func() {
		work, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		list, err := refs.ListActiveInstruments(work)
		var adapterID, reqID string
		var items []domain.MarketQuote
		if err == nil && len(list) == 0 {
			err = fmt.Errorf("instrument dictionary is not ready")
		}
		if err == nil {
			adapterID, reqID, items, err = manager.SubscribeMarketData(work, domain.MarketDataQuery{Boards: boards, Instruments: list})
		}
		state := manager.MarketDataSubscription()
		if err != nil && state.Error == "" {
			state.Error = err.Error()
		}
		if err != nil && err.Error() != lastError && ctx.Err() == nil {
			log.Warn("FIX market data automatic recovery", "error", err)
		}
		lastError = ""
		if err != nil {
			lastError = err.Error()
		}
		stateKey := fmt.Sprintf("%s|%s|%d|%d|%s", state.AdapterID, state.Status, state.Expected, state.Confirmed, state.Error)
		if stateKey != lastState {
			hub.Broadcast("marketdata.tick", map[string]any{"subscription": state, "quotes": items, "replace": true})
			lastState = stateKey
		}
		var received []domain.MarketQuote
		var receivedKeys []string
		for _, row := range items {
			if row.HasData() {
				received = append(received, row)
				receivedKeys = append(receivedKeys, row.Board+"|"+row.SecurityID+"|"+row.Symbol+"|"+row.MDReqID)
			}
		}
		// Persist only genuine responses, once per subscription/coverage change.
		sort.Strings(receivedKeys)
		saveKey := adapterID + "|" + strings.Join(receivedKeys, ";")
		if len(received) > 0 && saveKey != lastSaved {
			if id, saveErr := store.BeginSnapshot(work, adapterID, reqID); saveErr == nil {
				if saveErr = store.InsertQuotes(work, id, received); saveErr == nil {
					saveErr = store.CompleteSnapshot(work, id, "OK", "", len(received))
				}
				if saveErr == nil {
					lastSaved = saveKey
				} else {
					log.Warn("market data snapshot persistence failed", "error", saveErr)
				}
			}
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
