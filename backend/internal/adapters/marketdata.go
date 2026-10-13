package adapters

import (
	"context"
	"fmt"
	"sort"
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

type marketDataStateSource interface {
	MarketDataSubscription() domain.MarketDataSubscription
}

type marketDataUnsubscriber interface{ UnsubscribeMarketData(context.Context) error }

func (m *Manager) adoptMarketDataOwner(ctx context.Context, id string) {
	previous := m.marketDataOwner()
	m.setMarketDataOwner(id)
	if previous == "" || previous == id {
		return
	}
	m.mu.RLock()
	old := m.adapters[previous]
	m.mu.RUnlock()
	if src, ok := old.(marketDataUnsubscriber); ok {
		_ = src.UnsubscribeMarketData(ctx)
	}
}

func (m *Manager) marketDataOwner() string {
	m.mdOwnerMu.RLock()
	defer m.mdOwnerMu.RUnlock()
	return m.mdOwner
}

func (m *Manager) setMarketDataOwner(id string) {
	m.mdOwnerMu.Lock()
	m.mdOwner = id
	m.mdOwnerMu.Unlock()
}

func (m *Manager) publishMarketData(id string, quotes []domain.MarketQuote) {
	if m.marketDataOwner() == id && m.onMD != nil {
		m.onMD(quotes)
	}
}

func (m *Manager) MarketDataSubscription() domain.MarketDataSubscription {
	id := m.marketDataOwner()
	m.mu.RLock()
	ad := m.adapters[id]
	m.mu.RUnlock()
	if ad == nil {
		return domain.MarketDataSubscription{Status: "WAITING"}
	}
	if ad.Status() != domain.AdapterOnline && ad.Status() != domain.AdapterDegraded {
		return domain.MarketDataSubscription{AdapterID: id, Status: "OFFLINE"}
	}
	if src, ok := ad.(marketDataStateSource); ok {
		return src.MarketDataSubscription()
	}
	return domain.MarketDataSubscription{AdapterID: id, Status: "STARTING"}
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
	m.mdMaintainMu.Lock()
	defer m.mdMaintainMu.Unlock()

	m.mu.RLock()
	var list []struct {
		id       string
		src      LiveMarketData
		priority int
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
			id       string
			src      LiveMarketData
			priority int
		}{id: ad.ID(), src: src, priority: ad.Meta().Priority})
	}
	m.mu.RUnlock()
	owner := m.marketDataOwner()
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].id == owner || list[j].id == owner {
			return list[i].id == owner
		}
		if list[i].priority != list[j].priority {
			return list[i].priority < list[j].priority
		}
		return list[i].id < list[j].id
	})

	var firstErr error
	var bestID, bestReqID string
	var bestItems []domain.MarketQuote
	bestCount := -1
	for _, c := range list {
		if bestID == "" && owner == "" {
			m.adoptMarketDataOwner(ctx, c.id)
		}
		reqID, items, err = c.src.SubscribeMarketData(ctx, q)
		confirmed := 0
		for _, row := range items {
			if row.HasData() {
				confirmed++
			}
		}
		if state, ok := c.src.(marketDataStateSource); ok {
			confirmed = state.MarketDataSubscription().Confirmed
		}
		if confirmed > bestCount {
			bestID, bestReqID, bestItems, bestCount = c.id, reqID, items, confirmed
		}
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		// Pending subscriptions are maintained by the supervisor. A dictionary
		// seed is not a confirmation, and cannot displace a working partial feed.
		if confirmed >= bestCount {
			m.adoptMarketDataOwner(ctx, c.id)
			return c.id, reqID, items, nil
		}
	}
	if bestID != "" {
		m.adoptMarketDataOwner(ctx, bestID)
	}
	if firstErr != nil {
		return bestID, bestReqID, bestItems, firstErr
	}
	if len(list) > 0 {
		return "", "", nil, fmt.Errorf("market data subscribe returned no quotes")
	}
	return "", "", nil, fmt.Errorf("no online FIX adapter available for Market Data subscribe")
}

func (m *Manager) LiveQuotes() (adapterID string, items []domain.MarketQuote, ok bool) {
	owner := m.marketDataOwner()
	m.mu.RLock()
	defer m.mu.RUnlock()
	ad := m.adapters[owner]
	if ad == nil || (ad.Status() != domain.AdapterOnline && ad.Status() != domain.AdapterDegraded) {
		return "", nil, false
	}
	src, isLive := ad.(LiveMarketData)
	if !isLive {
		return "", nil, false
	}
	items = src.LiveQuotes()
	return owner, items, len(items) > 0
}
