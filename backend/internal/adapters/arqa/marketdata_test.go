package arqa

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/fix-gateway/backend/internal/domain"
	"golang.org/x/text/encoding/charmap"
)

func TestMarketDataCyrillicSurvivesFIXToJSON(t *testing.T) {
	name := "ГКО-2 выпуск (GBA02270915)"
	legacy, err := charmap.Windows1251.NewEncoder().String(name)
	if err != nil {
		t.Fatal(err)
	}
	for _, wireName := range []string{name, legacy} {
		raw := strings.Join([]string{
			"35=X", "262=MD-text", "268=1", "279=0", "269=5", "270=93.1300",
			"22=8", "48=GBA02270915", "55=" + wireName, "100=LIST_GBA",
		}, "\x01") + "\x01"
		_, rows := parseMarketDataFromRaw(raw, "a1", true)
		if len(rows) != 1 {
			t.Fatal(rows)
		}
		// json.Marshal hides invalid bytes; the TEXT column raw_fix does not.
		if !utf8.ValidString(rows[0].RawFIX) || !strings.Contains(rows[0].RawFIX, "55="+name+"|") {
			t.Fatalf("raw_fix is not UTF-8 text: %q", rows[0].RawFIX)
		}
		data, err := json.Marshal(rows[0])
		if err != nil {
			t.Fatal(err)
		}
		var response domain.MarketQuote
		if err := json.Unmarshal(data, &response); err != nil {
			t.Fatal(err)
		}
		if response.Symbol != name || response.SecurityID != "GBA02270915" || response.Board != "LIST_GBA" || response.Close != "93.1300" {
			t.Fatalf("corrupted FIX/API text or instrument fields: %+v", response)
		}
	}
}

func TestParseMarketDataSnapshot_ArqaUnderlyingTags(t *testing.T) {
	raw := strings.Join([]string{
		"8=FIX.4.2", "9=200", "35=W", "49=QUIK", "56=TEST", "34=20",
		"262=MD-GBA", "311=GBA02280810", "305=8", "100=LIST_GBA",
		"309=GBA02280810", "308=LIST_GBA", "310=GO", "318=KGS",
		"268=2",
		"269=0", "270=99.5", "271=10",
		"269=1", "270=100.5", "271=12",
		"10=000",
	}, "\x01")
	_, quotes := parseMarketDataFromRaw(raw, "a1", false)
	if len(quotes) != 1 {
		t.Fatalf("got %d quotes: %+v", len(quotes), quotes)
	}
	q := quotes[0]
	if q.SecurityID != "GBA02280810" || q.Symbol != "GBA02280810" || q.Board != "LIST_GBA" {
		t.Fatalf("instrument %+v", q)
	}
	if q.Bid != "99.5" || q.Offer != "100.5" {
		t.Fatalf("book %+v", q)
	}
	if len(q.Bids) != 1 || q.Bids[0].Price != "99.5" || q.Bids[0].Size != "10" {
		t.Fatalf("bids %+v", q.Bids)
	}
	if len(q.Offers) != 1 || q.Offers[0].Price != "100.5" {
		t.Fatalf("offers %+v", q.Offers)
	}
}

func TestParseMarketDataSnapshot_CurrentTrades(t *testing.T) {
	raw := strings.Join([]string{
		"8=FIX.4.2", "9=200", "35=W", "49=QUIK", "56=TEST", "34=10",
		"262=MD1-1", "22=8", "48=LKOH", "100=BQUOTE", "15=RUB",
		"75=20260813", "326=17", "336=OPEN", "387=12500", "6143=3125000",
		"268=6",
		"269=0", "270=249.5", "271=100",
		"269=1", "270=250.1", "271=80",
		"269=2", "270=250.0", "271=10", "273=10:15:01",
		"269=4", "270=248.0",
		"269=7", "270=251.0",
		"269=8", "270=247.5",
		"10=000",
	}, "\x01")

	reqID, quotes := parseMarketDataFromRaw(raw, "arqa-1", false)
	if reqID != "MD1-1" {
		t.Fatalf("reqID=%s", reqID)
	}
	if len(quotes) != 1 {
		t.Fatalf("got %d quotes: %+v", len(quotes), quotes)
	}
	q := quotes[0]
	if q.SecurityID != "LKOH" || q.Board != "BQUOTE" {
		t.Fatalf("instrument %+v", q)
	}
	if q.Bid != "249.5" || q.BidSize != "100" || q.Offer != "250.1" || q.Last != "250.0" {
		t.Fatalf("book %+v", q)
	}
	if q.Open != "248.0" || q.High != "251.0" || q.Low != "247.5" {
		t.Fatalf("ohlc %+v", q)
	}
	if q.Volume != "12500" || q.Value != "3125000" || q.TradingStatus != "17" {
		t.Fatalf("totals %+v", q)
	}
}

