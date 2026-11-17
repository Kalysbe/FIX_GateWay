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
	Adapter
	id       string
	status   domain.AdapterStatus
	priority int
	board    string
	fail     bool
}

func (a *dictionaryAdapter) ID() string                   { return a.id }
func (a *dictionaryAdapter) Status() domain.AdapterStatus { return a.status }
func (a *dictionaryAdapter) Meta() domain.Adapter {
	return domain.Adapter{ID: a.id, Priority: a.priority}
}
func (a *dictionaryAdapter) FetchSecurities(context.Context) ([]domain.SecurityRef, error) {
	if a.fail {
		return nil, errors.New("dictionary unavailable")
	}
	return []domain.SecurityRef{{SecurityID: "AAA", Board: a.board}}, nil
}

// The dictionary comes from the adapter with the best priority and falls back
// to the next one when it fails.
func TestDictionaryFollowsPriorityAndFallsBackOnFailure(t *testing.T) {
	a := &dictionaryAdapter{id: "a1", status: domain.AdapterOnline, priority: 100, board: "FIRST"}
	b := &dictionaryAdapter{id: "a2", status: domain.AdapterOnline, priority: 150, board: "SECOND"}
	m := &Manager{adapters: map[string]Adapter{"a1": a, "a2": b}, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	id, rows, err := m.FetchSecurities(context.Background())
	if err != nil || id != "a1" || rows[0].Board != "FIRST" {
		t.Fatal(id, rows, err)
	}
	a.fail = true
	id, rows, err = m.FetchSecurities(context.Background())
	if err != nil || id != "a2" || rows[0].Board != "SECOND" {
		t.Fatal(id, rows, err)
	}
}
