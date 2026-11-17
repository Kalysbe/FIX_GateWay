package realtime

import (
	"testing"

	"github.com/fix-gateway/backend/internal/auth"
)

func TestBrokerPositionsTriggersDoNotLeakAdminData(t *testing.T) {
	h := NewHub()
	both := []string{MoneyLimitsUpdated, DepoLimitsUpdated}
	for _, topic := range both {
		if _, err := h.SubscribeBroker([]string{topic}, ConnectionInfo{BrokerID: 1}, []string{auth.PermOrdersRead}); err != ErrPermission {
			t.Fatalf("%s accepted without permission", topic)
		}
	}
	// The combined event is gone: money and securities are separate.
	if _, err := h.SubscribeBroker([]string{"positions.updated"}, ConnectionInfo{BrokerID: 1}, []string{auth.PermPositionsRead}); err != ErrPermission {
		t.Fatal("positions.updated still accepted")
	}
	a, err := h.SubscribeBroker(both, ConnectionInfo{BrokerID: 1}, []string{auth.PermPositionsRead})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := h.SubscribeBroker([]string{DepoLimitsUpdated}, ConnectionInfo{BrokerID: 2}, []string{auth.PermPositionsRead})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	for _, want := range both {
		if event := <-a.Events; event.Type != want || event.Sequence == 0 || event.Data != nil {
			t.Fatal("bad initial snapshot trigger", event)
		}
	}
	if event := <-b.Events; event.Type != DepoLimitsUpdated || len(b.Events) != 0 {
		t.Fatal("bad initial snapshot trigger", event)
	}
	admin, err := h.Subscribe(auth.RoleSuperAdmin, both)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	h.Broadcast(MoneyLimitsUpdated, map[string]any{"secret": "other firm", "firm_id": "OTHER"})
	if event := <-a.Events; event.Type != MoneyLimitsUpdated || event.Data != nil {
		t.Fatal("admin data leaked to broker", event)
	}
	if len(b.Events) != 0 {
		t.Fatal("money event delivered to a securities-only subscription")
	}
	if event := <-admin.Events; event.Data.Fields["secret"].GetStringValue() != "other firm" {
		t.Fatal("admin event changed")
	}
	if err := a.SetTopics(both); err != nil {
		t.Fatal(err)
	}
	if len(a.Events) != 2 {
		t.Fatal("resubscription missing snapshots")
	}
}
