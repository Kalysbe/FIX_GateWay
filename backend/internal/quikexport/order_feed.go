package quikexport

import (
	"bytes"
	"context"
	"time"
)

// RunOrders reads complete successful selections before comparing them. An
// outage must never be interpreted as an empty table or cancel/expiry evidence.
type OrderChange struct {
	Order   Order
	Removed bool
}

func (s *Store) RunOrders(ctx context.Context, publish func(OrderChange), failure func(error)) {
	runOrderFeed(ctx, s.ListOrders, 2*time.Second, publish, failure)
}

func runOrderFeed(ctx context.Context, readOrders func(context.Context, OrderFilter) (OrderPage, error), interval time.Duration, publish func(OrderChange), failure func(error)) {
	previous := map[string]Order{}
	// The first complete selection is the baseline, not a change: clients load
	// current rows over REST, and replaying the whole table would overflow
	// every subscriber queue at startup.
	primed := false
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		read, cancel := context.WithTimeout(ctx, 10*time.Second)
		page, err := readOrders(read, OrderFilter{})
		cancel()
		if err != nil {
			if ctx.Err() == nil && failure != nil {
				failure(err)
			}
		} else {
			current := make(map[string]Order, len(page.Orders))
			for _, o := range page.Orders {
				current[o.Key()] = o
			}
			if primed {
				for _, o := range orderChanges(previous, current) {
					publish(o)
				}
			}
			previous, primed = current, true
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func orderChanges(previous, current map[string]Order) []OrderChange {
	changes := []OrderChange{}
	for id, o := range current {
		old, ok := previous[id]
		if !ok || !bytes.Equal(old.Raw, o.Raw) {
			// Firm reassignment removes the old scope's copy before publishing
			// the new row to its new firm.
			if ok && old.Text("FirmId") != o.Text("FirmId") {
				changes = append(changes, OrderChange{Order: old, Removed: true})
			}
			changes = append(changes, OrderChange{Order: o})
		}
	}
	for id, o := range previous {
		if _, ok := current[id]; !ok {
			changes = append(changes, OrderChange{Order: o, Removed: true})
		}
	}
	return changes
}
