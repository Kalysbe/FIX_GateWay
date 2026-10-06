package orders

import (
	"errors"
	"github.com/shopspring/decimal"
	"testing"
)

func TestTradingRuleBoundariesAndInheritance(t *testing.T) {
	quantity := decimal.RequireFromString("10.125")
	open := 3
	global := TradingLimit{MaxQuantity: &quantity, MaxOpenOrders: &open}
	generous := decimal.NewFromInt(100)
	moreOpen := 100
	broker := TradingLimit{BrokerID: 7, MaxQuantity: &generous, MaxOpenOrders: &moreOpen}
	for _, tc := range []struct {
		name, qty string
		count     int
		policies  []TradingLimit
		code      string
	}{
		{"exact quantity boundary", "10.125", 2, []TradingLimit{global, broker}, ""},
		{"decimal above boundary", "10.12500001", 0, []TradingLimit{global, broker}, "MAX_ORDER_QUANTITY"},
		{"open boundary", "1", 3, []TradingLimit{global, broker}, "MAX_OPEN_ORDERS"},
		{"released capacity", "1", 2, []TradingLimit{global, broker}, ""},
		{"global pause cannot be lifted by broker", "1", 0, []TradingLimit{{Paused: true}, broker}, "TRADING_PAUSED"},
		{"broker pause", "1", 0, []TradingLimit{{}, {BrokerID: 7, Paused: true}}, "TRADING_PAUSED"},
		{"broker stricter than global", "10.12500001", 0, []TradingLimit{{MaxQuantity: &generous}, {BrokerID: 7, MaxQuantity: &quantity}}, "MAX_ORDER_QUANTITY"},
		{"unlimited does not count", "100000", 100000, []TradingLimit{{}, {}}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			err := evaluateTradingLimits(CreateInput{BrokerID: 7, Quantity: decimal.RequireFromString(tc.qty)}, tc.policies, func() (int, error) { calls++; return tc.count, nil })
			if tc.code == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else {
				var rejection *RiskRejected
				if !errors.As(err, &rejection) || rejection.Code != tc.code {
					t.Fatalf("want %s got %v", tc.code, err)
				}
			}
			if calls > 1 {
				t.Fatal("count queried more than once")
			}
			if tc.name == "unlimited does not count" && calls != 0 {
				t.Fatal("unlimited policy queried capacity")
			}
		})
	}
}

func TestTradingCapacityFailureIsNotAcceptance(t *testing.T) {
	limit := 3
	unavailable := errors.New("database unavailable")
	err := evaluateTradingLimits(CreateInput{Quantity: decimal.NewFromInt(1)}, []TradingLimit{{MaxOpenOrders: &limit}}, func() (int, error) { return 0, unavailable })
	if !errors.Is(err, unavailable) {
		t.Fatalf("storage failure lost: %v", err)
	}
}
