package arqa

import (
	"testing"

	"github.com/quickfixgo/enum"
	"github.com/quickfixgo/quickfix"
	"github.com/quickfixgo/tag"
)

func TestLooksLikeISIN(t *testing.T) {
	if !looksLikeISIN("KG0106166315") {
		t.Fatal("expected ISIN")
	}
	if looksLikeISIN("AAPL") {
		t.Fatal("ticker should not be ISIN")
	}
}

func TestBuildNewOrderSingleClientAndFirm(t *testing.T) {
	msg := buildNewOrderSingle("C1", "1-3101-2040", "Broker1", "2040", "KG0105166315", "LISTEDSTOCKK", "BUY", "LIMIT", "1", "400", "DAY", "KGS")
	if _, err := msg.Body.GetString(tag.Symbol); err == nil {
		t.Fatal("expected no Symbol (55)")
	}
	if _, err := msg.Body.GetString(tag.SecurityExchange); err == nil {
		t.Fatal("expected no SecurityExchange (207)")
	}
	sid, err := msg.Body.GetString(tag.SecurityID)
	if err != nil || sid != "KG0105166315" {
		t.Fatalf("SecurityID: %q err=%v", sid, err)
	}
	src, err := msg.Body.GetString(tag.SecurityIDSource)
	if err != nil || src != string(enum.SecurityIDSource_EXCHANGE_SYMBOL) {
		t.Fatalf("SecurityIDSource: %q err=%v", src, err)
	}
	dest, err := msg.Body.GetString(tag.ExDestination)
	if err != nil || dest != "LISTEDSTOCKK" {
		t.Fatalf("ExDestination: %q err=%v", dest, err)
	}
	qty, err := msg.Body.GetString(tag.OrderQty)
	if err != nil || qty != "1" {
		t.Fatalf("OrderQty: %q err=%v", qty, err)
	}
	px, err := msg.Body.GetString(tag.Price)
	if err != nil || px != "400" {
		t.Fatalf("Price: %q err=%v", px, err)
	}
	cid, err := msg.Body.GetString(tag.ClientID)
	if err != nil || cid != "Broker1" {
		t.Fatalf("ClientID: %q err=%v", cid, err)
	}
	firm, err := msg.Body.GetString(quickfix.Tag(5018))
	if err != nil || firm != "2040" {
		t.Fatalf("5018: %q err=%v", firm, err)
	}
	// No silent fallback: empty client/firm stay empty.
	msg2 := buildNewOrderSingle("C2", "1-3101-006", "", "", "SBER", "TQBR", "BUY", "LIMIT", "1", "400", "DAY", "RUB")
	if _, err := msg2.Body.GetString(tag.ClientID); err == nil {
		t.Fatal("expected missing ClientID when empty")
	}
	if _, err := msg2.Body.GetString(quickfix.Tag(5018)); err == nil {
		t.Fatal("expected missing 5018 when empty")
	}
}
