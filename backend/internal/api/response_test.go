package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fix-gateway/backend/internal/domain"
	"github.com/fix-gateway/backend/internal/orders"
	"github.com/shopspring/decimal"
)

func TestDecodeJSONBody_AccountIDStringOK(t *testing.T) {
	body := `{"ClassCode":"LIST_GBA","SecCode":"GBA02270915","Operation":"Купля","Price":400,"Qty":1,"Account":"1-3101-2040","ClientCode":"006"}`
	r := httptest.NewRequest("POST", "/api/v1/orders", bytes.NewBufferString(body))
	var req orders.CreateOrderRequest
	if err := decodeJSONBody(r, &req); err != nil {
		t.Fatal(err)
	}
	if req.AccountID != "1-3101-2040" || req.Board != "LIST_GBA" || req.Symbol != "GBA02270915" || req.Operation != "Купля" ||
		req.Price == nil || req.Price.String() != "400" || req.Quantity.String() != "1" || req.ClientCode != "006" {
		t.Fatalf("got %+v", req)
	}
}

// A refused order is answered with the reason only: {"error": ...}.
func TestCreateOrderBadBodyIsErrorOnly(t *testing.T) {
	r := httptest.NewRequest("POST", "/api/v1/orders", bytes.NewBufferString(`{"account_id":"x"}`))
	r = r.WithContext(context.WithValue(r.Context(), ctxBroker, domain.Broker{ID: 1}))
	w := httptest.NewRecorder()
	(&Server{}).createOrder(w, r)
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || w.Code != 400 || len(body) != 1 ||
		!strings.Contains(body["error"].(string), "Account") {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}

// The previous field names are refused rather than silently ignored.
func TestDecodeJSONBody_OldFieldNamesRefused(t *testing.T) {
	for _, field := range []string{"account_id", "symbol", "board", "side", "order_type", "time_in_force", "quantity", "price", "client_code"} {
		body := `{"` + field + `":"x"}`
		var req orders.CreateOrderRequest
		if err := decodeJSONBody(httptest.NewRequest("POST", "/api/v1/orders", bytes.NewBufferString(body)), &req); err == nil {
			t.Fatalf("%s must be an unknown field", field)
		}
	}
}

func TestDecodeJSONBody_UnknownField(t *testing.T) {
	body := `{"ClassCode":"LIST_GBA","SecCode":"GBA02270915","Operation":"Купля","Qty":1,"Account":"1-3101-2040","ClientCode":"006","foo":1}`
	r := httptest.NewRequest("POST", "/api/v1/orders", bytes.NewBufferString(body))
	var req orders.CreateOrderRequest
	err := decodeJSONBody(r, &req)
	if err == nil || !strings.Contains(err.Error(), "неизвестное поле") {
		t.Fatalf("expected unknown field error, got %v", err)
	}
}

// POST /orders replies with the QUIK outcome, not with the queued order.
func TestWriteOrderAnswer(t *testing.T) {
	reason := "Недостаточно средств"
	for _, tc := range []struct {
		status domain.OrderStatus
		reason *string
		code   int
		want   string
	}{
		{domain.OrderAcknowledged, nil, 201, `"State":"Активна"`},
		{domain.OrderFilled, nil, 201, `"State":"Исполнена"`},
		{domain.OrderRejected, &reason, 422, `"RejectReason":"` + reason},
		{domain.OrderFailed, nil, 502, "ORDER_FAILED"},
		{domain.OrderQueued, nil, 202, "QUIK_ANSWER_PENDING"},
		{domain.OrderSent, nil, 202, "QUIK_ANSWER_PENDING"},
	} {
		w := httptest.NewRecorder()
		writeOrderAnswer(w, domain.Order{Status: tc.status, RejectReason: tc.reason})
		if w.Code != tc.code || !strings.Contains(w.Body.String(), tc.want) {
			t.Fatalf("%s: %d %s", tc.status, w.Code, w.Body.String())
		}
	}
}

// POST /orders/{id}/cancel replies with the QUIK outcome of the cancel.
func TestWriteCancelAnswer(t *testing.T) {
	for _, tc := range []struct {
		status domain.OrderStatus
		code   int
		want   string
	}{
		{domain.OrderCancelled, 200, `"State":"Снята"`},
		{domain.OrderCancelPending, 202, "QUIK_ANSWER_PENDING"},
		{domain.OrderAcknowledged, 409, "CANCEL_REJECTED"},
		{domain.OrderFilled, 409, `"State":"Исполнена"`},
	} {
		w := httptest.NewRecorder()
		writeCancelAnswer(w, domain.Order{Status: tc.status}, "")
		if w.Code != tc.code || !strings.Contains(w.Body.String(), tc.want) {
			t.Fatalf("%s: %d %s", tc.status, w.Code, w.Body.String())
		}
	}
}

// QUIK's reason code goes to error_code; the text keeps no FIX marker.
func TestRejectCodes(t *testing.T) {
	reason := "Вам запрещена работа по данному торговому счету; OrdRejReason=8637225"
	w := httptest.NewRecorder()
	writeOrderAnswer(w, domain.Order{Status: domain.OrderRejected, RejectReason: &reason})
	body := w.Body.String()
	if !strings.Contains(body, `"error_code":8637225`) || !strings.Contains(body, `"error":"Вам запрещена работа по данному торговому счету"`) {
		t.Fatal(body)
	}
	noCode := "[QME] Цена вне границ"
	w = httptest.NewRecorder()
	writeOrderAnswer(w, domain.Order{Status: domain.OrderRejected, RejectReason: &noCode})
	if !strings.Contains(w.Body.String(), `"error_code":null`) {
		t.Fatal(w.Body.String())
	}
	w = httptest.NewRecorder()
	writeCancelAnswer(w, domain.Order{Status: domain.OrderFilled}, cancelRejectReason("Too late to cancel", "0"))
	if body := w.Body.String(); !strings.Contains(body, `"error_code":0`) || !strings.Contains(body, `"error":"Too late to cancel"`) {
		t.Fatal(body)
	}
}

// GET /api/v1/errors lists every reply code and the known QUIK error codes.
func TestListErrorCodes(t *testing.T) {
	w := serveEnveloped((&Server{}).listErrorCodes, "/api/v1/errors")
	var env struct {
		Success bool `json:"success"`
		Data    struct {
			Codes      []gatewayCode   `json:"codes"`
			ErrorCodes []quikErrorCode `json:"error_codes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil || !env.Success {
		t.Fatal(w.Body.String())
	}
	seen := map[string]bool{}
	for _, c := range env.Data.Codes {
		seen[c.Code] = true
	}
	for _, code := range statusCodes {
		if !seen[code] {
			t.Errorf("code %s missing from /api/v1/errors", code)
		}
	}
	if len(env.Data.ErrorCodes) == 0 {
		t.Fatal("no QUIK error codes")
	}
}

// The reply uses QUIK Orders names and JSON numbers, like an Orders row.
func TestOrderReplyQUIKNames(t *testing.T) {
	price, num := decimal.RequireFromString("400.5"), "26102912345"
	o := domain.Order{Board: "LIST_GBA", Symbol: "GBA02270915", Side: domain.SideSell, Price: &price,
		Quantity: decimal.NewFromInt(3), FilledQuantity: decimal.NewFromInt(1), QuikOrderID: &num,
		TradingAccount: "1-3101-71", ClientCode: "SENTI", QuikFirm: "SENTI2004", Status: domain.OrderPartiallyFilled}
	b, err := json.Marshal(orderReply(o))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"OrderNum":26102912345`, `"State":"Активна"`, `"ClassCode":"LIST_GBA"`,
		`"SecCode":"GBA02270915"`, `"Operation":"Продажа"`, `"Price":400.5`, `"Qty":3`, `"Balance":2`,
		`"Account":"1-3101-71"`, `"ClientCode":"SENTI"`, `"FirmId":"SENTI2004"`, `"RejectReason":null`} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("missing %s in %s", want, b)
		}
	}
	o.Price, o.QuikOrderID, o.Status = nil, nil, domain.OrderSent
	b, _ = json.Marshal(orderReply(o))
	if !strings.Contains(string(b), `"Price":null`) || !strings.Contains(string(b), `"OrderNum":null`) || !strings.Contains(string(b), `"State":null`) {
		t.Fatalf("market order before QUIK answer: %s", b)
	}
}

// client_order_id is generated by the gateway; a broker-supplied one is refused.
func TestDecodeJSONBody_ClientOrderIDRejected(t *testing.T) {
	body := `{"Account":"1-3101-2040","client_order_id":"x","ClassCode":"LIST_GBA","SecCode":"GBA02270915","Operation":"Купля","Qty":1,"Price":400}`
	r := httptest.NewRequest("POST", "/api/v1/orders", bytes.NewBufferString(body))
	var req orders.CreateOrderRequest
	if err := decodeJSONBody(r, &req); err == nil || !strings.Contains(err.Error(), "client_order_id") {
		t.Fatalf("expected client_order_id to be an unknown field, got %v", err)
	}
}
