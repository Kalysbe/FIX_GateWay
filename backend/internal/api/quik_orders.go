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

func (s *Server) listQuikOrders(w http.ResponseWriter, r *http.Request, admin bool) {
	s.listQuikOrdersInState(w, r, admin, "")
}

func (s *Server) listQuikOrdersInState(w http.ResponseWriter, r *http.Request, admin bool, state string) {
	s.listQuikOrdersMatching(w, r, admin, quikexport.OrderFilter{State: state})
}

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
	limit, offset := 200, 0
	for _, p := range []struct {
		key      string
		dest     *int
		min, max int
	}{{"limit", &limit, 1, 1000}, {"offset", &offset, 0, 10000000}} {
		if raw := r.URL.Query().Get(p.key); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n < p.min || n > p.max {
				writeErr(w, http.StatusBadRequest, "invalid "+p.key)
				return
			}
			*p.dest = n
		}
	}
	if s.quikOrders == nil {
		s.writeOrderReadError(w, quikexport.ErrNotConfigured)
		return
	}
	filter.Firm, filter.Limit, filter.Offset = firm, limit, offset
	page, err := s.quikOrders.ListOrders(r.Context(), filter)
	if err != nil {
		s.writeOrderReadError(w, err)
		return
	}
	w.Header().Set("X-Has-More", strconv.FormatBool(page.HasMore))
	w.Header().Set("X-Next-Offset", strconv.Itoa(page.NextOffset))
	writeQuikJSON(w, page.Orders)
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
