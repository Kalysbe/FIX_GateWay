package api

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/fix-gateway/backend/internal/quikexport"
)

// Trading accounts and firms come from QUIK quik_export2.public."Trdaccs" and
// public."Firms", served as database rows. A broker sees the accounts of the
// firm of its token (the broker code when the token has none).
type quikRefReader interface {
	ListTradingAccounts(context.Context, string) ([]quikexport.Row, error)
	ListFirms(context.Context) ([]quikexport.Row, error)
	Firm(context.Context, string) (quikexport.Row, error)
}

var errQuikRefsUnavailable = errors.New("QUIK accounts and firms source unavailable")

func (s *Server) writeQuikRefError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, quikexport.ErrFirmNotFound):
		writeErr(w, http.StatusNotFound, err.Error())
	case errors.Is(err, errOrderFirmScope):
		writeErr(w, http.StatusForbidden, err.Error())
	default:
		if s.log != nil && !errors.Is(err, quikexport.ErrNotConfigured) && !errors.Is(err, errQuikRefsUnavailable) {
			s.log.Error("QUIK Trdaccs/Firms read failed", "error", err)
		}
		writeErr(w, http.StatusServiceUnavailable, errQuikRefsUnavailable.Error())
	}
}

func (s *Server) brokerFirm(r *http.Request) (string, error) {
	b, ok := BrokerFromContext(r.Context())
	if !ok {
		return "", errOrderFirmScope
	}
	tok, _ := TokenFromContext(r.Context())
	firm := quikexport.BrokerOrderFirm(b.Code, tok.QuikFirm)
	if firm == "" {
		return "", errOrderFirmScope
	}
	return firm, nil
}

func (s *Server) listBrokerAccounts(w http.ResponseWriter, r *http.Request) {
	firm, err := s.brokerFirm(r)
	if err == nil && s.quikRefs == nil {
		err = errQuikRefsUnavailable
	}
	var rows []quikexport.Row
	if err == nil {
		rows, err = s.quikRefs.ListTradingAccounts(r.Context(), firm)
	}
	if err != nil {
		s.writeQuikRefError(w, err)
		return
	}
	writeQuikJSON(w, rows)
}

func (s *Server) getBrokerFirm(w http.ResponseWriter, r *http.Request) {
	firm, err := s.brokerFirm(r)
	if err == nil && s.quikRefs == nil {
		err = errQuikRefsUnavailable
	}
	var row quikexport.Row
	if err == nil {
		row, err = s.quikRefs.Firm(r.Context(), firm)
	}
	if err != nil {
		s.writeQuikRefError(w, err)
		return
	}
	writeQuikJSON(w, row)
}

// listAccounts shows an admin the Trdaccs rows of the firms the broker's
// active tokens trade for.
func (s *Server) listAccounts(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	b, err := s.brokers.GetBroker(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	tokens, err := s.brokers.ListTokens(r.Context(), id)
	if err != nil {
		s.writeInternal(w, err)
		return
	}
	firmSet := map[string]bool{}
	for _, t := range tokens {
		if t.RevokedAt == nil {
			if firm := quikexport.BrokerOrderFirm(b.Code, t.QuikFirm); firm != "" {
				firmSet[firm] = true
			}
		}
	}
	firms := make([]string, 0, len(firmSet))
	for firm := range firmSet {
		firms = append(firms, firm)
	}
	sort.Strings(firms)
	if s.quikRefs == nil {
		s.writeQuikRefError(w, errQuikRefsUnavailable)
		return
	}
	rows := []quikexport.Row{}
	for _, firm := range firms {
		list, err := s.quikRefs.ListTradingAccounts(r.Context(), firm)
		if err != nil {
			s.writeQuikRefError(w, err)
			return
		}
		rows = append(rows, list...)
	}
	w.Header().Set("X-Firms", strings.Join(firms, ","))
	writeQuikJSON(w, rows)
}

func (s *Server) listQuikFirms(w http.ResponseWriter, r *http.Request) {
	if s.quikRefs == nil {
		s.writeQuikRefError(w, errQuikRefsUnavailable)
		return
	}
	rows, err := s.quikRefs.ListFirms(r.Context())
	if err != nil {
		s.writeQuikRefError(w, err)
		return
	}
	writeQuikJSON(w, rows)
}

// checkTokenFirm accepts only a firm present in QUIK Firms (FIX tag 5018).
// It writes the response and returns false when the firm is not acceptable.
func (s *Server) checkTokenFirm(w http.ResponseWriter, r *http.Request, firm string) bool {
	firm = strings.TrimSpace(firm)
	if firm == "" {
		writeErr(w, http.StatusBadRequest, "quik_firm is required (код фирмы → FIX tag 5018)")
		return false
	}
	if s.quikRefs == nil {
		s.writeQuikRefError(w, errQuikRefsUnavailable)
		return false
	}
	if _, err := s.quikRefs.Firm(r.Context(), firm); err != nil {
		if errors.Is(err, quikexport.ErrFirmNotFound) {
			writeErr(w, http.StatusBadRequest, "quik_firm "+strconv.Quote(firm)+" не найден в QUIK Firms")
		} else {
			s.writeQuikRefError(w, err)
		}
		return false
	}
	return true
}
