package realtime

import (
	"encoding/json"
	"testing"

	"github.com/fix-gateway/backend/internal/auth"
)

func TestMarketDataRowsReachAllSubscribersVerbatim(t *testing.T) {
	hub := NewHub()
	admin, err := hub.Subscribe(auth.RoleReadOnly, []string{"marketdata.tick"})
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	var brokers []*Subscription
	for _, info := range []ConnectionInfo{{BrokerID: 1, OrderFirmID: "2040"}, {BrokerID: 2, OrderFirmID: "9999"}} {
		sub, err := hub.SubscribeBroker([]string{"marketdata.tick"}, info, []string{auth.PermOrdersRead})
		if err != nil {
			t.Fatal(err)
		}
		defer sub.Close()
		brokers = append(brokers, sub)
	}
	// Numbers keep their source text; unknown and null columns pass through.
	row := json.RawMessage(`{"ClassCode":"LIST_GBA","SecCode":"GBA02280810","Last":123.4567890123456789,"Bid":null,"NumTrades":9007199254740993,"FutureColumn":"  x  "}`)
	hub.BroadcastMarketData(row, false)
	for _, sub := range append(brokers, admin) {
		if len(sub.Events) != 1 {
			t.Fatal("market data is not firm-scoped", len(sub.Events))
		}
		event := <-sub.Events
		if event.GetType() != "marketdata.tick" || event.GetDataJson() != string(row) || event.GetData() != nil || event.GetRemoved() {
			t.Fatalf("row changed: %v", event)
		}
	}
	hub.BroadcastMarketData(json.RawMessage(`{broken`), false)
	if len(admin.Events) != 0 {
		t.Fatal("invalid JSON delivered")
	}
}
