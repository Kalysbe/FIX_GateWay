package realtime

import (
	"strings"
	"testing"

	pb "github.com/fix-gateway/backend/gen/realtime/v1"
	"github.com/fix-gateway/backend/internal/auth"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestBrokerMarketDataKeepsBusinessFieldsAndAdminDiagnostics(t *testing.T) {
	hub := NewHub()
	quote := map[string]any{
		"symbol": "GBA02280810", "security_id": "GBA02280810", "board": "LIST_GBA",
		"currency": "KGS", "last": "123.4567890123456789", "trading_status": "17",
		"updated_at": "2026-10-21T09:00:00Z", "adapter_id": "private-adapter",
		"md_req_id": "private-request", "raw_fix": "private-wire",
		"entries":               map[string]any{"private-tag": "private-value"},
		"future_internal_field": "private-future-value",
		"bids":                  []any{map[string]any{"price": "123.4567890123456789", "size": "10", "internal": "private-level"}},
	}
	full := map[string]any{
		"quotes": []any{quote}, "replace": true, "internal": "private-root",
		"subscription": map[string]any{"adapter_id": "private-adapter", "status": "ACTIVE", "error": "private-error"},
	}
	hub.MarketDataSnapshot = func() any { return full }
	admin, err := hub.Subscribe(auth.RoleSuperAdmin, []string{"marketdata.tick"})
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	brokers := []*Subscription{}
	for _, id := range []int64{1, 2} {
		sub, err := hub.SubscribeBroker([]string{"marketdata.tick"}, ConnectionInfo{BrokerID: id}, []string{auth.PermOrdersRead})
		if err != nil {
			t.Fatal(err)
		}
		defer sub.Close()
		brokers = append(brokers, sub)
	}
	assertPublic := func(event *pb.Event, snapshot bool) {
		t.Helper()
		encoded, err := protojson.Marshal(event.Data)
		if err != nil {
			t.Fatal(err)
		}
		for _, internal := range []string{"adapter_id", "md_req_id", "raw_fix", "entries", "subscription", "private", "internal"} {
			if strings.Contains(string(encoded), internal) {
				t.Fatalf("broker market event leaked %s: %s", internal, encoded)
			}
		}
		q := event.Data.Fields["quotes"].GetListValue().Values[0].GetStructValue()
		if q.Fields["last"].GetStringValue() != "123.4567890123456789" ||
			q.Fields["trading_status"].GetStringValue() != "17" || q.Fields["board"].GetStringValue() != "LIST_GBA" ||
			q.Fields["updated_at"].GetStringValue() != "2026-10-21T09:00:00Z" {
			t.Fatal("business quote fields lost", event.Data)
		}
		level := q.Fields["bids"].GetListValue().Values[0].GetStructValue()
		if len(level.Fields) != 2 || level.Fields["price"].GetStringValue() != "123.4567890123456789" || level.Fields["size"].GetStringValue() != "10" {
			t.Fatal("book projection changed market values", level)
		}
		if snapshot {
			if !event.Data.Fields["replace"].GetBoolValue() || !event.Data.Fields["live"].GetBoolValue() {
				t.Fatal("snapshot availability lost", event.Data)
			}
		} else if event.Data.Fields["replace"] != nil || event.Data.Fields["live"] != nil {
			t.Fatal("delta invented snapshot or availability state", event.Data)
		}
	}
	initialAdmin := <-admin.Events
	for _, sub := range brokers {
		assertPublic(<-sub.Events, true)
	}
	if initialAdmin.Data.Fields["subscription"].GetStructValue().Fields["adapter_id"].GetStringValue() != "private-adapter" {
		t.Fatal("admin diagnostics removed")
	}
	if err := brokers[0].SetTopics([]string{"marketdata.tick"}); err != nil {
		t.Fatal(err)
	}
	assertPublic(<-brokers[0].Events, true)
	for _, snapshot := range []bool{true, false} {
		data := full
		if !snapshot {
			data = map[string]any{"quotes": []any{quote}}
		}
		hub.Broadcast("marketdata.tick", data)
		adminEvent := <-admin.Events
		for _, sub := range brokers {
			event := <-sub.Events
			assertPublic(event, snapshot)
			if event.Sequence != adminEvent.Sequence || !event.OccurredAt.AsTime().Equal(adminEvent.OccurredAt.AsTime()) {
				t.Fatal("broker projection changed event ordering")
			}
		}
		adminQuote := adminEvent.Data.Fields["quotes"].GetListValue().Values[0].GetStructValue()
		if adminQuote.Fields["md_req_id"].GetStringValue() != "private-request" || quote["raw_fix"] != "private-wire" {
			t.Fatal("shared admin snapshot mutated")
		}
	}
	for _, state := range []string{"STARTING", "RETRYING", "OFFLINE", "PARTIAL"} {
		hub.Broadcast("marketdata.tick", map[string]any{"quotes": nil, "replace": true, "subscription": map[string]any{"status": state, "error": "private-error"}})
		<-admin.Events
		for _, sub := range brokers {
			event := <-sub.Events
			if event.Data.Fields["live"].GetBoolValue() || len(event.Data.Fields["quotes"].GetListValue().Values) != 0 {
				t.Fatal("empty or unavailable snapshot presented as live", event.Data)
			}
			if event.Data.Fields["subscription"] != nil || !event.Data.Fields["replace"].GetBoolValue() {
				t.Fatal("empty snapshot contract violated", event.Data)
			}
		}
	}
}
