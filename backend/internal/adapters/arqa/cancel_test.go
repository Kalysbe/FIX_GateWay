package arqa

import (
	"testing"

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
