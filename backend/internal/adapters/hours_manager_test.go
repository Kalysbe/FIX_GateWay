package adapters

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/fix-gateway/backend/internal/domain"
)

// Outside the session hours the manager starts no adapter: StartAll only
// marks the pause, and a start request is refused before any FIX connection.
func TestManagerConnectsOnlyInSessionHours(t *testing.T) {
	bishkek := time.FixedZone("Asia/Bishkek", 6*60*60)
	hours, err := ParseSessionHours("09:05-03:59", bishkek)
	if err != nil {
		t.Fatal(err)
	}
	clock := time.Date(2026, 11, 6, 6, 0, 0, 0, bishkek)
	m := &Manager{adapters: map[string]Adapter{}, log: slog.New(slog.NewTextHandler(io.Discard, nil)), interval: time.Hour, now: func() time.Time { return clock }}
	m.SetSessionHours(hours)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // the background loops stop at once; StartAll must not read the store
	if err := m.StartAll(ctx); err != nil {
		t.Fatal(err)
	}
	if !m.Paused() {
		t.Fatal("06:00 is outside 09:05-03:59")
	}
	if err := m.startOne(context.Background(), domain.Adapter{ID: "quik-fix-1", Type: "arqa"}); !errors.Is(err, errOutsideSessionHours) {
		t.Fatalf("start outside the hours: %v", err)
	}
	if len(m.adapters) != 0 {
		t.Fatal("no adapter may run outside the hours")
	}
	clock = time.Date(2026, 11, 6, 9, 5, 0, 0, bishkek)
	if !m.inSession() {
		t.Fatal("09:05 opens the session")
	}
}
