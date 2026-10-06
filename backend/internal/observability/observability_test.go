package observability

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestClassifyBrokerAPI(t *testing.T) {
	cases := []struct {
		method, path, subtype string
	}{
		{"POST", "/api/v1/orders", SubOrderCreate},
		{"GET", "/api/v1/orders", SubOrdersList},
		{"GET", "/api/v1/orders/abc", SubOrderRead},
		{"POST", "/api/v1/orders/abc/cancel", SubOrderCancel},
		{"GET", "/api/v1/trades", SubTradesList},
		{"GET", "/api/v1/positions", SubPositionsList},
		{"GET", "/api/v1/instruments", SubInstrumentRead},
		{"GET", "/health", SubHealthRequest},
		{"POST", "/api/v1/admin/brokers", SubBrokerRequest},
		{"POST", "/api/v1/admin/brokers/1/tokens", SubTokenRequest},
	}
	for _, c := range cases {
		got := ClassifyRequest(c.method, c.path)
		if got.Subtype != c.subtype {
			t.Fatalf("%s %s: got %s want %s", c.method, c.path, got.Subtype, c.subtype)
		}
		if got.Type != TypeAPI {
			t.Fatalf("%s %s type %s", c.method, c.path, got.Type)
		}
	}
}

func TestRedactSecrets(t *testing.T) {
	in := "Authorization: Bearer sk_live_abc123 password=supersecret"
	out := RedactSecrets(in)
	if strings.Contains(out, "sk_live_abc123") || strings.Contains(out, "supersecret") {
		t.Fatalf("secret leaked: %s", out)
	}
	if !strings.Contains(out, "***") {
		t.Fatalf("expected redaction: %s", out)
	}
}

func TestRedactQuery(t *testing.T) {
	out := RedactQuery("foo=1&token=secret&bar=2")
	if strings.Contains(out, "secret") {
		t.Fatalf("token leaked: %s", out)
	}
}

func TestPerfClass(t *testing.T) {
	if PerfClass(40, 100, 500, 2000) != PerfFast {
		t.Fatal("fast")
	}
	if PerfClass(120, 100, 500, 2000) != PerfNormal {
		t.Fatal("normal")
	}
	if PerfClass(800, 100, 500, 2000) != PerfSlow {
		t.Fatal("slow")
	}
	if PerfClass(3000, 100, 500, 2000) != PerfVerySlow {
		t.Fatal("very slow")
	}
}

func TestClientIPTrustedProxy(t *testing.T) {
	_, n, _ := net.ParseCIDR("127.0.0.1/32")
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "127.0.0.1:9"
	r.Header.Set("X-Forwarded-For", "203.0.113.10, 127.0.0.1")
	ip, xff := ClientIP(r, []*net.IPNet{n})
	if ip != "203.0.113.10" {
		t.Fatalf("got %s", ip)
	}
	if xff == "" {
		t.Fatal("forwarded-for empty")
	}
}

func TestClientIPUntrusted(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "203.0.113.50:9"
	r.Header.Set("X-Forwarded-For", "1.2.3.4")
	ip, _ := ClientIP(r, nil)
	if ip != "203.0.113.50" {
		t.Fatalf("untrusted xff must not win, got %s", ip)
	}
}

func TestRequestProtoUntrusted(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "203.0.113.50:9"
	r.Header.Set("X-Forwarded-Proto", "https")
	if RequestProto(r, nil) != "http" {
		t.Fatal("spoofed proto must not win without trusted proxy")
	}
}

func TestRequestIDFormat(t *testing.T) {
	id := NewRequestID()
	if !strings.HasPrefix(id, "req_") || len(id) < 10 {
		t.Fatalf("bad request id %s", id)
	}
	if NewTraceID() == "" {
		t.Fatal("trace id")
	}
}

func TestAuthSubtype(t *testing.T) {
	if AuthSubtypeFromReason("ip not allowed") != AuthIPNotAllowed {
		t.Fatal("ip")
	}
	if AuthSubtypeFromReason("ip not allowed: 172.19.0.1") != AuthIPNotAllowed {
		t.Fatal("ip with address")
	}
	if AuthSubtypeFromReason("token expired") != AuthTokenExpired {
		t.Fatal("expired")
	}
}

func TestIPNotAllowedMessage(t *testing.T) {
	if got := IPNotAllowedMessage("172.19.0.1", ""); got != "ip not allowed: 172.19.0.1" {
		t.Fatalf("got %q", got)
	}
	if got := IPNotAllowedMessage("172.19.0.1", "203.0.113.10"); got != "ip not allowed: 172.19.0.1 (X-Forwarded-For: 203.0.113.10)" {
		t.Fatalf("got %q", got)
	}
	if got := IPNotAllowedMessage("172.19.0.1", "172.19.0.1"); got != "ip not allowed: 172.19.0.1" {
		t.Fatalf("duplicate xff: %q", got)
	}
}

func TestCursorRoundtrip(t *testing.T) {
	ts := time.Now().UTC().Truncate(time.Microsecond)
	uid := uuid.New()
	c := EncodeCursor(ts, uid)
	gotTS, gotID, err := DecodeCursor(c)
	if err != nil {
		t.Fatal(err)
	}
	if gotID != uid {
		t.Fatal("id")
	}
	if !gotTS.Equal(ts) {
		t.Fatalf("%v vs %v", gotTS, ts)
	}
}

func TestClampLogListLimit(t *testing.T) {
	if clampLogListLimit(0) != 25 || clampLogListLimit(-1) != 25 {
		t.Fatal("default")
	}
	if clampLogListLimit(25) != 25 || clampLogListLimit(100) != 100 {
		t.Fatal("passthrough")
	}
	if clampLogListLimit(101) != 100 || clampLogListLimit(5000) != 100 {
		t.Fatal("cap")
	}
}

func TestIsInternalUIRequest(t *testing.T) {
	skip := skipHTTPHosts(nil)
	admin := httptest.NewRequest(http.MethodGet, "/api/v1/admin/dashboard", nil)
	admin.Host = "localhost:5173"
	if IsInternalUIRequest(admin, skip) {
		t.Fatal("admin API must be logged")
	}
	fromOrigin := httptest.NewRequest(http.MethodGet, "/api/v1/admin/dashboard", nil)
	fromOrigin.Host = "localhost:8080"
	fromOrigin.Header.Set("Origin", "http://localhost:5173")
	if IsInternalUIRequest(fromOrigin, skip) {
		t.Fatal("origin cannot suppress API logging")
	}
	broker := httptest.NewRequest(http.MethodGet, "/api/v1/orders", nil)
	broker.Host = "localhost:5174"
	broker.Header.Set("Origin", "http://localhost:5174")
	if IsInternalUIRequest(broker, skip) {
		t.Fatal("broker-app 5174 must be logged")
	}
	external := httptest.NewRequest(http.MethodPost, "/api/v1/orders", nil)
	external.Host = "localhost:8080"
	external.Header.Set("Origin", "https://broker.example.com")
	if IsInternalUIRequest(external, skip) {
		t.Fatal("external broker must be logged")
	}
}
