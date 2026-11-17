package realtime

import (
	"github.com/fix-gateway/backend/internal/auth"
	"testing"
)

func TestNoMarketSnapshotOnSubscribeAndTopicChange(t *testing.T) {
	h := NewHub()
	s, err := h.Subscribe(auth.RoleReadOnly, []string{"trade.created"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.SetTopics([]string{"marketdata.tick"}); err != nil {
		t.Fatal(err)
	}
	// Clients load the current Params rows over REST; the stream carries changes.
	if len(s.Events) != 0 {
		t.Fatal("unexpected market data snapshot")
	}
	h.BroadcastMarketData([]byte(`{"ClassCode":"TQBR","SecCode":"SBER"}`), false)
	first := <-s.Events
	h.BroadcastMarketData([]byte(`{"ClassCode":"TQBR","SecCode":"SBER"}`), true)
	if next := <-s.Events; next.Sequence <= first.Sequence || !next.GetRemoved() {
		t.Fatal("event ordering or removal flag")
	}
}

func TestSubscriptionSecurityAndBackpressure(t *testing.T) {
	h := NewHub()
	for _, role := range []string{auth.RoleReadOnly, "unknown"} {
		if _, e := h.Subscribe(role, []string{"obs.event"}); e != ErrPermission {
			t.Fatalf("role %s: %v", role, e)
		}
	}
	s, e := h.Subscribe(auth.RoleReadOnly, []string{"marketdata.tick"})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if e := s.SetTopics([]string{"obs.event"}); e != ErrPermission {
		t.Fatal(e)
	}
	h.Broadcast("obs.event", nil)
	h.Broadcast("unknown.event", nil)
	if len(s.Events) != 0 {
		t.Fatal("restricted event leaked")
	}
	for i := 0; i < subscriptionBuffer+1; i++ {
		h.Broadcast("marketdata.tick", map[string]any{"price": "1.23"})
	}
	select {
	case <-s.Done:
	default:
		t.Fatal("slow consumer not disconnected")
	}
	if s.Err() != ErrBackpressure {
		t.Fatal(s.Err())
	}
	fresh, e := h.Subscribe(auth.RoleReadOnly, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer fresh.Close()
	h.Broadcast("marketdata.tick", nil)
	if len(fresh.Events) != 1 {
		t.Fatal("other consumer blocked")
	}
}
