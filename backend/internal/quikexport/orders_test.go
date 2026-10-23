package quikexport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestOrderPreservesDatabaseJSON(t *testing.T) {
	raw := json.RawMessage(`{"OrderNum":9007199254740993123,"TradeDate":"2026-10-22T00:00:00","ClassCode":"LIST_GBA","FirmId":"2040","Operation":"Купля","State":"Активна","Qty":10.00000001,"Balance":null,"Price":105.12345678,"FutureField":"  unchanged  "}`)
	o := Order{Raw: raw}
	encoded, err := json.Marshal(o)
	if err != nil || !bytes.Equal(raw, encoded) {
		t.Fatalf("database row changed: %s %v", encoded, err)
	}
	if o.Text("OrderNum") != "9007199254740993123" || o.Text("FutureField") != "  unchanged  " {
		t.Fatal("source fields changed")
	}
	if o.VenueID() != "261022-LIST_GBA-9007199254740993123" {
		t.Fatal(o.VenueID())
	}
}

func TestOrderKeysContainFullSourcePrimaryKey(t *testing.T) {
	for _, key := range []string{"quik-20261022-LIST_GBA-22455", "261022-LIST_GBA-22455"} {
		date, board, number, err := ParseOrderKey(key)
		if err != nil || date != "2026-10-22" || board != "LIST_GBA" || number != "22455" {
			t.Fatal(key, date, board, number, err)
		}
	}
	for _, key := range []string{"22455", "quik-20260230-LIST_GBA-1", "quik-20261022-LIST_GBA-1' OR 1=1", "quik-20261022-../x-1"} {
		if _, _, _, err := ParseOrderKey(key); err == nil {
			t.Fatal("invalid key accepted", key)
		}
	}
	if BrokerOrderFirm("A", " B ") != "B" || BrokerOrderFirm(" A ", "") != "A" {
		t.Fatal("unexpected scope")
	}
}

func TestOrderChangesDoNotModifyRows(t *testing.T) {
	old := Order{Raw: json.RawMessage(`{"OrderNum":1,"FirmId":"A","State":"Активна"}`)}
	next := Order{Raw: json.RawMessage(`{"OrderNum":1,"FirmId":"B","State":"Активна"}`)}
	changes := orderChanges(map[string]Order{"same": old}, map[string]Order{"same": next})
	if len(changes) != 2 || !changes[0].Removed || !bytes.Equal(changes[0].Order.Raw, old.Raw) || changes[1].Removed || !bytes.Equal(changes[1].Order.Raw, next.Raw) {
		t.Fatal(changes)
	}
	changes = orderChanges(map[string]Order{"same": old}, map[string]Order{})
	if len(changes) != 1 || !changes[0].Removed || !bytes.Equal(changes[0].Order.Raw, old.Raw) {
		t.Fatal("removal changed the row", changes)
	}
}

func TestOrderFeedOutageDoesNotDeleteRows(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	o := Order{Raw: json.RawMessage(`{"OrderNum":1,"FirmId":"2040"}`)}
	calls, failures := 0, 0
	changes := []OrderChange{}
	runOrderFeed(ctx, func(context.Context, OrderFilter) (OrderPage, error) {
		calls++
		switch calls {
		case 1, 3:
			return OrderPage{Orders: []Order{o}}, nil
		case 2:
			return OrderPage{}, errors.New("database offline")
		default:
			cancel()
			return OrderPage{Orders: []Order{}}, nil
		}
	}, time.Millisecond, func(o OrderChange) { changes = append(changes, o) }, func(error) { failures++ })
	// Call 1 is the baseline, the outage keeps it, call 3 is unchanged and only
	// the real disappearance in call 4 is published.
	if failures != 1 || len(changes) != 1 || !changes[0].Removed || !bytes.Equal(changes[0].Order.Raw, o.Raw) {
		t.Fatal(calls, failures, changes)
	}
}

func TestOrderFeedDoesNotReplayInitialSelection(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := Order{Raw: json.RawMessage(`{"TradeDate":"2026-10-22","ClassCode":"X","OrderNum":1}`)}
	b := Order{Raw: json.RawMessage(`{"TradeDate":"2026-10-22","ClassCode":"X","OrderNum":2}`)}
	calls := 0
	changes := []OrderChange{}
	runOrderFeed(ctx, func(context.Context, OrderFilter) (OrderPage, error) {
		calls++
		if calls == 1 {
			return OrderPage{Orders: []Order{a}}, nil
		}
		cancel()
		return OrderPage{Orders: []Order{a, b}}, nil
	}, time.Millisecond, func(o OrderChange) { changes = append(changes, o) }, nil)
	if len(changes) != 1 || changes[0].Removed || !bytes.Equal(changes[0].Order.Raw, b.Raw) {
		t.Fatal("only the row added after the baseline must be published", changes)
	}
}
