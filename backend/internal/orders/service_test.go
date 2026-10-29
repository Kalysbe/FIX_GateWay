package orders

import (
	"strings"
	"testing"

	"github.com/fix-gateway/backend/internal/domain"
	"github.com/shopspring/decimal"
)

func TestCreateRequestValidation(t *testing.T) {
	qty := decimal.NewFromInt(10)
	price := decimal.NewFromInt(100)

	ok := CreateOrderRequest{
		AccountID: "1-3101-006", ClientCode: "006",
		Symbol: "SBER", Board: "TQBR", Side: domain.SideBuy, OrderType: domain.OrderTypeLimit,
		Quantity: qty, Price: &price,
	}
	if err := ValidateCreateRequest(ok); err != nil {
		t.Fatal(err)
	}
	missingClient := ok
	missingClient.ClientCode = ""
	if err := ValidateCreateRequest(missingClient); err == nil {
		t.Fatal("expected missing client_code error")
	}
	if err := ValidateCreateRequest(CreateOrderRequest{AccountID: "1-3101-006", ClientCode: "006", Side: domain.SideBuy, OrderType: domain.OrderTypeMarket, Quantity: qty, Symbol: "SBER", Board: "TQBR"}); err != nil {
		t.Fatalf("MARKET order without broker id must be valid: %v", err)
	}
	noPrice := ok
	noPrice.Price = nil
	if err := ValidateCreateRequest(noPrice); err == nil {
		t.Fatal("expected price required")
	}
	if err := ValidateCreateRequest(CreateOrderRequest{AccountID: "", ClientCode: "006", Symbol: "SBER", Board: "TQBR", Side: domain.SideBuy, OrderType: domain.OrderTypeMarket, Quantity: qty}); err == nil {
		t.Fatal("expected account_id error")
	}
	noBoard := ok
	noBoard.Board = ""
	noBoard.OrderType = domain.OrderTypeMarket
	noBoard.Price = nil
	if err := ValidateCreateRequest(noBoard); err == nil {
		t.Fatal("expected board required")
	}
	for name, mutate := range map[string]func(*CreateOrderRequest){
		"client_code": func(r *CreateOrderRequest) { r.ClientCode = "006\x015018=OTHER\x011=1-3101-999" },
		"board":       func(r *CreateOrderRequest) { r.Board = "TQBR\x015018=OTHER" },
		"account_id":  func(r *CreateOrderRequest) { r.AccountID = "1-3101-006\x01" },
		"symbol":      func(r *CreateOrderRequest) { r.Symbol = "SBER\n" },
	} {
		injected := ok
		mutate(&injected)
		if err := ValidateCreateRequest(injected); err == nil {
			t.Fatalf("%s with control characters must be rejected", name)
		}
	}
}

// Operation and Price decide side and order type; the term is always DAY.
func TestDeriveFIXFields(t *testing.T) {
	price := decimal.NewFromInt(400)
	for _, tc := range []struct {
		op    string
		price *decimal.Decimal
		side  domain.Side
		typ   domain.OrderType
	}{
		{OperationBuy, &price, domain.SideBuy, domain.OrderTypeLimit},
		{OperationSell, nil, domain.SideSell, domain.OrderTypeMarket},
		{"BUY", &price, "", domain.OrderTypeLimit},
	} {
		r := CreateOrderRequest{Operation: tc.op, Price: tc.price}
		r.deriveFIXFields()
		if r.Side != tc.side || r.OrderType != tc.typ || r.TimeInForce != "DAY" {
			t.Fatalf("%s: %+v", tc.op, r)
		}
	}
}

func TestIdempotencyKeyValidation(t *testing.T) {
	for _, key := range []string{"", "IDEM-1", strings.Repeat("k", MaxIdempotencyKeyLen)} {
		if err := ValidateIdempotencyKey(key); err != nil {
			t.Fatalf("key %q: %v", key, err)
		}
	}
	for _, key := range []string{"k\x01", "k\n", strings.Repeat("k", MaxIdempotencyKeyLen+1)} {
		if err := ValidateIdempotencyKey(key); err == nil {
			t.Fatalf("key %q must be rejected", key)
		}
	}
}
