package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/fix-gateway/backend/internal/domain"
)

func TestFIXSecurityTypeFromQUIK(t *testing.T) {
	for secType, want := range map[string]string{"1": "CS", " 2 ": "GO", "7": "FXSPOT", "29": "", "": ""} {
		if got := fixSecurityType(secType); got != want {
			t.Errorf("SecType %q: %q, want %q", secType, got, want)
		}
	}
}

type testMDManager struct {
	calls   int
	cancel  context.CancelFunc
	queries []domain.MarketDataQuery
}

func (m *testMDManager) SubscribeMarketData(_ context.Context, q domain.MarketDataQuery) (string, string, []domain.MarketQuote, error) {
	m.calls++
	m.queries = append(m.queries, q)
	row := domain.MarketQuote{Symbol: "AAA", SecurityID: "AAA", Board: "BOARD"}
	if m.calls < 3 {
		return "a1", "MD1", []domain.MarketQuote{row}, errors.New("rejected")
	}
	row.MDReqID = "MD1"
	if m.calls == 4 {
		m.cancel()
	}
	return "a1", "MD1", []domain.MarketQuote{row}, nil
}
func (m *testMDManager) MarketDataSubscription() domain.MarketDataSubscription {
	if m.calls < 3 {
		return domain.MarketDataSubscription{AdapterID: "a1", Status: "RETRYING", Expected: 1}
	}
	return domain.MarketDataSubscription{AdapterID: "a1", Status: "ACTIVE", Expected: 1, Confirmed: 1}
}

type testMDReference struct{ calls int }

func (r *testMDReference) ListActiveInstruments(context.Context) ([]domain.SecurityRef, error) {
	r.calls++
	list := []domain.SecurityRef{{Symbol: "AAA", Board: "BOARD"}}
	if r.calls > 1 {
		list = append(list, domain.SecurityRef{Symbol: "BBB", Board: "BOARD"})
	}
	return list, nil
}

type testMDHub struct{ events int }

func (h *testMDHub) Broadcast(string, any) { h.events++ }

func TestMarketDataSupervisorContinuesAfterSeedsAndRefreshesDictionaryWithoutUI(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	m := &testMDManager{cancel: cancel}
	refs := &testMDReference{}
	syncMarketDataLoop(ctx, slog.New(slog.NewTextHandler(io.Discard, nil)), m, refs, time.Millisecond, nil)
	if m.calls != 4 {
		t.Fatal("automatic recovery stopped on seeded or quiet rows", m.calls)
	}
	if len(m.queries[0].Instruments) != 1 || len(m.queries[1].Instruments) != 2 {
		t.Fatal("dictionary change was not reconciled")
	}
}
