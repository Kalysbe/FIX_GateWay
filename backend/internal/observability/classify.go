package observability

import (
	"net/http"
	"regexp"
	"strings"
)

type RouteClass struct {
	Type    string
	Subtype string
	Route   string
}

func ClassifyRequest(method, path string) RouteClass {
	p := strings.TrimRight(path, "/")
	if p == "" {
		p = "/"
	}
	m := strings.ToUpper(method)

	switch {
	case p == "/health" || p == "/ready":
		return RouteClass{TypeAPI, SubHealthRequest, p}
	case p == "/metrics":
		return RouteClass{TypeAPI, SubHealthRequest, p}
	}

	if strings.HasPrefix(p, "/api/v1/admin") {
		return classifyAdmin(m, p)
	}
	if strings.HasPrefix(p, "/api/v1/") {
		return classifyBrokerAPI(m, p)
	}
	return RouteClass{TypeAPI, SubUnknown, p}
}

func classifyBrokerAPI(method, path string) RouteClass {
	switch {
	case method == http.MethodPost && path == "/api/v1/orders":
		return RouteClass{TypeAPI, SubOrderCreate, "/api/v1/orders"}
	case method == http.MethodGet && path == "/api/v1/orders":
		return RouteClass{TypeAPI, SubOrdersList, "/api/v1/orders"}
	case method == http.MethodGet && orderIDPath.MatchString(path):
		return RouteClass{TypeAPI, SubOrderRead, "/api/v1/orders/{id}"}
	case method == http.MethodPost && strings.HasSuffix(path, "/cancel") && strings.HasPrefix(path, "/api/v1/orders/"):
		return RouteClass{TypeAPI, SubOrderCancel, "/api/v1/orders/{id}/cancel"}
	case method == http.MethodPost && strings.HasSuffix(path, "/replace") && strings.HasPrefix(path, "/api/v1/orders/"):
		return RouteClass{TypeAPI, SubOrderReplace, "/api/v1/orders/{id}/replace"}
	case path == "/api/v1/trades" || strings.HasPrefix(path, "/api/v1/trades/"):
		if method == http.MethodGet && path != "/api/v1/trades" {
			return RouteClass{TypeAPI, SubTradeRead, "/api/v1/trades/{id}"}
		}
		return RouteClass{TypeAPI, SubTradesList, "/api/v1/trades"}
	case path == "/api/v1/positions" || strings.HasPrefix(path, "/api/v1/positions/"):
		if method == http.MethodGet && path != "/api/v1/positions" {
			return RouteClass{TypeAPI, SubPositionRead, "/api/v1/positions/{id}"}
		}
		return RouteClass{TypeAPI, SubPositionsList, "/api/v1/positions"}
	case strings.HasPrefix(path, "/api/v1/instruments"):
		return RouteClass{TypeAPI, SubInstrumentRead, "/api/v1/instruments"}
	case strings.HasPrefix(path, "/api/v1/accounts") || path == "/api/v1/firm":
		return RouteClass{TypeAPI, SubAccountRead, path}
	default:
		return RouteClass{TypeAPI, SubUnknown, path}
	}
}

func classifyAdmin(method, path string) RouteClass {
	switch {
	case strings.Contains(path, "/tokens"):
		return RouteClass{TypeAPI, SubTokenRequest, adminRoute(path)}
	case strings.Contains(path, "/brokers"):
		return RouteClass{TypeAPI, SubBrokerRequest, adminRoute(path)}
	case path == "/api/v1/admin/login":
		return RouteClass{TypeAPI, SubTokenRequest, path}
	case path == "/health" || path == "/api/v1/admin/events/stream":
		return RouteClass{TypeAPI, SubHealthRequest, path}
	default:
		return RouteClass{TypeAPI, SubAdminRequest, adminRoute(path)}
	}
}

var (
	orderIDPath = regexp.MustCompile(`^/api/v1/orders/[^/]+$`)
	uuidRe      = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)
	numIDRe     = regexp.MustCompile(`/\d+`)
)

func adminRoute(path string) string {
	p := uuidRe.ReplaceAllString(path, "{id}")
	p = numIDRe.ReplaceAllString(p, "/{id}")
	return p
}

func SkipHTTPLog(path string) bool {
	switch path {
	case "/metrics", "/api/openapi.yaml", "/swagger", "/api/broker-openapi.yaml", "/broker-swagger":
		return true
	}
	return path == "/api/v1/admin/events/stream"
}

func ErrorSubtypeForStatus(status int, apiSubtype string) string {
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return ErrAuth
	case status == http.StatusTooManyRequests:
		return ErrRateLimit
	case status == http.StatusRequestTimeout || status == http.StatusGatewayTimeout:
		return ErrTimeout
	case status == http.StatusBadRequest:
		return ErrValidation
	case status >= 500:
		return ErrInternal
	default:
		return ErrSystem
	}
}

func AuthSubtypeFromReason(reason string) string {
	r := strings.ToLower(strings.TrimSpace(reason))
	switch {
	case r == "token expired":
		return AuthTokenExpired
	case r == "token revoked" || strings.Contains(r, "revoked"):
		return AuthTokenRevoked
	case r == "ip not allowed" || strings.HasPrefix(r, "ip not allowed:"):
		return AuthIPNotAllowed
	case strings.Contains(r, "hmac"):
		return AuthHMACFailed
	case strings.Contains(r, "nonce"):
		return AuthNonceReplay
	case strings.Contains(r, "rate limit"):
		return AuthRateLimitExceeded
	case strings.Contains(r, "permission"):
		return AuthPermissionDenied
	case r == "missing_bearer" || strings.Contains(r, "not found") || strings.Contains(r, "invalid"):
		return AuthTokenNotFound
	default:
		return AuthFailed
	}
}
