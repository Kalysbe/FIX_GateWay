package api

import (
	"github.com/fix-gateway/backend/internal/observability"
	"net"
	"net/http"
)

func (s *Server) restrictInternal(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := net.ParseIP(observability.RemoteIP(r))
		if ip == nil || !(ip.IsLoopback() || ip.IsPrivate()) {
			writeErr(w, http.StatusForbidden, "forbidden")
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (s *Server) docsEnabled() bool { return s.cfg.DocsPublic || !s.cfg.IsProduction() }
