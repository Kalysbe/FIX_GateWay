package orders

import (
	"testing"

	"github.com/fix-gateway/backend/internal/domain"
	"github.com/shopspring/decimal"
)

func TestCreateRequestValidation(t *testing.T) {
	qty := decimal.NewFromInt(10)
	price := decimal.NewFromInt(100)

	ok := CreateOrderRequest{
		AccountID: "1-3101-006", ClientCode: "006", ClientOrderID: "c1",
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
	if err := ValidateCreateRequest(CreateOrderRequest{AccountID: "1-3101-006", ClientCode: "006", Side: domain.SideBuy, OrderType: domain.OrderTypeMarket, Quantity: qty, Symbol: "SBER", Board: "TQBR"}); err == nil {
		t.Fatal("expected missing client_order_id error")
	}
	noPrice := ok
	noPrice.Price = nil
	if err := ValidateCreateRequest(noPrice); err == nil {
		t.Fatal("expected price required")
	}
	if err := ValidateCreateRequest(CreateOrderRequest{AccountID: "", ClientCode: "006", ClientOrderID: "c1", Symbol: "SBER", Board: "TQBR", Side: domain.SideBuy, OrderType: domain.OrderTypeMarket, Quantity: qty}); err == nil {
		t.Fatal("expected account_id error")
	}
	noBoard := ok
	noBoard.Board = ""
	noBoard.OrderType = domain.OrderTypeMarket
	noBoard.Price = nil
	if err := ValidateCreateRequest(noBoard); err == nil {
		t.Fatal("expected board required")
	}
}
