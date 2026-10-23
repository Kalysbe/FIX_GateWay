package domain

import (
	"fmt"
	"unicode/utf8"
)

// MaxFIXTextLen bounds free-form values copied into outgoing FIX tags.
const MaxFIXTextLen = 64

// CheckFIXText rejects values that could break FIX framing: SOH (\x01) or any
// other control character would let a caller append extra tags (5018, 1, ...)
// to the outgoing message.
func CheckFIXText(name, v string, maxLen int) error {
	if !utf8.ValidString(v) {
		return fmt.Errorf("%s: недопустимая кодировка", name)
	}
	for _, r := range v {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("%s: управляющие символы недопустимы", name)
		}
	}
	if maxLen > 0 && utf8.RuneCountInString(v) > maxLen {
		return fmt.Errorf("%s: не длиннее %d символов", name, maxLen)
	}
	return nil
}

// CheckFIXTexts checks name/value pairs with the default length limit.
func CheckFIXTexts(pairs ...string) error {
	for i := 0; i+1 < len(pairs); i += 2 {
		if err := CheckFIXText(pairs[i], pairs[i+1], MaxFIXTextLen); err != nil {
			return err
		}
	}
	return nil
}

// CheckMarketDataQueryText applies CheckFIXText to every string that 35=V carries.
func CheckMarketDataQueryText(q MarketDataQuery) error {
	if err := CheckFIXTexts("symbol", q.Symbol, "board", q.Board, "security_type", q.SecurityType,
		"subscription_type", q.SubscriptionType); err != nil {
		return err
	}
	for _, b := range q.Boards {
		if err := CheckFIXText("board", b, MaxFIXTextLen); err != nil {
			return err
		}
	}
	for _, in := range q.Instruments {
		if err := CheckFIXTexts("symbol", in.Symbol, "security_id", in.SecurityID, "board", in.Board,
			"currency", in.Currency, "security_type", in.SecurityType); err != nil {
			return err
		}
	}
	return nil
}

// CheckPositionMaintainText applies CheckFIXText to every string that 35=AL carries.
func CheckPositionMaintainText(req PositionMaintainRequest) error {
	for _, f := range []struct{ name, value string }{
		{"firm_id", req.Firm}, {"client_code", req.ClientCode}, {"account", req.Account},
		{"clearing_business_date", req.ClearingBusinessDate}, {"action", req.Action}, {"trans_type", req.TransType},
	} {
		if err := CheckFIXText(f.name, f.value, MaxFIXTextLen); err != nil {
			return err
		}
	}
	for _, b := range req.Blocks {
		if err := CheckFIXText("pos_type", b.PosType, MaxFIXTextLen); err != nil {
			return err
		}
		if err := CheckFIXText("update_action", b.UpdateAction, MaxFIXTextLen); err != nil {
			return err
		}
		for _, m := range []map[string]string{b.Keys, b.Items} {
			for k, v := range m {
				if err := CheckFIXText("position key", k, MaxFIXTextLen); err != nil {
					return err
				}
				if err := CheckFIXText(k, v, MaxFIXTextLen); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
