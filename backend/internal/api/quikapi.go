package api

import (
	"errors"
	"net/http"

	"github.com/fix-gateway/backend/internal/quikapi"
)

func (s *Server) getQuikAPISettings(w http.ResponseWriter, r *http.Request) {
	if s.quikAPI == nil {
		writeErr(w, http.StatusServiceUnavailable, quikapi.ErrNotConfigured.Error())
		return
	}
	base, def, err := s.quikAPI.Settings(r.Context())
	if err != nil {
		s.writeInternal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"base_url":         base,
		"default_base_url": def,
	})
}

func (s *Server) patchQuikAPISettings(w http.ResponseWriter, r *http.Request) {
	if s.quikAPI == nil {
		writeErr(w, http.StatusServiceUnavailable, quikapi.ErrNotConfigured.Error())
		return
	}
	var req struct {
		BaseURL string `json:"base_url"`
	}
	if err := decodeJSONBody(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	admin, _ := AdminFromContext(r.Context())
	before, _, _ := s.quikAPI.Settings(r.Context())
	base, err := s.quikAPI.SetBaseURL(r.Context(), req.BaseURL, admin.Email)
	if err != nil {
		if errors.Is(err, quikapi.ErrInvalidURL) {
			writeErr(w, http.StatusBadRequest, "укажите http(s) адрес хоста, например http://192.168.0.89:5000")
			return
		}
		s.writeInternal(w, err)
		return
	}
	if s.audit != nil {
		s.writeAdminAudit(r, "QUIK_API_BASE_URL_UPDATED", "gateway_settings", quikapi.SettingKey, nil,
			map[string]any{"base_url": before}, map[string]any{"base_url": base})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"base_url":         base,
		"default_base_url": s.quikAPI.DefaultBaseURL(),
	})
}

func (s *Server) listQuikAPIQuotes(w http.ResponseWriter, r *http.Request) {
	if s.quikAPI == nil {
		writeErr(w, http.StatusServiceUnavailable, quikapi.ErrNotConfigured.Error())
		return
	}
	res, err := s.quikAPI.Quotes(r.Context(), r.URL.Query())
	if err != nil {
		switch {
		case errors.Is(err, quikapi.ErrInvalidURL):
			writeErr(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, quikapi.ErrNotConfigured):
			writeErr(w, http.StatusServiceUnavailable, err.Error())
		case errors.Is(err, quikapi.ErrUpstream):
			writeErr(w, http.StatusBadGateway, err.Error())
		default:
			s.writeInternal(w, err)
		}
		return
	}
	writeJSON(w, http.StatusOK, res)
}
