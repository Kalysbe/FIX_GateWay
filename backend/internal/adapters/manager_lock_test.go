package adapters

import (
	"context"
	"github.com/fix-gateway/backend/internal/domain"
	"testing"
	"time"
)

type slowDictionary struct {
	Adapter
	entered, release chan struct{}
}

func (a *slowDictionary) ID() string                   { return "slow" }
func (a *slowDictionary) Status() domain.AdapterStatus { return domain.AdapterOnline }
func (a *slowDictionary) FetchSecurities(ctx context.Context) ([]domain.SecurityRef, error) {
	close(a.entered)
	select {
	case <-a.release:
		return nil, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func TestDictionaryWaitDoesNotBlockManagerWriterOrReaders(t *testing.T) {
	ad := &slowDictionary{entered: make(chan struct{}), release: make(chan struct{})}
	m := &Manager{adapters: map[string]Adapter{"slow": ad}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); m.FetchSecurities(ctx) }()
	<-ad.entered
	changed := make(chan struct{})
	go func() { m.mu.Lock(); delete(m.adapters, "slow"); m.mu.Unlock(); close(changed) }()
	select {
	case <-changed:
	case <-time.After(time.Second):
		t.Fatal("network wait holds manager lock")
	}
	read := make(chan int, 1)
	go func() { read <- m.AvailableCount() }()
	select {
	case count := <-read:
		if count != 0 {
			t.Fatal(count)
		}
	case <-time.After(time.Second):
		t.Fatal("routing/readiness blocked")
	}
	close(ad.release)
	<-done
}
