package api

import (
	"bytes"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fix-gateway/backend/internal/orders"
)

func TestDecodeJSONBody_AccountIDStringOK(t *testing.T) {
	body := `{"account_id":"1-3101-2040","client_order_id":"x","symbol":"SBER","board":"TQBR","side":"BUY","order_type":"LIMIT","quantity":"1","price":"400"}`
	r := httptest.NewRequest("POST", "/api/v1/orders", bytes.NewBufferString(body))
	var req orders.CreateOrderRequest
	if err := decodeJSONBody(r, &req); err != nil {
		t.Fatal(err)
	}
	if req.AccountID != "1-3101-2040" {
		t.Fatalf("got %q", req.AccountID)
	}
}

func TestDecodeJSONBody_UnknownField(t *testing.T) {
	body := `{"account_id":"1-3101-2040","client_order_id":"x","symbol":"SBER","board":"TQBR","side":"BUY","order_type":"LIMIT","quantity":"1","price":"400","foo":1}`
	r := httptest.NewRequest("POST", "/api/v1/orders", bytes.NewBufferString(body))
	var req orders.CreateOrderRequest
	err := decodeJSONBody(r, &req)
	if err == nil || !strings.Contains(err.Error(), "неизвестное поле") {
		t.Fatalf("expected unknown field error, got %v", err)
	}
}
