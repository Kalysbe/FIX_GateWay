package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/fix-gateway/backend/internal/brokers"
	"github.com/fix-gateway/backend/internal/domain"
	"github.com/google/uuid"
)

func lookupTokenDTO(token domain.APIToken, now time.Time) map[string]any {
	result := safeToken(token)
	if token.Status == domain.TokenActive && token.ExpiresAt != nil && !now.Before(*token.ExpiresAt) {
		result["status"] = domain.TokenExpired
	}
	return result
}

func (s *Server) searchTokens(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	var input struct {
		Token string `json:"token"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2048)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid token search request")
		return
	}
	if decoder.Decode(new(any)) != io.EOF {
		writeErr(w, http.StatusBadRequest, "invalid token search request")
		return
	}
	value, err := brokers.NormalizeTokenLookup(input.Token)
	if err != nil {
		writeErr(w, http.StatusBadRequest, brokers.ErrTokenLookup.Error())
		return
	}
	rows, more, err := s.brokers.SearchTokens(r.Context(), value)
	if err != nil {
		s.writeInternal(w, err)
		return
	}
	results := make([]map[string]any, 0, len(rows))
	ids := make([]string, 0, len(rows))
	now := time.Now().UTC()
	for _, row := range rows {
		item := lookupTokenDTO(row.Token, now)
		item["broker_code"], item["broker_name"] = row.BrokerCode, row.BrokerName
		results = append(results, item)
		ids = append(ids, row.Token.ID.String())
	}
	if s.audit != nil {
		s.writeAdminAudit(r, "TOKEN_LOOKUP", "api_token", "", nil, nil, map[string]any{"matched_ids": ids})
	}
	writeJSON(w, http.StatusOK, map[string]any{"tokens": results, "has_more": more})
}

func (s *Server) getTokenOverview(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid token id")
		return
	}
	token, err := s.brokers.GetToken(r.Context(), id)
	if errors.Is(err, brokers.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "token not found")
		return
	}
	if err != nil {
		s.writeInternal(w, err)
		return
	}
	broker, err := s.brokers.GetBroker(r.Context(), token.BrokerID)
	if err != nil {
		s.writeInternal(w, err)
		return
	}
	whitelist, err := s.brokers.ListIPWhitelist(r.Context(), token.BrokerID)
	if err != nil {
		s.writeInternal(w, err)
		return
	}
	if whitelist == nil {
		whitelist = []domain.BrokerIPAllow{}
	}
	if s.audit != nil {
		s.writeAdminAudit(r, "TOKEN_INSPECTED", "api_token", id.String(), &token.BrokerID, nil, nil)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"token": lookupTokenDTO(token, time.Now().UTC()), "broker": broker, "ip_whitelist": whitelist,
	})
}
