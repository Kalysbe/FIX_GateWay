package arqa

import (
	"strings"
	"testing"
	"time"

	"github.com/fix-gateway/backend/internal/domain"
	"github.com/quickfixgo/quickfix"
	"github.com/quickfixgo/tag"
)

func TestBuildCancelUsesARQATags(t *testing.T) {
	msg := buildCancel(
		"G1492c075-b29a-4e59-b4a1-76b40702-C",
		"G1492c075-b29a-4e59-b4a1-76b40702",
		"260818-LISTEDSTOCKK-1671",
		"KG0106166315",
		"LISTEDSTOCKK",
		"BUY",
		"1-3101-2040",
		"kalys",
		"2040",
		"1",
	)
	if _, err := msg.Body.GetString(tag.Symbol); err == nil {
		t.Fatal("cancel must not send FIX 55 Symbol")
	}
	if v, _ := msg.Body.GetString(tag.SecurityID); v != "KG0106166315" {
		t.Fatalf("48 SecurityID=%s", v)
	}
	if v, _ := msg.Body.GetString(tag.SecurityIDSource); v != "8" {
		t.Fatalf("22 SecurityIDSource=%s", v)
	}
	if v, _ := msg.Body.GetString(tag.ExDestination); v != "LISTEDSTOCKK" {
		t.Fatalf("100 ExDestination=%s", v)
	}
	if v, _ := msg.Body.GetString(tag.OrigClOrdID); v != "G1492c075-b29a-4e59-b4a1-76b40702" {
		t.Fatalf("41 OrigClOrdID=%s", v)
	}
	if v, _ := msg.Body.GetString(tag.Account); v != "1-3101-2040" {
		t.Fatalf("1 Account=%s", v)
	}
	if v, _ := msg.Body.GetString(tag.ClientID); v != "kalys" {
		t.Fatalf("109 ClientID=%s", v)
	}
	firm, err := msg.Body.GetString(quickfix.Tag(5018))
	if err != nil || firm != "2040" {
		t.Fatalf("5018 firm=%s err=%v", firm, err)
	}
	if v, _ := msg.Body.GetString(tag.OrderQty); v != "1" {
		t.Fatalf("38 OrderQty=%s", v)
	}
}

func TestCancelClOrdIDIsUniquePerAttempt(t *testing.T) {
	orig := "G1492c075b29a4e59b4a176b40702abcd"
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	first := cancelClOrdID(orig, now)
	retry := cancelClOrdID(orig, now.Add(2*time.Second))
	if first == retry {
		t.Fatalf("retry reused ClOrdID %s", first)
	}
	for _, id := range []string{first, retry} {
		// orders.clOrdLookupKeys maps the cancel back to the order by the last "-C".
		if i := strings.LastIndex(id, "-C"); i <= 0 || id[:i] != orig {
			t.Fatalf("%s does not map back to %s", id, orig)
		}
	}
}

func TestOrderWireTextRejectsTagInjection(t *testing.T) {
	order := domain.Order{TradingAccount: "1-3101-006", ClientCode: "006\x015018=OTHER", Symbol: "SBER", Board: "TQBR"}
	if err := checkOrderWireText(order); err == nil {
		t.Fatal("SOH in client_code must not reach the wire")
	}
	order.ClientCode = "006"
	if err := checkOrderWireText(order); err != nil {
		t.Fatal(err)
	}
}
