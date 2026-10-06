package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSplitMuxPattern(t *testing.T) {
	t.Parallel()
	method, path, ok := splitMuxPattern("PATCH /api/v1/admin/brokers/{id}/accounts/{account_id}")
	if !ok || method != http.MethodPatch || path != "/api/v1/admin/brokers/{id}/accounts/{account_id}" {
		t.Fatalf("got method=%q path=%q ok=%v", method, path, ok)
	}
}

func TestMergeOpenAPIAddsMissingRoute(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	base := filepath.Join(dir, "openapi.yaml")
	if err := os.WriteFile(base, []byte(`openapi: 3.0.3
info:
  title: Test
  version: "1"
paths:
  /health:
    get:
      summary: Liveness
      responses:
        "200":
          description: OK
`), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := mergeOpenAPI(base, []docRoute{
		{Method: http.MethodGet, Path: "/health"},
		{Method: http.MethodPatch, Path: "/api/v1/admin/brokers/{id}/accounts/{account_id}"},
		{Method: http.MethodGet, Path: "/api/v1/trades"},
	}, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	text := string(out)
	if !strings.Contains(text, "/api/v1/admin/brokers/{id}/accounts/{account_id}") {
		t.Fatalf("missing auto route:\n%s", text)
	}
	if !strings.Contains(text, "Liveness") {
		t.Fatalf("existing summary was overwritten:\n%s", text)
	}
	if !strings.Contains(text, "/api/v1/trades") {
		t.Fatalf("missing trades route:\n%s", text)
	}
}

func TestMergeRealOpenAPIFile(t *testing.T) {
	base := findOpenAPIFile(t)
	mux := newDocMux()
	mux.HandleFunc("GET /health", func(http.ResponseWriter, *http.Request) {})
	mux.HandleFunc("PATCH /api/v1/admin/brokers/{id}/accounts/{account_id}", func(http.ResponseWriter, *http.Request) {})
	mux.HandleFunc("GET /api/v1/trades", func(http.ResponseWriter, *http.Request) {})
	mux.HandleFunc("GET /api/v1/admin/definitely-not-in-spec", func(http.ResponseWriter, *http.Request) {})
	out, err := mergeOpenAPI(base, mux.routes, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	text := string(out)
	if !strings.Contains(text, "/api/v1/admin/brokers/{id}/accounts/{account_id}") {
		t.Fatalf("missing account patch path")
	}
	if !strings.Contains(text, "/api/v1/admin/definitely-not-in-spec") {
		t.Fatalf("auto-discovered route missing from merged spec")
	}
}

func findOpenAPIFile(t *testing.T) string {
	t.Helper()
	for _, p := range []string{"docs/openapi.yaml", "../../docs/openapi.yaml"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	t.Skip("docs/openapi.yaml not found")
	return ""
}

func TestMergeOpenAPIBrokerOnlyHidesAdmin(t *testing.T) {
	t.Parallel()
	out, err := mergeOpenAPI("", []docRoute{
		{Method: http.MethodGet, Path: "/api/v1/trades"},
		{Method: http.MethodGet, Path: "/api/v1/admin/orders"},
	}, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	text := string(out)
	if !strings.Contains(text, "/api/v1/trades") {
		t.Fatalf("broker trades missing:\n%s", text)
	}
	if strings.Contains(text, "/api/v1/admin/orders") {
		t.Fatalf("admin route leaked into broker spec:\n%s", text)
	}
}

func TestAutoTagQuikAPI(t *testing.T) {
	t.Parallel()
	if got := autoTag("/api/v1/admin/quik-api/quotes"); got != "QUIK API" {
		t.Fatalf("got %q", got)
	}
}
