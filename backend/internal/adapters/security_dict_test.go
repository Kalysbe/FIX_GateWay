package adapters

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/fix-gateway/backend/internal/domain"
)

type dictionaryAdapter struct {
	mdTestAdapter
	priority int
	board    string
	fail     bool
}

func (a *dictionaryAdapter) Meta() domain.Adapter {
	return domain.Adapter{ID: a.id, Priority: a.priority}
}
func (a *dictionaryAdapter) FetchSecurities(context.Context) ([]domain.SecurityRef, error) {
	if a.fail {
		return nil, errors.New("dictionary unavailable")
	}
	return []domain.SecurityRef{{SecurityID: "AAA", Board: a.board}}, nil
}

func TestDictionaryStaysWithMarketDataOwnerAndFallsBackOnFailure(t *testing.T) {
	a := &dictionaryAdapter{mdTestAdapter: mdTestAdapter{id: "a1", status: domain.AdapterOnline}, priority: 200, board: "LEGACY"}
	b := &dictionaryAdapter{mdTestAdapter: mdTestAdapter{id: "a2", status: domain.AdapterOnline}, priority: 150, board: "CURRENT"}
	m := &Manager{adapters: map[string]Adapter{"a1": a, "a2": b}}
	id, rows, err := m.FetchSecurities(context.Background())
	if err != nil || id != "a2" || rows[0].Board != "CURRENT" {
		t.Fatal(id, rows, err)
	}
	m.setMarketDataOwner("a1")
	id, rows, err = m.FetchSecurities(context.Background())
	if err != nil || id != "a1" || rows[0].Board != "LEGACY" {
		t.Fatal(id, rows, err)
	}
	a.fail = true
	// Failure logging is exercised by the manager; use the test's quiet logger.
	m.log = slog.New(slog.NewTextHandler(io.Discard, nil))
	id, rows, err = m.FetchSecurities(context.Background())
	if err != nil || id != "a2" || rows[0].Board != "CURRENT" {
		t.Fatal(id, rows, err)
	}
}
