package quikapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestClientQuotesMapsPascalCase(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/quotes" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("ClassCode") != "LIST_GBA" {
			t.Errorf("ClassCode=%q", r.URL.Query().Get("ClassCode"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[{"Operation":"Продажа","OrderNum":"1793","ClassCode":"LIST_GBA","SecCode":"GBA02280810","Price":"90.00000000","Value":"90000000.00000000","Qty":"1000000.00000000"}]`)
	}))
	defer upstream.Close()

	c := NewClient(NewStore(nil, upstream.URL), upstream.Client())
	q := url.Values{"class_code": []string{"LIST_GBA"}}
	res, err := c.Quotes(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if res.Count != 1 || len(res.Quotes) != 1 {
		t.Fatalf("count=%d quotes=%d", res.Count, len(res.Quotes))
	}
	got := res.Quotes[0]
	if got.Operation != "Продажа" || got.OrderNum != "1793" || got.ClassCode != "LIST_GBA" || got.SecCode != "GBA02280810" {
		t.Fatalf("%+v", got)
	}
	if got.Price != "90.00000000" || got.Qty != "1000000.00000000" {
		t.Fatalf("numeric fields: %+v", got)
	}
	if res.Source != "quik_api" || res.BaseURL != strings.TrimRight(upstream.URL, "/") {
		t.Fatalf("meta %+v", res)
	}
}

func TestClientQuotesUpstreamError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "message": "Таблица Orders не найдена", "error": "NotFoundError"})
	}))
	defer upstream.Close()

	c := NewClient(NewStore(nil, upstream.URL), upstream.Client())
	_, err := c.Quotes(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "Таблица Orders не найдена") {
		t.Fatalf("err=%v", err)
	}
}

func TestClientSetBaseURL(t *testing.T) {
	c := NewClient(NewStore(nil, DefaultBaseURL), nil)
	got, err := c.SetBaseURL(context.Background(), "10.1.2.3:5000", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if got != "http://10.1.2.3:5000" {
		t.Fatalf("got %q", got)
	}
	base, def, err := c.Settings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if base != "http://10.1.2.3:5000" || def != DefaultBaseURL {
		t.Fatalf("base=%q def=%q", base, def)
	}
}
