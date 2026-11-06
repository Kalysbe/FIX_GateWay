package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/fix-gateway/backend/internal/quikexport"
)

// Market trades for price charts (поручение пользователя 05.11.2026): every
// executed KSE trade once, chronologically, without participants.
type quikMarketTradeReader interface {
	ListMarketTrades(context.Context, quikexport.MarketTradeFilter) (quikexport.TradePage, error)
}

// tradeBound reads a period bound: a KSE date (YYYY-MM-DD, the whole day) or
// an RFC 3339 time. end=true turns it into the exclusive end of the period.
func tradeBound(raw string, end bool) (*time.Time, bool) {
	if raw == "" {
		return nil, true
	}
	if day, err := time.ParseInLocation("2006-01-02", raw, quikexport.KSETime); err == nil {
		if end {
			day = day.AddDate(0, 0, 1)
		}
		return &day, true
	}
	t, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return nil, false
	}
	if end {
		// QUIK keeps whole seconds (or milliseconds); "to" is inclusive.
		t = t.Add(time.Microsecond)
	}
	return &t, true
}

// listMarketTrades serves GET /api/v1/market-data/trades: board and symbol
// narrow the instrument, from and to the period of TradeDateTime.
func (s *Server) listMarketTrades(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := quikexport.MarketTradeFilter{ClassCode: strings.TrimSpace(q.Get("board")), SecCode: strings.TrimSpace(q.Get("symbol"))}
	var ok bool
	if f.From, ok = tradeBound(strings.TrimSpace(q.Get("from")), false); !ok {
		writeErr(w, http.StatusBadRequest, "invalid from: expected YYYY-MM-DD or RFC 3339 time")
		return
	}
	if f.Before, ok = tradeBound(strings.TrimSpace(q.Get("to")), true); !ok {
		writeErr(w, http.StatusBadRequest, "invalid to: expected YYYY-MM-DD or RFC 3339 time")
		return
	}
	if f.From != nil && f.Before != nil && !f.From.Before(*f.Before) {
		writeErr(w, http.StatusBadRequest, "from is after to")
		return
	}
	if f.Limit, f.Offset, ok = pageParams(w, r); !ok {
		return
	}
	if s.quikMarketTrades == nil {
		s.writeTradeReadError(w, quikexport.ErrNotConfigured)
		return
	}
	page, err := s.quikMarketTrades.ListMarketTrades(r.Context(), f)
	if err != nil {
		s.writeTradeReadError(w, err)
		return
	}
	w.Header().Set("X-Has-More", strconv.FormatBool(page.HasMore))
	w.Header().Set("X-Next-Offset", strconv.Itoa(page.NextOffset))
	writeQuikJSON(w, page.Trades)
}
