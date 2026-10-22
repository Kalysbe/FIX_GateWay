package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fix-gateway/backend/internal/domain"
	"github.com/fix-gateway/backend/internal/refdata"
)

type recoveryDictionaryManager struct {
	online atomic.Bool
	calls  atomic.Int32
	state  atomic.Value
}

func (m *recoveryDictionaryManager) AvailableCount() int {
	if m.online.Load() {
		return 1
	}
	return 0
}
func (m *recoveryDictionaryManager) MarketDataSubscription() domain.MarketDataSubscription {
	return m.state.Load().(domain.MarketDataSubscription)
}
func (m *recoveryDictionaryManager) FetchSecurities(context.Context) (string, []domain.SecurityRef, error) {
	switch m.calls.Add(1) {
	case 1:
		return "a1", nil, errors.New("temporary disconnect")
	case 2:
		return "a1", nil, nil
	case 3:
		return "a1", []domain.SecurityRef{{SecurityID: "USD", Board: "CROSSRATE"}}, nil
	default:
		return "a1", []domain.SecurityRef{{SecurityID: "GOLD010101", Board: "QME_MTL"}}, nil
	}
}

type recoveryDictionaryStore struct {
	manager *recoveryDictionaryManager
	saved   chan string
}

func (s *recoveryDictionaryStore) SyncFromFIX(_ context.Context, rows []domain.SecurityRef) (refdata.SyncResult, error) {
	if len(rows) == 0 {
		panic("empty dictionary would replace existing reference data")
	}
	state := domain.MarketDataSubscription{AdapterID: "a1", Status: "ACTIVE"}
	if rows[0].Board == "CROSSRATE" {
		state.Status, state.Error = "RETRYING", "Unknown ExDestination [CROSSRATE] (reason=0)"
	}
	s.manager.state.Store(state)
	s.saved <- rows[0].Board
	return refdata.SyncResult{InstrumentsUpserted: len(rows)}, nil
}

func TestDictionaryRecoversAfterStartupOutageAndUnknownClassWithoutRestart(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	m := &recoveryDictionaryManager{}
	m.state.Store(domain.MarketDataSubscription{Status: "WAITING"})
	store := &recoveryDictionaryStore{manager: m, saved: make(chan string, 4)}
	done := make(chan struct{})
	go func() {
		defer close(done)
		syncInstrumentLoop(ctx, slog.New(slog.NewTextHandler(io.Discard, nil)), m, store, time.Millisecond, time.Hour, 10*time.Millisecond)
	}()
	time.Sleep(12 * time.Millisecond)
	if m.calls.Load() != 0 {
		t.Fatal("dictionary requested before Logon")
	}
	m.online.Store(true)
	for _, want := range []string{"CROSSRATE", "QME_MTL"} {
		select {
		case got := <-store.saved:
			if got != want {
				t.Fatal(got, want)
			}
		case <-ctx.Done():
			t.Fatal("automatic dictionary recovery stopped")
		}
	}
	time.Sleep(25 * time.Millisecond)
	if m.calls.Load() != 4 {
		t.Fatal("healthy dictionary is being fetched repeatedly", m.calls.Load())
	}
	cancel()
	<-done
}
