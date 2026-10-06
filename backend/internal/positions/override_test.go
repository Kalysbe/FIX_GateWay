package positions

import (
	"errors"
	"testing"

	"github.com/fix-gateway/backend/internal/domain"
	"github.com/shopspring/decimal"
)

func dec(s string) decimal.Decimal {
	d, err := decimal.NewFromString(s)
	if err != nil {
		panic(err)
	}
	return d
}

func TestBuildCashRequiresFields(t *testing.T) {
	_, err := BuildCash(CashSet{ClientCode: "kalys", Currency: "KGS", Amount: dec("10")})
	if err == nil {
		t.Fatal("expected firm_id error")
	}
	_, err = BuildCash(CashSet{FirmID: "2040", Currency: "KGS", Amount: dec("10")})
	if err == nil {
		t.Fatal("expected client_code error")
	}
	_, err = BuildCash(CashSet{FirmID: "2040", ClientCode: "kalys", Amount: dec("10")})
	if err == nil {
		t.Fatal("expected currency error")
	}
}

func TestBuildCashDefaultsTagAndItems(t *testing.T) {
	p, err := BuildCash(CashSet{
		FirmID: "2040", ClientCode: "kalys", AccountCode: "1-3101-2040",
		Currency: "kgs", Amount: dec("1500000"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.Kind != "CASH" || p.PosType != "ML" || p.Source != SourceManual {
		t.Fatalf("meta %#v", p)
	}
	if p.Currency != "KGS" || p.Symbol != "EQTV" {
		t.Fatalf("currency/tag %s/%s", p.Currency, p.Symbol)
	}
	if p.Keys["TAG"] != "EQTV" || p.Keys["KIND"] != "T0" {
		t.Fatalf("keys %#v", p.Keys)
	}
	if p.NetQty.String() != "1500000" || p.Items["CUR_BAL"] != "1500000" {
		t.Fatalf("amount %#v items %#v", p.NetQty, p.Items)
	}
	if p.Keys["ACC"] != "1-3101-2040" || p.Keys["CLIENT"] != "kalys" {
		t.Fatalf("keys %#v", p.Keys)
	}
}

func TestBuildInstrumentQtyFromNet(t *testing.T) {
	qty := dec("100")
	p, err := BuildInstrument(InstrumentSet{
		FirmID: "2040", ClientCode: "kalys", Symbol: "KG0105166315", Board: "LISTEDSTOCKK",
		Quantity: &qty,
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.Kind != "INSTRUMENT" || p.PosType != "DL" || p.NetQty.String() != "100" || p.LongQty.String() != "100" {
		t.Fatalf("qty %#v", p)
	}
	if p.Items["CUR_BAL"] != "100" || p.Keys["SEC"] != "KG0105166315" || p.Keys["KIND"] != "T0" {
		t.Fatalf("items/keys %#v %#v", p.Items, p.Keys)
	}
	short := dec("5")
	long := dec("12")
	p, err = BuildInstrument(InstrumentSet{
		FirmID: "2040", ClientCode: "kalys", Symbol: "SBER", Board: "TQBR",
		LongQty: &long, ShortQty: &short,
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.NetQty.String() != "7" {
		t.Fatalf("net %s", p.NetQty)
	}
	_, err = BuildInstrument(InstrumentSet{FirmID: "2040", ClientCode: "kalys", Symbol: "SBER"})
	if err == nil {
		t.Fatal("expected qty error")
	}
}

func TestBuildInstrumentRejectsInconsistentBalances(t *testing.T) {
	negative, long, short, net := dec("-1"), dec("12"), dec("5"), dec("8")
	for _, tc := range []struct {
		name             string
		qty, long, short *decimal.Decimal
	}{
		{"negative long", nil, &negative, nil},
		{"negative short", nil, nil, &negative},
		{"inconsistent net", &net, &long, &short},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := BuildInstrument(InstrumentSet{FirmID: "2040", ClientCode: "kalys", Symbol: "SBER", Quantity: tc.qty, LongQty: tc.long, ShortQty: tc.short})
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("expected invalid balance, got %v", err)
			}
		})
	}
	qty := dec("-5")
	p, err := BuildInstrument(InstrumentSet{FirmID: "2040", ClientCode: "kalys", Symbol: "SBER", Quantity: &qty})
	if err != nil || !p.NetQty.Equal(qty) || !p.ShortQty.Equal(short) || !p.LongQty.IsZero() {
		t.Fatalf("valid short balance rejected: %#v, %v", p, err)
	}
	net = dec("7")
	p, err = BuildInstrument(InstrumentSet{FirmID: "2040", ClientCode: "kalys", Symbol: "SBER", Quantity: &net, LongQty: &long, ShortQty: &short})
	if err != nil || !p.NetQty.Equal(net) {
		t.Fatalf("consistent balance rejected: %#v, %v", p, err)
	}
}

func TestMergePositionsOverrideWins(t *testing.T) {
	fix := []domain.FIXPosition{
		{Kind: "CASH", FirmID: "2040", ClientCode: "kalys", Currency: "KGS", Symbol: "EQTV", NetQty: dec("1"), Source: SourceFIX},
		{Kind: "INSTRUMENT", FirmID: "2040", ClientCode: "kalys", Symbol: "KG0105166315", Board: "LISTEDSTOCKK", NetQty: dec("10"), Source: SourceFIX},
	}
	over := []domain.FIXPosition{
		{Kind: "CASH", FirmID: "2040", ClientCode: "kalys", Currency: "KGS", Symbol: "EQTV", NetQty: dec("99"), Source: SourceManual},
		{Kind: "INSTRUMENT", FirmID: "2040", ClientCode: "new", Symbol: "AAPL", Board: "TQBR", NetQty: dec("3"), Source: SourceManual},
	}
	got := MergePositions(fix, over)
	if len(got) != 3 {
		t.Fatalf("len %d %#v", len(got), got)
	}
	if got[0].Source != SourceManual || got[0].NetQty.String() != "99" {
		t.Fatalf("cash overlay %#v", got[0])
	}
	if got[1].Source != SourceFIX {
		t.Fatalf("instrument kept %#v", got[1])
	}
	if got[2].Symbol != "AAPL" {
		t.Fatalf("appended %#v", got[2])
	}
}

func TestMergeCashMatchesByTagOrCurrency(t *testing.T) {
	fix := []domain.FIXPosition{
		{Kind: "CASH", FirmID: "2040", ClientCode: "kalys", Currency: "KGS", Symbol: "EQTV", Source: SourceFIX},
	}
	over := []domain.FIXPosition{
		{Kind: "CASH", FirmID: "2040", ClientCode: "kalys", Currency: "KGS", Symbol: "EQTV", NetQty: dec("5"), Source: SourceManual},
	}
	got := MergePositions(fix, over)
	if len(got) != 1 || got[0].Source != SourceManual {
		t.Fatalf("got %#v", got)
	}
}
