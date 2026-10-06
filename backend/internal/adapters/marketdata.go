package adapters

import (
	"context"
	"fmt"
	"sync"

	"github.com/fix-gateway/backend/internal/domain"
)

// MarketDataSource is implemented by adapters that can request QUIK «Текущие торги» via FIX (35=V/W).
type MarketDataSource interface {
	FetchMarketData(ctx context.Context, q domain.MarketDataQuery) (reqID string, items []domain.MarketQuote, err error)
}

// LiveMarketData is implemented by adapters that keep a FIX market-data subscription (263=1).
type LiveMarketData interface {
	SubscribeMarketData(ctx context.Context, q domain.MarketDataQuery) (reqID string, items []domain.MarketQuote, err error)
	LiveQuotes() []domain.MarketQuote
}

var mdFetchMu sync.Mutex

func (m *Manager) FetchMarketData(ctx context.Context, q domain.MarketDataQuery) (adapterID, reqID string, items []domain.MarketQuote, err error) {
	mdFetchMu.Lock()
	defer mdFetchMu.Unlock()

	type cand struct {
		id  string
		src MarketDataSource
	}
	m.mu.RLock()
	var list []cand
	for _, ad := range m.adapters {
		if ad.Status() != domain.AdapterOnline && ad.Status() != domain.AdapterDegraded {
			continue
		}
		src, ok := ad.(MarketDataSource)
		if !ok {
			continue
		}
		list = append(list, cand{id: ad.ID(), src: src})
	}
	m.mu.RUnlock()

	var firstErr error
	for _, c := range list {
		reqID, items, err = c.src.FetchMarketData(ctx, q)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			m.log.Warn("market data request failed", "adapter_id", c.id, "error", err)
			continue
		}
		return c.id, reqID, items, nil
	}
	if firstErr != nil {
		return "", "", nil, firstErr
	}
	return "", "", nil, fmt.Errorf("no online FIX adapter available for Market Data Request")
}

func (m *Manager) SubscribeMarketData(ctx context.Context, q domain.MarketDataQuery) (adapterID, reqID string, items []domain.MarketQuote, err error) {
	mdFetchMu.Lock()
	defer mdFetchMu.Unlock()

	m.mu.RLock()
	var list []struct {
		id  string
		src LiveMarketData
	}
	for _, ad := range m.adapters {
		if ad.Status() != domain.AdapterOnline && ad.Status() != domain.AdapterDegraded {
			continue
		}
		src, ok := ad.(LiveMarketData)
		if !ok {
			continue
		}
		list = append(list, struct {
			id  string
			src LiveMarketData
		}{id: ad.ID(), src: src})
	}
	m.mu.RUnlock()

	var firstErr error
	for _, c := range list {
		reqID, items, err = c.src.SubscribeMarketData(ctx, q)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			m.log.Warn("market data subscribe failed", "adapter_id", c.id, "error", err)
			continue
		}
		if len(items) == 0 {
			m.log.Warn("market data subscribe returned no quotes", "adapter_id", c.id)
			continue
		}
		return c.id, reqID, items, nil
	}
	if firstErr != nil {
		return "", "", nil, firstErr
	}
	if len(list) > 0 {
		return "", "", nil, fmt.Errorf("market data subscribe returned no quotes")
	}
	return "", "", nil, fmt.Errorf("no online FIX adapter available for Market Data subscribe")
}

func (m *Manager) LiveQuotes() (adapterID string, items []domain.MarketQuote, ok bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, ad := range m.adapters {
		src, isLive := ad.(LiveMarketData)
		if !isLive {
			continue
		}
		items = src.LiveQuotes()
		if len(items) == 0 {
			continue
		}
		return ad.ID(), items, true
	}
	return "", nil, false
}
