package routing

import (
	"testing"

	"github.com/fix-gateway/backend/internal/domain"
)

func TestLeastConnections(t *testing.T) {
	b := &LeastConnections{}
	id, err := b.Select([]Candidate{
		{ID: "a1", Status: domain.AdapterOnline, Weight: 100, ActiveOrders: 100, Enabled: true},
		{ID: "a2", Status: domain.AdapterOnline, Weight: 50, ActiveOrders: 40, Enabled: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if id != "a2" {
		t.Fatalf("expected a2, got %s", id)
	}
}

func TestHealthAwareSkipsOffline(t *testing.T) {
	b := &LeastConnections{}
	id, err := b.Select([]Candidate{
		{ID: "a1", Status: domain.AdapterOffline, Weight: 100, ActiveOrders: 0, Enabled: true},
		{ID: "a2", Status: domain.AdapterOnline, Weight: 50, ActiveOrders: 10, Enabled: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if id != "a2" {
		t.Fatalf("expected a2, got %s", id)
	}
}

func TestNoHealthy(t *testing.T) {
	b := &RoundRobin{}
	_, err := b.Select([]Candidate{
		{ID: "a1", Status: domain.AdapterOffline, Enabled: true},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestWeightedRoundRobinBias(t *testing.T) {
	b := &WeightedRoundRobin{}
	counts := map[string]int{}
	cands := []Candidate{
		{ID: "a1", Status: domain.AdapterOnline, Weight: 100, Enabled: true},
		{ID: "a2", Status: domain.AdapterOnline, Weight: 50, Enabled: true},
	}
	for i := 0; i < 150; i++ {
		id, err := b.Select(cands)
		if err != nil {
			t.Fatal(err)
		}
		counts[id]++
	}
	if counts["a1"] <= counts["a2"] {
		t.Fatalf("expected a1 > a2, got %#v", counts)
	}
}
