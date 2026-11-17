package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/fix-gateway/backend/internal/domain"
	"github.com/fix-gateway/backend/internal/orders"
	"github.com/fix-gateway/backend/internal/quikexport"
	"github.com/google/uuid"
)

type quikOrderReader interface {
	ListOrders(context.Context, quikexport.OrderFilter) (quikexport.OrderPage, error)
	GetOrder(context.Context, string, *string) (quikexport.Order, error)
	CountOrdersByState(context.Context) (map[string]int, error)
	ListTrades(context.Context, quikexport.TradeFilter) (quikexport.TradePage, error)
	GetOrderBook(ctx context.Context, classCode, secCode, activeState string) (quikexport.OrderBook, error)
}

// quikOrderStats keeps the dashboard counters that existed before the QUIK
// source; they group raw State values and are not part of any Orders row.
func quikOrderStats(byState map[string]int) map[string]int {
	stats := map[string]int{"total": 0, "active": 0, "filled": 0, "rejected": 0}
	for state, n := range byState {
		stats["total"] += n
		switch strings.ToUpper(strings.TrimSpace(state)) {
		case "АКТИВНА", "ACTIVE":
			stats["active"] += n
		case "ИСПОЛНЕНА", "FILLED":
			stats["filled"] += n
		case "ОТКЛОНЕНА", "REJECTED":
			stats["rejected"] += n
		}
	}
	return stats
}

var errOrderFirmScope = errors.New("order firm scope unavailable")

func (s *Server) orderReadScope(r *http.Request, admin bool) (*string, *int64, error) {
	if !admin {
		b, ok := BrokerFromContext(r.Context())
		if !ok {
			return nil, nil, errOrderFirmScope
		}
		tok, _ := TokenFromContext(r.Context())
		firm := quikexport.BrokerOrderFirm(b.Code, tok.QuikFirm)
		if firm == "" {
			return nil, nil, errOrderFirmScope
		}
		return &firm, &b.ID, nil
	}
	firm := strings.TrimSpace(r.URL.Query().Get("firm_id"))
	if raw := r.URL.Query().Get("broker_id"); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id <= 0 {
			return nil, nil, errOrderFirmScope
		}
		b, err := s.brokers.GetBroker(r.Context(), id)
		if err != nil || strings.TrimSpace(b.Code) == "" {
			return nil, nil, errOrderFirmScope
		}
		if firm != "" && firm != strings.TrimSpace(b.Code) {
			return nil, nil, errOrderFirmScope
		}
		firm = strings.TrimSpace(b.Code)
		return &firm, &id, nil
	}
	if firm == "" {
		return nil, nil, nil
	}
	return &firm, nil, nil
}

func (s *Server) writeOrderReadError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errOrderFirmScope):
		writeErr(w, http.StatusForbidden, errOrderFirmScope.Error())
	case errors.Is(err, quikexport.ErrOrderNotFound), errors.Is(err, orders.ErrNotFound):
		writeErr(w, http.StatusNotFound, quikexport.ErrOrderNotFound.Error())
	case errors.Is(err, quikexport.ErrOrderKey):
		writeErr(w, http.StatusBadRequest, quikexport.ErrOrderKey.Error())
	default:
		if s.log != nil {
			s.log.Error("QUIK Orders read failed", "error", err)
		}
		writeErr(w, http.StatusServiceUnavailable, "QUIK Orders source unavailable")
	}
}

// quikActiveState is the raw QUIK "State" value of an order still in the book.
const quikActiveState = "Активна"

// listAdminQuotes serves the quotes of the admin "Котировки" page: today's
// QUIK Orders rows still in the book (State = "Активна") of every firm, as
// stored. board and symbol narrow them to one instrument.
func (s *Server) listAdminQuotes(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	s.listQuikOrdersMatching(w, r, true, quotesFilter(q.Get("board"), q.Get("symbol")))
}

// quotesFilter selects quotes: today's orders in state "Активна". Older rows
// QUIK still exports as "Активна" are not quotes.
func quotesFilter(board, symbol string) quikexport.OrderFilter {
	return quikexport.OrderFilter{State: quikActiveState, ClassCode: board, SecCode: symbol, Today: true}
}

func (s *Server) listQuikOrdersMatching(w http.ResponseWriter, r *http.Request, admin bool, filter quikexport.OrderFilter) {
	firm, _, err := s.orderReadScope(r, admin)
	if err != nil {
		s.writeOrderReadError(w, err)
		return
	}
	filter.Firm = firm
	s.writeQuikOrderPage(w, r, filter, func(rows []quikexport.Order) any { return rows })
}

// maxPageOffset bounds how deep a list can be paged.
const maxPageOffset = 10000000

// pageParams reads limit (1–1000, default 200) and the page: page (from 1),
// as brokers page (поручение пользователя 17.11.2026), or offset, as the
// console pages. A bad value is answered with 400 and ok=false.
func pageParams(w http.ResponseWriter, r *http.Request) (limit, offset int, ok bool) {
	return pageParamsWithin(w, r, 200, 1000)
}

