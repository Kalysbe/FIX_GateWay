package quikexport

import (
	"context"
	"strings"
	"time"
)

// KSETime is the exchange's time zone. QUIK stores Trades.TradeDateTime and
// CanceledDateTime as local KSE time without a zone (checked 05.11.2026: an
// order sent at 08:32:57 UTC has OrderDateTime 14:32:57).
var KSETime = time.FixedZone("Asia/Bishkek", 6*60*60)

// MarketTradeFilter selects market trades for price charts. Empty ClassCode or
// SecCode matches any; From (inclusive) and Before (exclusive) bound
// TradeDateTime, nil is unbounded.
type MarketTradeFilter struct {
	ClassCode string
	SecCode   string
	From      *time.Time
	Before    *time.Time
	Limit     int
	Offset    int
}

// kseTimestamp renders a QUIK local timestamp column with its +06:00 offset;
// TradeTimeMs-style milliseconds are added when they are a sub-second part.
func kseTimestamp(column, ms string) string {
	value := column
	if ms != "" {
		value = `(` + column + ` + CASE WHEN ` + ms + ` BETWEEN 0 AND 999 THEN ` + ms + ` * interval '1 millisecond' ELSE interval '0' END)`
	}
	return `to_char(` + value + `, 'YYYY-MM-DD"T"HH24:MI:SS.MS"+06:00"')`
}

// marketTradeRows are the executed trades of the market (поручение
// пользователя 05.11.2026: график цен), every firm, without participants:
// Trades holds a row per side, both sides share ClassCode, TradeDate and
// TradeNum, so each trade is served once. Price is the unit price as stored,
// with no commission; SettleCurrency is the settlement currency; Kind tells
// ordinary trades from REPO; CanceledDateTime is null unless QUIK cancelled it.
var marketTradeRows = `SELECT DISTINCT ON (btrim(t."ClassCode"::text), t."TradeDate", t."TradeNum")
t."TradeNum", btrim(t."ClassCode"::text) AS "ClassCode", btrim(t."SecCode"::text) AS "SecCode",
` + kseTimestamp(`t."TradeDateTime"`, `t."TradeTimeMs"`) + ` AS "TradeDateTime",
t."Price", t."Qty", t."SettleCurrency", t."Kind",
` + kseTimestamp(`t."CanceledDateTime"`, `t."CanceledDateTimeMs"`) + ` AS "CanceledDateTime",
t."TradeDateTime" AS sort_time
FROM public."Trades" t
WHERE ($1::text[] IS NULL OR btrim(t."ClassCode"::text)=ANY($1::text[]))
AND ($2::text='' OR btrim(t."ClassCode"::text)=$2) AND ($3::text='' OR btrim(t."SecCode"::text)=$3)
AND ($4::timestamp IS NULL OR t."TradeDateTime">=$4::timestamp)
AND ($5::timestamp IS NULL OR t."TradeDateTime"<$5::timestamp)
ORDER BY btrim(t."ClassCode"::text), t."TradeDate", t."TradeNum", t."CanceledDateTime" DESC NULLS LAST, t."Operation"`

// kseLocal is a bound as QUIK's zone-less local KSE timestamp.
func kseLocal(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.In(KSETime).Format("2006-01-02 15:04:05.999999")
	return &s
}

// ListMarketTrades pages market trades in chronological order: TradeDateTime,
// then TradeNum.
func (s *Store) ListMarketTrades(ctx context.Context, f MarketTradeFilter) (TradePage, error) {
	out := TradePage{Trades: []Order{}}
	if s == nil || s.pool == nil {
		return out, ErrNotConfigured
	}
	classes, err := s.servedClasses(ctx)
	if err != nil {
		return out, err
	}
	rows, err := s.pool.Query(ctx, `SELECT row_to_json(r) FROM (
SELECT x."TradeNum", x."ClassCode", x."SecCode", x."TradeDateTime", x."Price", x."Qty", x."SettleCurrency", x."Kind", x."CanceledDateTime"
FROM (`+marketTradeRows+`) x
ORDER BY x.sort_time, x."TradeNum", x."ClassCode"
LIMIT $6 OFFSET $7) r`, classes, strings.TrimSpace(f.ClassCode), strings.TrimSpace(f.SecCode), kseLocal(f.From), kseLocal(f.Before), f.Limit+1, f.Offset)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return out, err
		}
		out.Trades = append(out.Trades, Order{Raw: raw})
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	if len(out.Trades) > f.Limit {
		out.HasMore = true
		out.Trades = out.Trades[:f.Limit]
	}
	out.NextOffset = f.Offset + len(out.Trades)
	return out, nil
}