func TestMergeQuote(t *testing.T) {
	a := domain.MarketQuote{Symbol: "SBER", SecurityID: "SBER", Board: "TQBR", Last: "250", TradingStatus: "17"}
	b := domain.MarketQuote{Symbol: "SBER", SecurityID: "SBER", Board: "TQBR", Offer: "251", Bid: "249"}
	out := mergeQuote(a, b)
	if out.Last != "250" || out.Offer != "251" || out.Bid != "249" || out.TradingStatus != "17" {
		t.Fatalf("merged %+v", out)
	}
}

func TestParseIncremental_VolumeInGroup(t *testing.T) {
	raw := strings.Join([]string{
		"8=FIX.4.2", "9=80", "35=X", "262=MD1",
		"268=1", "279=1", "269=2", "270=431.46", "271=2", "273=10:44:01",
		"48=MAIR6", "100=LISTEDSTOCKK", "387=88", "6143=36526",
		"10=000",
	}, "\x01")
	_, quotes := parseMarketDataFromRaw(raw, "a1", true)
	if len(quotes) != 1 {
		t.Fatalf("got %d", len(quotes))
	}
	q := quotes[0]
	if q.Last != "431.46" || q.Volume != "88" || q.Value != "36526" {
		t.Fatalf("%+v", q)
	}
	if q.DeltaVolume != "2" {
		t.Fatalf("delta vol %s", q.DeltaVolume)
	}
}

func TestApplyLive_AccumulatesTurnoverFromLastTrade(t *testing.T) {
	app := &sessionApp{mdBook: map[string]domain.MarketQuote{}}
	app.mdBook["LISTEDSTOCKK|MAIR6"] = domain.MarketQuote{
		Symbol: "MAIR6", SecurityID: "MAIR6", Board: "LISTEDSTOCKK",
		Last: "430", Volume: "86", Value: "35664",
	}
	_, quotes := parseMarketDataFromRaw(strings.Join([]string{
		"8=FIX.4.2", "35=X", "262=MD1",
		"268=1", "279=0", "269=2", "270=431.46", "271=2",
		"48=MAIR6", "100=LISTEDSTOCKK",
		"10=000",
	}, "\x01"), "a1", true)
	app.applyLive(quotes)
	got := app.mdBook["LISTEDSTOCKK|MAIR6"]
	if got.Last != "431.46" {
		t.Fatalf("last %s", got.Last)
	}
	if got.Volume != "88" { // 86+2
		t.Fatalf("volume %s", got.Volume)
	}
	if got.Value != "36526.92" && got.Value != "36526.92000000001" { // 35664 + 431.46*2
		// allow formatted float
		n, _ := parseFixNum(got.Value)
		if n < 36526 || n > 36527 {
			t.Fatalf("value %s", got.Value)
		}
	}
}

func TestBuildMarketDataRequest_AllClassesOmitsRelatedSym(t *testing.T) {
	raw := strings.ReplaceAll(buildMarketDataRequest("MD1", domain.MarketDataQuery{}).String(), "\x01", "|")
	if strings.Contains(raw, "146=") {
		t.Fatalf("empty query must not pin 146: %s", raw)
	}
	if strings.Contains(raw, "100=") {
		t.Fatalf("empty query must not set a single class: %s", raw)
	}
}

func TestBuildMarketDataRequest_BoardOnly(t *testing.T) {
	raw := strings.ReplaceAll(buildMarketDataRequest("MD1", domain.MarketDataQuery{Board: "LIST_GBA"}).String(), "\x01", "|")
	if strings.Contains(raw, "146=") {
		t.Fatalf("board-only must not send 146 without SecurityID 48: %s", raw)
	}
	if strings.Contains(raw, "48=") {
		t.Fatalf("board-only must not invent a SecurityID: %s", raw)
	}
}

func TestBuildMarketDataRequest_GovtType(t *testing.T) {
	msg := buildMarketDataRequest("MD1", domain.MarketDataQuery{
		Instruments: []domain.SecurityRef{
			{Symbol: "GBA02280810", Board: "LIST_GBA", SecurityType: "GO"},
		},
	})
	raw := strings.ReplaceAll(msg.String(), "\x01", "|")
	if strings.Contains(raw, "146=") {
		t.Fatalf("single instrument must not send 146: %s", raw)
	}
	if !strings.Contains(raw, "167=GO") {
		t.Fatalf("expected 167=GO in %s", raw)
	}
	if strings.Contains(raw, "167=GOVT") {
		t.Fatalf("ARQA uses GO not GOVT: %s", raw)
	}
	if !strings.Contains(raw, "100=LIST_GBA") {
		t.Fatalf("expected 100=LIST_GBA in %s", raw)
	}
	if !strings.Contains(raw, "48=GBA02280810") {
		t.Fatalf("expected 48=GBA02280810 in %s", raw)
	}
	if !strings.Contains(raw, "264=0") {
		t.Fatalf("expected full book 264=0 in %s", raw)
	}
}

