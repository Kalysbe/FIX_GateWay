package api

import (
	"context"
	"github.com/fix-gateway/backend/internal/auth"
	"github.com/fix-gateway/backend/internal/realtime"
	"testing"
)

func TestBrokerPresenceIncludesActiveGRPC(t *testing.T) {
	hub := realtime.NewHub()
	s := &Server{hub: hub}
	info := realtime.ConnectionInfo{BrokerID: 1, BrokerCode: "2040", BrokerName: "Freedom", Transport: "gRPC", Peer: "127.0.0.1:1234"}
	a, err := hub.SubscribeBroker([]string{"marketdata.tick"}, info, auth.ReadOnlyPermissions)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := hub.SubscribeBroker([]string{"order.updated"}, info, auth.ReadOnlyPermissions)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	admin, err := hub.SubscribeIdentified(auth.RoleSuperAdmin, []string{"order.updated"}, realtime.ConnectionInfo{Transport: "gRPC"})
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	if got := s.connectedBrokers(context.Background()); len(got) != 1 || got[0].BrokerID != 1 || got[0].LastActivity.IsZero() {
		t.Fatal("active broker not counted exactly once", got)
	}
	a.Close()
	if len(s.connectedBrokers(context.Background())) != 1 {
		t.Fatal("remaining stream ignored")
	}
	b.Close()
	if len(s.connectedBrokers(context.Background())) != 0 {
		t.Fatal("disconnected broker or admin shown as online")
	}
}
