package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/fix-gateway/backend/internal/quikexport"
)

// Market data is the QUIK current trades table quik_export2.public."Params",
// served as database rows: original column names, numbers, strings and nulls.
type quikParamReader interface {
	ListParams(context.Context, quikexport.ParamFilter) (quikexport.ParamPage, error)
	GetParam(context.Context, string, string) (quikexport.Param, error)
}

func (s *Server) writeParamReadError(w http.ResponseWriter, err error) {
	if errors.Is(err, quikexport.ErrParamNotFound) {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	if s.log != nil && !errors.Is(err, quikexport.ErrNotConfigured) {
		s.log.Error("QUIK Params read failed", "error", err)
	}
	writeErr(w, http.StatusServiceUnavailable, "market data unavailable")
}

// board and symbol filter the raw ClassCode and SecCode columns.
func (s *Server) listMarketData(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, offset := 1000, 0
	for _, p := range []struct {
		key      string
		dest     *int
		min, max int
	}{{"limit", &limit, 1, 5000}, {"offset", &offset, 0, 10000000}} {
		if raw := q.Get(p.key); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n < p.min || n > p.max {
				writeErr(w, http.StatusBadRequest, "invalid "+p.key)
				return
			}
			*p.dest = n
		}
	}
	if s.quikParams == nil {
		s.writeParamReadError(w, quikexport.ErrNotConfigured)
		return
	}
	page, err := s.quikParams.ListParams(r.Context(), quikexport.ParamFilter{
		ClassCode: strings.TrimSpace(q.Get("board")), SecCode: strings.TrimSpace(q.Get("symbol")), Limit: limit, Offset: offset,
	})
	if err != nil {
		s.writeParamReadError(w, err)
		return
	}
	w.Header().Set("X-Has-More", strconv.FormatBool(page.HasMore))
	w.Header().Set("X-Next-Offset", strconv.Itoa(page.NextOffset))
	writeQuikJSON(w, page.Params)
}

func (s *Server) getMarketData(w http.ResponseWriter, r *http.Request) {
	board, symbol := strings.TrimSpace(r.PathValue("board")), strings.TrimSpace(r.PathValue("symbol"))
	if board == "" || symbol == "" {
		writeErr(w, http.StatusBadRequest, "board and symbol are required")
		return
	}
	if s.quikParams == nil {
		s.writeParamReadError(w, quikexport.ErrNotConfigured)
		return
	}
	p, err := s.quikParams.GetParam(r.Context(), board, symbol)
	if err != nil {
		s.writeParamReadError(w, err)
		return
	}
	writeQuikJSON(w, p)
}
