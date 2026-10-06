package realtime

import (
	"github.com/fix-gateway/backend/internal/auth"
	"testing"
)

func TestMarketSnapshotOnSubscribeAndTopicChange(t *testing.T) {
	h := NewHub()
	h.MarketDataSnapshot = func() any { return map[string]any{"quotes": []any{map[string]any{"symbol": "TEST", "price": "12.34"}}} }
	s, err := h.Subscribe(auth.RoleReadOnly, []string{"trade.created"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if len(s.Events) != 0 {
		t.Fatal("snapshot leaked to trade-only subscription")
	}
	if err := s.SetTopics([]string{"marketdata.tick"}); err != nil {
		t.Fatal(err)
	}
	initial := <-s.Events
	if initial.Type != "marketdata.tick" || len(initial.Data.Fields["quotes"].GetListValue().Values) != 1 {
		t.Fatal("missing snapshot")
	}
	h.Broadcast("marketdata.tick", map[string]any{"quotes": []any{}})
	if next := <-s.Events; next.Sequence <= initial.Sequence {
		t.Fatal("event ordering")
	}
	fresh, err := h.Subscribe(auth.RoleReadOnly, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	if len(fresh.Events) != 1 {
		t.Fatal("new subscriber did not receive current quotes")
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
	for i := 0; i < 65; i++ {
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
