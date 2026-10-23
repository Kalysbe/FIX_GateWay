package domain

import (
	"strings"
	"testing"
)

func TestCheckFIXTextRejectsFramingCharacters(t *testing.T) {
	for _, v := range []string{"006\x015018=OTHER", "a\nb", "a\rb", "a\x00", "a\x7f", "a\tb", string([]byte{0xff})} {
		if err := CheckFIXText("client_code", v, MaxFIXTextLen); err == nil {
			t.Fatalf("%q must be rejected", v)
		}
	}
	for _, v := range []string{"", "1-3101-006", "kalys", "LISTEDSTOCKK", "Клиент 1", "a=b"} {
		if err := CheckFIXText("client_code", v, MaxFIXTextLen); err != nil {
			t.Fatalf("%q: %v", v, err)
		}
	}
	if err := CheckFIXText("board", strings.Repeat("Я", MaxFIXTextLen), MaxFIXTextLen); err != nil {
		t.Fatalf("limit counts runes: %v", err)
	}
	if err := CheckFIXText("board", strings.Repeat("A", MaxFIXTextLen+1), MaxFIXTextLen); err == nil {
		t.Fatal("too long value must be rejected")
	}
	if err := CheckFIXText("client_order_id", strings.Repeat("A", 500), 0); err != nil {
		t.Fatalf("maxLen 0 disables the limit: %v", err)
	}
}

func TestCheckPositionMaintainTextChecksBlockKeysAndItems(t *testing.T) {
	ok := PositionMaintainRequest{Firm: "2040", ClientCode: "kalys", Action: "2", TransType: "3",
		Blocks: []PositionBlock{{PosType: "ML", UpdateAction: "1", Keys: map[string]string{"CurrCode": "KGS"}, Items: map[string]string{"Limit": "10"}}}}
	if err := CheckPositionMaintainText(ok); err != nil {
		t.Fatal(err)
	}
	bad := ok
	bad.Blocks = []PositionBlock{{PosType: "ML", Keys: map[string]string{"Tag": "EQTV\x015018=X"}}}
	if err := CheckPositionMaintainText(bad); err == nil {
		t.Fatal("SOH in a position key value must be rejected")
	}
	bad = ok
	bad.ClientCode = "kalys\x011=OTHER"
	if err := CheckPositionMaintainText(bad); err == nil {
		t.Fatal("SOH in client_code must be rejected")
	}
}

func TestCheckMarketDataQueryTextChecksInstruments(t *testing.T) {
	q := MarketDataQuery{Board: "TQBR", Instruments: []SecurityRef{{Symbol: "SBER", Board: "TQBR"}}}
	if err := CheckMarketDataQueryText(q); err != nil {
		t.Fatal(err)
	}
	q.Instruments[0].Symbol = "SBER\x01100=X"
	if err := CheckMarketDataQueryText(q); err == nil {
		t.Fatal("SOH in instrument symbol must be rejected")
	}
}
