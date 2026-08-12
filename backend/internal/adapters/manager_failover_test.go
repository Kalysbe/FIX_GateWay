package adapters_test

import (
	"testing"

	"github.com/fix-gateway/backend/internal/domain"
	"github.com/fix-gateway/backend/internal/orders"
	"github.com/fix-gateway/backend/internal/routing"
)

func TestFailoverSelectionAfterOffline(t *testing.T) {
	b := routing.New("least_connections")
	id, err := b.Select([]routing.Candidate{
		{ID: "adapter-1", Status: domain.AdapterOffline, ActiveOrders: 0, Weight: 100, Enabled: true},
		{ID: "adapter-2", Status: domain.AdapterOnline, ActiveOrders: 5, Weight: 50, Enabled: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if id != "adapter-2" {
		t.Fatalf("got %s", id)
	}
}

func TestNoResendWhenExternallyAccepted(t *testing.T) {
	qid := "Q1"
	o := domain.Order{Status: domain.OrderAcknowledged, QuikOrderID: &qid}
	if !orders.IsExternallyAccepted(o) {
		t.Fatal("must block failover resend")
	}
}
