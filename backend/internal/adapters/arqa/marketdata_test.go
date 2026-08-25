package arqa

import (
	"strings"
	"testing"

	"github.com/fix-gateway/backend/internal/domain"
)

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
