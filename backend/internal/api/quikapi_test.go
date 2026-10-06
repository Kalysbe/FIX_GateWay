package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fix-gateway/backend/internal/quikapi"
)

func TestListQuikAPIQuotes(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/quotes" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"Operation":"Купля","OrderNum":"1","ClassCode":"LISTEDSTOCKK","SecCode":"KG0105166315","Price":10,"Value":100,"Qty":2}]`))
	}))
	defer upstream.Close()

	s := &Server{quikAPI: quikapi.NewClient(quikapi.NewStore(nil, upstream.URL), upstream.Client())}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/quik-api/quotes", nil)
	rr := httptest.NewRecorder()
	s.listQuikAPIQuotes(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d body=%s", rr.Code, rr.Body.String())
	}
	var got quikapi.QuotesResult
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Count != 1 || got.Quotes[0].Operation != "Купля" || got.Quotes[0].Price != "10" {
		t.Fatalf("%+v", got)
	}
}

func TestPatchQuikAPISettingsRejectsBadURL(t *testing.T) {
	s := &Server{quikAPI: quikapi.NewClient(quikapi.NewStore(nil, quikapi.DefaultBaseURL), nil)}
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/admin/quik-api/settings", strings.NewReader(`{"base_url":"ftp://evil"}`))
	rr := httptest.NewRecorder()
	s.patchQuikAPISettings(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestPatchQuikAPISettingsSaves(t *testing.T) {
	s := &Server{quikAPI: quikapi.NewClient(quikapi.NewStore(nil, quikapi.DefaultBaseURL), nil)}
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/admin/quik-api/settings", strings.NewReader(`{"base_url":"http://192.168.1.10:5000"}`))
	rr := httptest.NewRecorder()
	s.patchQuikAPISettings(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d body=%s", rr.Code, rr.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["base_url"] != "http://192.168.1.10:5000" {
		t.Fatalf("%v", got)
	}
}
