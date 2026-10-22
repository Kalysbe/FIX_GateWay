package arqa

import (
	"strings"
	"testing"

	"golang.org/x/text/encoding/charmap"
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

func TestSecurityDefinitionUnderlyingAndSymbolAreOneInstrument(t *testing.T) {
	name := "Золотой слиток 5 грамм"
	encoded, err := charmap.Windows1251.NewEncoder().String(name)
	if err != nil {
		t.Fatal(err)
	}
	// Real .84 layout: 311 and 55 both occur inside each 146 group.
	raw := strings.Join([]string{
		"35=d", "393=2", "146=2",
		"311=" + encoded, "100=QME_MTL", "309=GOLD010101", "318=KGS", "310=FXSPOT", "308=QME_MTL",
		"55=" + encoded, "107=" + encoded,
		"311=Government bond", "100=LIST_GBA", "309=GBA02270915", "310=GO", "308=LIST_GBA",
		"55=Government bond", "107=GBA02270915", "10=000",
	}, "\x01")
	items := parseRelatedSymFromRaw(raw)
	if len(items) != 2 || items[0].SecurityID != "GOLD010101" || items[0].Board != "QME_MTL" || items[0].Name != name || items[0].Symbol != name {
		t.Fatalf("duplicate instrument or lost Windows-1251 text: %+v", items)
	}
	if items[1].SecurityID != "GBA02270915" || items[1].Board != "LIST_GBA" || items[1].Name != "GBA02270915" {
		t.Fatal(items[1])
	}
}

func TestSecurityDefinitionSymbolDelimitedGroupsStillParse(t *testing.T) {
	raw := "35=d\x01146=2\x0155=AAA\x0148=AAA-ID\x01100=BOARD\x0155=BBB\x0148=BBB-ID\x01100=OTHER\x01"
	items := parseRelatedSymFromRaw(raw)
	if len(items) != 2 || items[0].SecurityID != "AAA-ID" || items[1].SecurityID != "BBB-ID" || items[1].Board != "OTHER" {
		t.Fatal(items)
	}
}
