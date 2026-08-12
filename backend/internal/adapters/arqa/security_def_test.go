package arqa

import (
	"strings"
	"testing"
)

func TestParseRelatedSymFromRaw(t *testing.T) {
	raw := strings.Join([]string{
		"8=FIX.4.2", "9=100", "35=d", "49=QUIK", "56=TEST", "34=1",
		"320=SD-1", "393=2", "146=2",
		"311=KG0106166315", "309=KG0106166315", "308=QME_A1", "318=KGS", "310=CS", "107=MAIR6",
		"311=GBA02270915", "309=GBA02270915", "308=LISTBND", "318=KGS", "310=GOVT", "107=GKO",
		"10=000",
	}, "\x01")

	items := parseRelatedSymFromRaw(raw)
	if len(items) != 2 {
		t.Fatalf("got %d items: %+v", len(items), items)
	}
	if items[0].SecurityID != "KG0106166315" || items[0].Board != "QME_A1" {
		t.Fatalf("item0=%+v", items[0])
	}
	if items[1].SecurityID != "GBA02270915" || items[1].Board != "LISTBND" {
		t.Fatalf("item1=%+v", items[1])
	}
}
