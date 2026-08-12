package routing

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/fix-gateway/backend/internal/domain"
)

type Candidate struct {
	ID           string
	Status       domain.AdapterStatus
	Weight       int
	ActiveOrders int64
	Priority     int
	Enabled      bool
}

type Balancer interface {
	Select(candidates []Candidate) (string, error)
}

func New(strategy string) Balancer {
	switch strategy {
	case "round_robin":
		return &RoundRobin{}
	case "weighted_round_robin":
		return &WeightedRoundRobin{}
	default:
		return &LeastConnections{}
	}
}

func filterHealthy(cands []Candidate) []Candidate {
	var out []Candidate
	for _, c := range cands {
		if !c.Enabled {
			continue
		}
		if c.Status == domain.AdapterOnline || c.Status == domain.AdapterDegraded {
			out = append(out, c)
		}
	}
	return out
}

type LeastConnections struct{}

func (l *LeastConnections) Select(candidates []Candidate) (string, error) {
	healthy := filterHealthy(candidates)
	if len(healthy) == 0 {
		return "", fmt.Errorf("no healthy adapters")
	}
	best := healthy[0]
	for _, c := range healthy[1:] {
		if c.ActiveOrders < best.ActiveOrders {
			best = c
			continue
		}
		if c.ActiveOrders == best.ActiveOrders && c.Weight > best.Weight {
			best = c
		}
	}
	return best.ID, nil
}

type RoundRobin struct {
	n uint64
}

func (r *RoundRobin) Select(candidates []Candidate) (string, error) {
	healthy := filterHealthy(candidates)
	if len(healthy) == 0 {
		return "", fmt.Errorf("no healthy adapters")
	}
	i := atomic.AddUint64(&r.n, 1)
	return healthy[int(i-1)%len(healthy)].ID, nil
}

type WeightedRoundRobin struct {
	mu    sync.Mutex
	current map[string]int
}

func (w *WeightedRoundRobin) Select(candidates []Candidate) (string, error) {
	healthy := filterHealthy(candidates)
	if len(healthy) == 0 {
		return "", fmt.Errorf("no healthy adapters")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.current == nil {
		w.current = map[string]int{}
	}
	total := 0
	bestIdx := -1
	bestScore := -1 << 30
	for i, c := range healthy {
		weight := c.Weight
		if weight <= 0 {
			weight = 1
		}
		w.current[c.ID] += weight
		total += weight
		if w.current[c.ID] > bestScore {
			bestScore = w.current[c.ID]
			bestIdx = i
		}
	}
	if bestIdx < 0 {
		return "", fmt.Errorf("no healthy adapters")
	}
	selected := healthy[bestIdx]
	w.current[selected.ID] -= total
	return selected.ID, nil
}
