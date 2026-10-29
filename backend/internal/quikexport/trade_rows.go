package quikexport

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"time"
)

// TradeFilter selects rows of QUIK "Trades" as brokers see them.
type TradeFilter struct {
	// nil is admin/all firms. A non-nil empty string matches no firm.
	Firm *string
	// Today keeps only rows whose TradeDate is today's KSE trading day (UTC+6).
	Today  bool
	Limit  int
	Offset int
}

// TradePage is one page of raw "Trades" rows (row_to_json, unchanged); each
// row is one side of a trade, as QUIK stores it.
type TradePage struct {
	Trades     []Order
	HasMore    bool
	NextOffset int
}

// ListTrades applies the firm predicate in SQL, before pagination. Limit=0 is
// reserved for the internal change reader; HTTP callers are paged.
func (s *Store) ListTrades(ctx context.Context, f TradeFilter) (TradePage, error) {
	out := TradePage{Trades: []Order{}}
	if s == nil || s.pool == nil {
		return out, ErrNotConfigured
	}
	limit := f.Limit
	if limit > 0 {
		limit++
	}
	rows, err := s.pool.Query(ctx, `SELECT row_to_json(t) FROM public."Trades" t
WHERE ($1::text IS NULL OR (btrim(t."FirmId")=$1 AND $1<>''))
AND (NOT $4::boolean OR t."TradeDate"::date=(now() AT TIME ZONE 'Asia/Bishkek')::date)
ORDER BY t."TradeDate" DESC, t."TradeNum" DESC, t."ClassCode", t."Operation"
LIMIT NULLIF($2,0) OFFSET $3`, f.Firm, limit, f.Offset, f.Today)
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
	if f.Limit > 0 && len(out.Trades) > f.Limit {
		out.HasMore = true
		out.Trades = out.Trades[:f.Limit]
	}
	out.NextOffset = f.Offset + len(out.Trades)
	return out, nil
}

// tradeKey names one side of a trade: both sides share TradeNum.
func tradeKey(t Order) string {
	var fields map[string]json.RawMessage
	if json.Unmarshal(t.Raw, &fields) != nil {
		return ""
	}
	parts := make([]string, 0, 6)
	for _, k := range []string{"TradeDate", "ClassCode", "TradeNum", "Operation", "FirmId", "Account"} {
		parts = append(parts, strings.TrimSpace(fieldText(fields[k])))
	}
	return strings.Join(parts, "\x00")
}

// RunTrades publishes today's new or changed "Trades" rows. Only complete
// selections are compared, and the first one is the baseline: clients load
// existing rows over REST.
func (s *Store) RunTrades(ctx context.Context, publish func(Order), failure func(error)) {
	runTradeFeed(ctx, s.ListTrades, 2*time.Second, publish, failure)
}

func runTradeFeed(ctx context.Context, read func(context.Context, TradeFilter) (TradePage, error), interval time.Duration, publish func(Order), failure func(error)) {
	previous := map[string]json.RawMessage{}
	primed := false
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		readCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		page, err := read(readCtx, TradeFilter{Today: true})
		cancel()
		if err != nil {
			if ctx.Err() == nil && failure != nil {
				failure(err)
			}
		} else {
			current := make(map[string]json.RawMessage, len(page.Trades))
			for _, t := range page.Trades {
				key := tradeKey(t)
				current[key] = t.Raw
				if old, ok := previous[key]; primed && (!ok || !bytes.Equal(old, t.Raw)) {
					publish(t)
				}
			}
			previous, primed = current, true
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
