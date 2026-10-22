package tradeledger

import (
	"encoding/json"
	"strings"
	"testing"
)

const sampleFIX = "8=FIX.4.2|35=8|37=261006-LIST_GBA-19358|11=local-sell|17=LIST_GBA-2sw-S-1-1-N|880=261006-LIST_GBA-11094|20=0|150=1|39=1|1=1-3101-2040|48=GBA02280810|100=LIST_GBA|54=2|32=201|31=99.00|14=201|151=671|60=20261006-12:20:32.798|5018=2040|109=imar|375=2040|75=20261006|5001=11094|5002=19358|"
const sampleQUIK = `{"TradeNum":"11094","Operation":"Купля","TradeDate":"06-10-2026","ClassCode":"LIST_GBA","SecCode":"GBA02280810","OrderNum":"22450","Price":"99.00000000","Qty":"201.00000000","ClientCode":"Kalys","FirmId":"2040","Account":"1-3101-2040"}`

func TestExchangeIdentityUsesTradeNumAndPreservesRealSides(t *testing.T) {
	p, fill, err := ParseFIX(sampleFIX, "adapter-1")
	if err != nil || !fill {
		t.Fatal(err)
	}
	if p.Number != "11094" || p.Side != "SELL" || p.Date != "2026-10-06" || p.OrderNumber != "19358" || p.Client != "imar" {
		t.Fatal(p)
	}
	q, err := ParseQUIK(json.RawMessage(sampleQUIK))
	if err != nil {
		t.Fatal(err)
	}
	if q.Match != p.Match || q.Side != "BUY" || q.Client != "Kalys" || q.OrderNumber != "22450" || q.ExecID != "" || q.OrderID != "" {
		t.Fatal(q)
	}
	if _, _, err := ParseFIX(strings.Replace(sampleFIX, "5001=11094", "5001=999", 1), "a"); err == nil {
		t.Fatal("inconsistent trade number accepted")
	}
	if _, fill, err := ParseFIX(strings.Replace(sampleFIX, "20=0", "20=3", 1), "a"); fill || err != nil {
		t.Fatal("status snapshot became a fill", fill, err)
	}
	if _, _, err := ParseFIX(strings.Replace(sampleFIX, "54=2", "54=1|54=2", 1), "a"); err == nil {
		t.Fatal("ambiguous side accepted")
	}
	huge := strings.Replace(sampleQUIK, `"11094"`, `"900719925474099312345"`, 1)
	q, err = ParseQUIK(json.RawMessage(huge))
	if err != nil || q.Number != "900719925474099312345" {
		t.Fatal("trade number lost precision", q.Number, err)
	}
}
