package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/fix-gateway/backend/internal/domain"
	"github.com/fix-gateway/backend/internal/quikexport"
)

type marketDataManager interface {
	SubscribeMarketData(context.Context, domain.MarketDataQuery) (string, string, []domain.MarketQuote, error)
	MarketDataSubscription() domain.MarketDataSubscription
}
type marketDataReference interface {
	ListActiveInstruments(context.Context) ([]domain.SecurityRef, error)
}
type marketDataPublisher interface{ Broadcast(string, any) }

// quikInstruments lists what to subscribe to from the current QUIK Securities
// rows of the served classes. A nil store reports ErrNotConfigured on each pass.
type quikInstruments struct{ store *quikexport.Store }

func (q quikInstruments) ListActiveInstruments(ctx context.Context) ([]domain.SecurityRef, error) {
	page, err := q.store.ListSecurities(ctx, quikexport.SecurityFilter{})
	if err != nil {
		return nil, err
	}
	out := make([]domain.SecurityRef, 0, len(page.Securities))
	for _, row := range page.Securities {
		symbol := strings.TrimSpace(row.Text("SecCode"))
		out = append(out, domain.SecurityRef{
			Symbol: symbol, SecurityID: symbol, Board: strings.TrimSpace(row.Text("ClassCode")),
			Currency: strings.TrimSpace(row.Text("FaceUnit")), SecurityType: fixSecurityType(row.Text("SecType")),
		})
	}
	return out, nil
}

// fixSecurityType maps QUIK Securities.SecType to the FIX 167 value the FIX
// dictionary carried for the same instruments; other types are sent without 167.
func fixSecurityType(secType string) string {
	switch strings.TrimSpace(secType) {
	case "1":
		return "CS"
	case "2":
		return "GO"
	case "7":
		return "FXSPOT"
	}
	return ""
}

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
