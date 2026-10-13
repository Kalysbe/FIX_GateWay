package realtime

import (
	"github.com/fix-gateway/backend/internal/auth"
	"testing"
)

func TestBrokerIsolationAndConnectionLifecycle(t *testing.T) {
	h := NewHub()
	if _, err := h.Subscribe(auth.RoleReadOnly, []string{"subscriptions.updated"}); err != ErrPermission {
		t.Fatal("read-only granted client identities")
	}
	monitor, err := h.Subscribe(auth.RoleAuditor, []string{"subscriptions.updated"})
	if err != nil {
		t.Fatal(err)
	}
	defer monitor.Close()
	<-monitor.Events
	a, err := h.SubscribeBroker([]string{"order.updated", "trade.created"}, ConnectionInfo{BrokerID: 1, BrokerCode: "A"}, auth.ReadOnlyPermissions)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := h.SubscribeBroker([]string{"order.updated"}, ConnectionInfo{BrokerID: 2}, auth.ReadOnlyPermissions)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	h.BroadcastForBroker("order.updated", map[string]any{"secret": "only A"}, 1)
	if len(a.Events) != 1 || len(b.Events) != 0 {
		t.Fatal("cross-broker data leakage")
	}
	h.Broadcast("order.updated", map[string]any{"secret": "unscoped"})
	if len(a.Events) != 1 || len(b.Events) != 0 {
		t.Fatal("unscoped private data leakage")
	}
	if err := a.SetTopics([]string{"obs.event"}); err != ErrPermission {
		t.Fatal("broker granted admin topics")
	}
	if _, err := h.SubscribeBroker([]string{"trade.created"}, ConnectionInfo{BrokerID: 3}, []string{auth.PermOrdersRead}); err != ErrPermission {
		t.Fatal("missing permission accepted")
	}
	before := h.Connections()
	if len(before.Connections) != 3 {
		t.Fatal("missing active connections")
	}
	for _, c := range before.Connections {
		if c.BrokerID == 1 && (len(c.Paths) != 2 || c.Paths[0] != "/api/v1/orders") {
			t.Fatal(c.Paths)
		}
	}
	if err := a.SetTopics([]string{"marketdata.tick"}); err != nil {
		t.Fatal(err)
	}
	for _, c := range h.Connections().Connections {
		if c.BrokerID == 1 && (len(c.Paths) != 2 || c.Paths[0] != "/api/v1/market-data" || c.Paths[1] != "/api/v1/orderbook") {
			t.Fatal("market data REST sources missing", c.Paths)
		}
	}
	after := h.Connections()
	if after.Revision <= before.Revision {
		t.Fatal("topic revision did not change")
	}
	a.Close()
	a.Close()
	final := h.Connections()
	if len(final.Connections) != 2 || final.Revision <= after.Revision {
		t.Fatal("closed connection retained")
	}
	// Snapshots cannot mutate the live subscription.
	final.Connections[0].Topics = append(final.Connections[0].Topics, "obs.event")
	for _, c := range h.Connections().Connections {
		if c.BrokerID > 0 {
			for _, topic := range c.Topics {
				if topic == "obs.event" {
					t.Fatal("snapshot mutated registry")
				}
			}
		}
	}
}
