package quikexport

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"
)

// The feed publishes only rows that appear or change after the baseline.
func TestRunTradeFeedPublishesNewRows(t *testing.T) {
	side := func(num, op, firm string) Order {
		return Order{Raw: json.RawMessage(`{"TradeNum":` + num + `,"TradeDate":"2026-10-29T00:00:00","ClassCode":"LIST_GBA","Operation":"` + op + `","FirmId":"` + firm + `","Qty":1}`)}
	}
	reads := [][]Order{
		{side("1", "Купля", "A")},
		{side("1", "Купля", "A"), side("2", "Купля", "A"), side("2", "Продажа", "B")},
		{side("1", "Купля", "A"), side("2", "Купля", "A"), side("2", "Продажа", "B")},
	}
	var mu sync.Mutex
	var got []string
	n := 0
	ctx, cancel := context.WithCancel(context.Background())
	read := func(context.Context, TradeFilter) (TradePage, error) {
		mu.Lock()
		defer mu.Unlock()
		if n >= len(reads) {
			cancel()
			return TradePage{Trades: reads[len(reads)-1]}, nil
		}
		n++
		return TradePage{Trades: reads[n-1]}, nil
	}
	runTradeFeed(ctx, read, time.Millisecond, func(t Order) {
		mu.Lock()
		got = append(got, t.Text("TradeNum")+"/"+t.Text("FirmId"))
		mu.Unlock()
	}, nil)
	if len(got) != 2 || got[0] == got[1] {
		t.Fatalf("want the two sides of trade 2 once each, got %v", got)
	}
	for _, g := range got {
		if g != "2/A" && g != "2/B" {
			t.Fatalf("unexpected publication %s", g)
		}
	}
}
