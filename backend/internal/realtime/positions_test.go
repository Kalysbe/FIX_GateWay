package realtime

import (
	"github.com/fix-gateway/backend/internal/auth"
	"testing"
)

func TestBrokerPositionsTriggersDoNotLeakAdminData(t *testing.T) {
	h := NewHub()
	if _, err := h.SubscribeBroker([]string{"positions.updated"}, ConnectionInfo{BrokerID: 1}, []string{auth.PermOrdersRead}); err != ErrPermission {
		t.Fatal("positions accepted without permission")
	}
	a, err := h.SubscribeBroker([]string{"positions.updated"}, ConnectionInfo{BrokerID: 1}, []string{auth.PermPositionsRead})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := h.SubscribeBroker([]string{"positions.updated"}, ConnectionInfo{BrokerID: 2}, []string{auth.PermPositionsRead})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	for _, sub := range []*Subscription{a, b} {
		event := <-sub.Events
		if event.Type != "positions.updated" || event.Sequence == 0 || event.Data != nil {
			t.Fatal("bad initial snapshot trigger", event)
		}
	}
	admin, err := h.Subscribe(auth.RoleSuperAdmin, []string{"positions.updated"})
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	h.Broadcast("positions.updated", map[string]any{"secret": "other firm", "firm_id": "OTHER"})
	for _, sub := range []*Subscription{a, b} {
		if event := <-sub.Events; event.Data != nil {
			t.Fatal("admin data leaked to broker", event)
		}
	}
	if event := <-admin.Events; event.Data.Fields["secret"].GetStringValue() != "other firm" {
		t.Fatal("admin event changed")
	}
	if err := a.SetTopics([]string{"positions.updated"}); err != nil {
		t.Fatal(err)
	}
	if len(a.Events) != 1 {
		t.Fatal("resubscription missing snapshot")
	}
}
