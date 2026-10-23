package realtime

import (
	"encoding/json"
	"github.com/fix-gateway/backend/internal/auth"
	"testing"
)

func TestQUIKOrdersAreDeliveredByFirmNotBrokerID(t *testing.T) {
	h := NewHub()
	a, err := h.SubscribeBroker([]string{"order.updated"}, ConnectionInfo{BrokerID: 1, OrderFirmID: "2040"}, auth.ReadOnlyPermissions)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := h.SubscribeBroker([]string{"order.updated"}, ConnectionInfo{BrokerID: 1, OrderFirmID: "9999"}, auth.ReadOnlyPermissions)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	admin, err := h.Subscribe(auth.RoleAdmin, []string{"order.updated"})
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	aJSON := json.RawMessage(`{"OrderNum":9007199254740993123,"FirmId":"2040","Price":120.00000000,"Balance":null}`)
	bJSON := json.RawMessage(`{"OrderNum":2,"FirmId":"9999"}`)
	h.BroadcastOrderForFirm(aJSON, "2040", false)
	h.BroadcastOrderForFirm(bJSON, "9999", true)
	h.BroadcastOrderForFirm(json.RawMessage(`{"OrderNum":3,"FirmId":null}`), "", false)
	if len(a.Events) != 1 || len(b.Events) != 1 || len(admin.Events) != 3 {
		t.Fatal(len(a.Events), len(b.Events), len(admin.Events))
	}
	aEvent, bEvent := <-a.Events, <-b.Events
	if aEvent.GetData() != nil || aEvent.GetDataJson() != string(aJSON) || aEvent.GetRemoved() || bEvent.GetDataJson() != string(bJSON) || !bEvent.GetRemoved() {
		t.Fatal("foreign firm order delivered")
	}
}