func pageParamsWithin(w http.ResponseWriter, r *http.Request, defaultLimit, maxLimit int) (limit, offset int, ok bool) {
	q := r.URL.Query()
	limit = defaultLimit
	page := 0
	for _, p := range []struct {
		key      string
		dest     *int
		min, max int
	}{{"limit", &limit, 1, maxLimit}, {"page", &page, 1, maxPageOffset}, {"offset", &offset, 0, maxPageOffset}} {
		if raw := q.Get(p.key); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n < p.min || n > p.max {
				writeErr(w, http.StatusBadRequest, "invalid "+p.key)
				return 0, 0, false
			}
			*p.dest = n
		}
	}
	if page > 0 {
		if q.Get("offset") != "" {
			writeErr(w, http.StatusBadRequest, "page and offset cannot be combined")
			return 0, 0, false
		}
		if page-1 > maxPageOffset/limit {
			writeErr(w, http.StatusBadRequest, "invalid page")
			return 0, 0, false
		}
		offset = (page - 1) * limit
	}
	return limit, offset, true
}

// setPageHeaders reports a page: X-Has-More and X-Next-Offset for the console,
// X-Page, X-Limit and X-Total-Count for the broker envelope, which turns them
// into page, limit and total.
func setPageHeaders(w http.ResponseWriter, limit, offset, count, total int) {
	h := w.Header()
	h.Set("X-Has-More", strconv.FormatBool(offset+count < total))
	h.Set("X-Next-Offset", strconv.Itoa(offset+count))
	h.Set("X-Page", strconv.Itoa(offset/limit+1))
	h.Set("X-Limit", strconv.Itoa(limit))
	h.Set("X-Total-Count", strconv.Itoa(total))
}

// writeQuikOrderPage serves one page of the Orders rows matching filter,
// whose Firm is already decided by the caller, as view renders them.
func (s *Server) writeQuikOrderPage(w http.ResponseWriter, r *http.Request, filter quikexport.OrderFilter, view func([]quikexport.Order) any) {
	limit, offset, ok := pageParams(w, r)
	if !ok {
		return
	}
	if s.quikOrders == nil {
		s.writeOrderReadError(w, quikexport.ErrNotConfigured)
		return
	}
	filter.Limit, filter.Offset = limit, offset
	page, err := s.quikOrders.ListOrders(r.Context(), filter)
	if err != nil {
		s.writeOrderReadError(w, err)
		return
	}
	setPageHeaders(w, limit, offset, len(page.Orders), page.Total)
	writeQuikJSON(w, view(page.Orders))
}

// UUIDs remain valid references for Gateway clients, but their current state
// must still be present in Orders. Missing rows are never restored from FIX.
func (s *Server) currentQuikOrder(ctx context.Context, key string, firm *string, brokerID *int64, resolveLocal bool) (quikexport.Order, *domain.Order, error) {
	if s.quikOrders == nil {
		return quikexport.Order{}, nil, quikexport.ErrNotConfigured
	}
	var local *domain.Order
	if id, err := uuid.Parse(key); err == nil {
		if s.orderStore == nil {
			return quikexport.Order{}, nil, orders.ErrNotFound
		}
		var o domain.Order
		if brokerID == nil {
			o, err = s.orderStore.Get(ctx, id)
		} else {
			o, err = s.orderStore.GetForBroker(ctx, *brokerID, id)
		}
		if err != nil {
			return quikexport.Order{}, nil, err
		}
		if firm != nil && o.QuikFirm != *firm {
			return quikexport.Order{}, nil, quikexport.ErrOrderNotFound
		}
		if o.QuikOrderID == nil || *o.QuikOrderID == "" {
			return quikexport.Order{}, nil, quikexport.ErrOrderNotFound
		}
		key = *o.QuikOrderID
		local = &o
	}
	o, err := s.quikOrders.GetOrder(ctx, key, firm)
	if err != nil {
		return quikexport.Order{}, nil, err
	}
	if resolveLocal && local == nil && s.orderStore != nil {
		linked, err := s.orderStore.FindByQuikOrderID(ctx, o.VenueID(), strings.TrimSpace(o.Text("FirmId")), brokerID)
		if err != nil && !errors.Is(err, orders.ErrNotFound) {
			return quikexport.Order{}, nil, err
		}
		if err == nil {
			local = &linked
		}
	}
	if local != nil {
		if local.QuikFirm != strings.TrimSpace(o.Text("FirmId")) || local.Symbol != o.Text("SecCode") || local.Board != o.Text("ClassCode") || string(local.Side) != o.Side() {
			return quikexport.Order{}, nil, quikexport.ErrOrderNotFound
		}
	}
	return o, local, nil
}

func (s *Server) getQuikOrder(w http.ResponseWriter, r *http.Request, admin bool) {
	firm, brokerID, err := s.orderReadScope(r, admin)
	if err != nil {
		s.writeOrderReadError(w, err)
		return
	}
	o, _, err := s.currentQuikOrder(r.Context(), r.PathValue("id"), firm, brokerID, false)
	if err != nil {
		s.writeOrderReadError(w, err)
		return
	}
	writeQuikJSON(w, o)
}

func writeQuikJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(value)
}
