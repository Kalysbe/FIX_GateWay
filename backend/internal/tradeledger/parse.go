package tradeledger

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

type Input struct {
	Match, Number, Date, Symbol, Board, Side         string
	OrderID, OrderNumber, ClOrdID, ExecID            string
	Firm, FirmName, Client, Account, Contra, Adapter string
	Qty, Price                                       decimal.Decimal
	Cum, Leaves                                      *decimal.Decimal
	Time                                             *time.Time
}

var digits = regexp.MustCompile(`^[0-9]+$`)
var matchFormat = regexp.MustCompile(`^(\d{6})-(.+)-(\d+)$`)

func FIXFields(raw string) (map[string]string, error) {
	fields := make(map[string]string)
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool { return r == '|' || r == '\x01' }) {
		k, v, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		// Repeating FIX party groups are preserved in raw evidence, not flattened.
		switch k {
		case "17", "37", "54", "75", "880", "5001", "5002", "32", "31", "100", "48":
			if old, exists := fields[k]; exists && old != v {
				return nil, fmt.Errorf("ambiguous FIX tag %s", k)
			}
		}
		fields[k] = v
	}
	return fields, nil
}

func ParseFIX(raw, adapter string) (Input, bool, error) {
	f, err := FIXFields(raw)
	if err != nil {
		return Input{}, false, err
	}
	if f["35"] != "8" {
		return Input{}, false, nil
	}
	qty, err := decimal.NewFromString(defaultString(f["32"], "0"))
	if err != nil || qty.IsNegative() {
		return Input{}, false, fmt.Errorf("invalid LastQty")
	}
	if f["20"] == "3" || f["150"] == "I" {
		return Input{}, false, nil
	} // status is not a new fill
	if f["150"] == "G" || f["150"] == "H" || (f["20"] != "" && f["20"] != "0") {
		return Input{}, false, fmt.Errorf("FIX correction/cancellation requires reconciliation")
	}
	if !qty.IsPositive() {
		return Input{}, false, nil
	}
	p := Input{Match: f["880"], Number: f["5001"], Symbol: defaultString(f["48"], f["55"]), Board: defaultString(f["100"], f["207"]),
		OrderID: f["37"], OrderNumber: f["5002"], ClOrdID: f["11"], ExecID: f["17"], Firm: f["5018"], Client: f["109"],
		Account: f["1"], Contra: f["375"], Adapter: adapter, Qty: qty}
	if f["54"] == "1" {
		p.Side = "BUY"
	} else if f["54"] == "2" {
		p.Side = "SELL"
	}
	if f["75"] != "" {
		d, e := time.Parse("20060102", f["75"])
		if e != nil {
			return p, true, e
		}
		p.Date = d.Format(time.DateOnly)
	}
	if p.Match != "" {
		m := matchFormat.FindStringSubmatch(p.Match)
		if m == nil {
			return p, true, fmt.Errorf("unsupported ARQA TrdMatchID format")
		}
		d, e := time.Parse("060102", m[1])
		if e != nil {
			return p, true, e
		}
		if p.Date != "" && p.Date != d.Format(time.DateOnly) {
			return p, true, fmt.Errorf("TradeDate differs from TrdMatchID")
		}
		p.Date = d.Format(time.DateOnly)
		if p.Number != "" && p.Number != m[3] {
			return p, true, fmt.Errorf("TradeNum differs from TrdMatchID")
		}
		p.Number = m[3]
		if p.Board != m[2] {
			return p, true, fmt.Errorf("class differs from TrdMatchID")
		}
	}
	p.Price, err = decimal.NewFromString(f["31"])
	if err != nil {
		return p, true, err
	}
	for tag, dest := range map[string]**decimal.Decimal{"14": &p.Cum, "151": &p.Leaves} {
		if f[tag] != "" {
			v, e := decimal.NewFromString(f[tag])
			if e != nil || v.IsNegative() {
				return p, true, fmt.Errorf("invalid FIX %s", tag)
			}
			*dest = &v
		}
	}
	if f["60"] != "" {
		t, e := time.Parse("20060102-15:04:05.999999999", f["60"])
		if e != nil {
			return p, true, e
		}
		p.Time = &t
	}
	if p.OrderNumber == "" {
		if i := strings.LastIndex(p.OrderID, "-"); i >= 0 {
			p.OrderNumber = p.OrderID[i+1:]
		}
	}
	return p, true, p.validate()
}

func ParseQUIK(raw json.RawMessage) (Input, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return Input{}, err
	}
	get := func(key string) string {
		v := m[key]
		var s string
		if json.Unmarshal(v, &s) == nil {
			return s
		}
		if string(v) == "null" {
			return ""
		}
		return string(v)
	}
	p := Input{Number: get("TradeNum"), Symbol: get("SecCode"), Board: get("ClassCode"), OrderNumber: get("OrderNum"),
		Firm: get("FirmId"), FirmName: get("FirmName"), Client: get("ClientCode"), Account: get("Account"), Contra: get("CPFirmId")}
	switch strings.ToLower(get("Operation")) {
	case "купля", "покупка", "buy":
		p.Side = "BUY"
	case "продажа", "sell":
		p.Side = "SELL"
	}
	d, err := time.Parse("02-01-2006", get("TradeDate"))
	if err != nil {
		return p, err
	}
	p.Date = d.Format(time.DateOnly)
	p.Match = d.Format("060102") + "-" + p.Board + "-" + p.Number
	p.Qty, err = decimal.NewFromString(get("Qty"))
	if err != nil {
		return p, err
	}
	p.Price, err = decimal.NewFromString(get("Price"))
	if err != nil {
		return p, err
	}
	if get("CanceledDateTime") != "" || get("CanceledUserId") != "" {
		return p, fmt.Errorf("cancelled QUIK trade requires reconciliation")
	}
	return p, p.validate()
}

func (p *Input) validate() error {
	d, err := time.Parse(time.DateOnly, p.Date)
	if err != nil {
		return fmt.Errorf("missing or invalid trade date")
	}
	if !digits.MatchString(p.Number) || !digits.MatchString(p.OrderNumber) || p.Symbol == "" || p.Board == "" || (p.Side != "BUY" && p.Side != "SELL") || !p.Qty.IsPositive() || p.Price.IsNegative() {
		return fmt.Errorf("incomplete exchange trade identity, side or amounts")
	}
	if p.Match == "" {
		p.Match = d.Format("060102") + "-" + p.Board + "-" + p.Number
	}
	return nil
}

func defaultString(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
