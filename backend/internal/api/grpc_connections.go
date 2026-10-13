package api

import "net/http"

func (s *Server) grpcConnections(w http.ResponseWriter, r *http.Request) {
	if s.hub == nil {
		writeErr(w, http.StatusServiceUnavailable, "gRPC realtime unavailable")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, s.hub.Connections())
}