func TestMarketDataSubscribeBatches_BoardsAndInstruments(t *testing.T) {
	batches := marketDataSubscribeBatches(domain.MarketDataQuery{
		Instruments: []domain.SecurityRef{{Symbol: "MAIR6", Board: "LISTEDSTOCKK"}},
		Boards:      []string{"LISTEDSTOCKK", "LIST_GBA", "LIST_GBA"},
	})
	if len(batches) != 1 {
		t.Fatalf("len=%d %#v", len(batches), batches)
	}
	if len(batches[0].Instruments) != 1 {
		t.Fatalf("instrument batch: %#v", batches[0])
	}
}

func TestParseMarketDataSnapshot_FullBook(t *testing.T) {
	raw := strings.Join([]string{
		"8=FIX.4.2", "9=200", "35=W", "49=QUIK", "56=TEST", "34=20",
		"262=MD-BOOK", "48=MAIR6", "100=LISTEDSTOCKK",
		"268=6",
		"269=0", "270=99.0", "271=5",
		"269=0", "270=99.5", "271=10",
		"269=0", "270=98.5", "271=7",
		"269=1", "270=101.0", "271=4",
		"269=1", "270=100.5", "271=12",
		"269=2", "270=100.0", "271=1",
		"10=000",
	}, "\x01")
	_, quotes := parseMarketDataFromRaw(raw, "a1", false)
	if len(quotes) != 1 {
		t.Fatalf("got %d quotes: %+v", len(quotes), quotes)
	}
	q := quotes[0]
	if q.Bid != "99.5" || q.BidSize != "10" {
		t.Fatalf("best bid %+v", q)
	}
	if q.Offer != "100.5" || q.OfferSize != "12" {
		t.Fatalf("best offer %+v", q)
	}
	if len(q.Bids) != 3 || q.Bids[0].Price != "99.5" || q.Bids[1].Price != "99.0" || q.Bids[2].Price != "98.5" {
		t.Fatalf("bids %+v", q.Bids)
	}
	if len(q.Offers) != 2 || q.Offers[0].Price != "100.5" || q.Offers[1].Price != "101.0" {
		t.Fatalf("offers %+v", q.Offers)
	}
	if !q.ReplaceBids || !q.ReplaceOffers {
		t.Fatalf("snapshot must replace both sides")
	}
}

func TestApplyLive_IncrementalBookUpdateAndDelete(t *testing.T) {
	app := &sessionApp{mdBook: map[string]domain.MarketQuote{}}
	_, snap := parseMarketDataFromRaw(strings.Join([]string{
		"8=FIX.4.2", "35=W", "262=MD1",
		"48=MAIR6", "100=LISTEDSTOCKK",
		"268=3",
		"269=0", "270=99.5", "271=10",
		"269=0", "270=99.0", "271=5",
		"269=1", "270=100.5", "271=12",
		"10=000",
	}, "\x01"), "a1", false)
	app.applyLive(snap)
	got := app.mdBook["LISTEDSTOCKK|MAIR6"]
	if len(got.Bids) != 2 || got.Bid != "99.5" {
		t.Fatalf("snapshot book %+v", got)
	}

	_, inc := parseMarketDataFromRaw(strings.Join([]string{
		"8=FIX.4.2", "35=X", "262=MD1",
		"268=2",
		"279=1", "269=0", "270=99.5", "271=3", "48=MAIR6", "100=LISTEDSTOCKK",
		"279=2", "269=0", "270=99.0", "48=MAIR6", "100=LISTEDSTOCKK",
		"10=000",
	}, "\x01"), "a1", true)
	app.applyLive(inc)
	got = app.mdBook["LISTEDSTOCKK|MAIR6"]
	if len(got.Bids) != 1 || got.Bids[0].Price != "99.5" || got.Bids[0].Size != "3" {
		t.Fatalf("after incremental %+v", got.Bids)
	}
	if got.Bid != "99.5" || got.BidSize != "3" {
		t.Fatalf("best bid after incremental %+v", got)
	}
	if len(got.Offers) != 1 || got.Offers[0].Price != "100.5" {
		t.Fatalf("offers must survive %+v", got.Offers)
	}
}
