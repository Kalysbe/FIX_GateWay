package observability

import (
	"net/http"
)

// HTTP request records are limited to POST, PATCH and DELETE. Independent
// security audit, trading lifecycle and system events retain their own policy.
func keepEvent(e Event) bool {
	if e.Type == TypeAPI || (e.Type == TypeError && e.HTTPMethod != "") {
		if e.HTTPMethod != http.MethodPost && e.HTTPMethod != http.MethodPatch && e.HTTPMethod != http.MethodDelete {
			return false
		}
	}
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
	}
	return true
}
