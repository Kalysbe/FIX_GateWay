package adapters

import (
	"context"
	"errors"
	"testing"

	"github.com/fix-gateway/backend/internal/domain"
)

type mdTestAdapter struct {
	Adapter
	id     string
	status domain.AdapterStatus
	err    error
	rows   []domain.MarketQuote
	calls  int
	stops  int
}

func (a *mdTestAdapter) ID() string                   { return a.id }
func (a *mdTestAdapter) Status() domain.AdapterStatus { return a.status }
func (a *mdTestAdapter) Meta() domain.Adapter         { return domain.Adapter{ID: a.id} }
func (a *mdTestAdapter) SubscribeMarketData(context.Context, domain.MarketDataQuery) (string, []domain.MarketQuote, error) {
	a.calls++
	return "MD1", a.rows, a.err
}
func (a *mdTestAdapter) LiveQuotes() []domain.MarketQuote            { return a.rows }
func (a *mdTestAdapter) UnsubscribeMarketData(context.Context) error { a.stops++; return nil }

func TestMarketDataOwnerFailsOverAndDoesNotReturnOfflineOrRandomFeed(t *testing.T) {
	a := &mdTestAdapter{id: "a1", status: domain.AdapterOnline, err: errors.New("rejected"), rows: []domain.MarketQuote{{Symbol: "AAA"}}}
	b := &mdTestAdapter{id: "a2", status: domain.AdapterOnline, rows: []domain.MarketQuote{{Symbol: "AAA", MDReqID: "MD1"}}}
	m := &Manager{adapters: map[string]Adapter{"a2": b, "a1": a}}
	id, _, _, err := m.SubscribeMarketData(context.Background(), domain.MarketDataQuery{})
	if err != nil || id != "a2" {
		t.Fatal(id, err)
	}
	if a.stops != 1 {
		t.Fatal("previous subscription was not released")
	}
	a.err = nil
	_, _, _, _ = m.SubscribeMarketData(context.Background(), domain.MarketDataQuery{})
	if a.calls != 1 || b.calls != 2 {
		t.Fatal("healthy owner was replaced", a.calls, b.calls)
	}
	if live, _, ok := m.LiveQuotes(); !ok || live != "a2" {
		t.Fatal("read selected wrong adapter", live)
	}
	b.status = domain.AdapterOffline
	if _, _, ok := m.LiveQuotes(); ok {
		t.Fatal("offline cache reported live")
	}
	id, _, _, err = m.SubscribeMarketData(context.Background(), domain.MarketDataQuery{})
	if err != nil || id != "a1" {
		t.Fatal("offline owner did not fail over", id, err)
	}
}

func TestMarketDataPublishesOnlyChosenAdapter(t *testing.T) {
	var published int
	m := &Manager{onMD: func([]domain.MarketQuote) { published++ }}
	m.setMarketDataOwner("a2")
	m.publishMarketData("a1", []domain.MarketQuote{{Symbol: "AAA"}})
	m.publishMarketData("a2", []domain.MarketQuote{{Symbol: "AAA"}})
	if published != 1 {
		t.Fatal("unselected adapter contaminated market feed")
	}
}
