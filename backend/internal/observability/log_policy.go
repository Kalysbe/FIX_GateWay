package observability

import (
	"net/http"
	"strings"
)

// keepEvent suppresses routine diagnostics without changing broker request
// statistics, security failures, trading history, or administrative audit.
func keepEvent(e Event) bool {
	if e.Result == ResultFailure || e.ErrorMessage != "" ||
		e.Severity == SeverityWarning || e.Severity == SeverityError || e.Severity == SeverityCritical ||
		e.Type == TypeError || e.Type == TypeAudit || e.Type == TypeBusiness {
		return true
	}
	if e.Severity == SeverityDebug {
		return false
	}
	if e.Type == TypeAuth && e.Subtype == AuthSuccess && e.ActorType != "admin" {
		return false
	}
	if e.Type == TypeFIX && e.Subtype == "FIX_MESSAGE_SENT" {
		return false
	}
	if e.Type == TypeAPI {
		if e.PerfClass == PerfSlow || e.PerfClass == PerfVerySlow || e.PerfClass == PerfTimeout {
			return true
		}
		if e.Subtype == SubHealthRequest || e.HTTPMethod == http.MethodOptions {
			return false
		}
		admin := e.Endpoint == "/api/v1/admin" || strings.HasPrefix(e.Endpoint, "/api/v1/admin/")
		if admin && (e.HTTPMethod == http.MethodGet || e.HTTPMethod == http.MethodHead) {
			return false
		}
	}
	return true
}
