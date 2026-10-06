package api

import (
	"net/http"
	"strings"
)

type docRoute struct {
	Method string
	Path   string
}

type docMux struct {
	*http.ServeMux
	routes []docRoute
}

func newDocMux() *docMux {
	return &docMux{ServeMux: http.NewServeMux()}
}

func (m *docMux) Handle(pattern string, h http.Handler) {
	m.record(pattern)
	m.ServeMux.Handle(pattern, h)
}

func (m *docMux) HandleFunc(pattern string, h http.HandlerFunc) {
	m.record(pattern)
	m.ServeMux.HandleFunc(pattern, h)
}

func (m *docMux) record(pattern string) {
	method, path, ok := splitMuxPattern(pattern)
	if !ok || skipOpenAPIPath(path) {
		return
	}
	m.routes = append(m.routes, docRoute{Method: method, Path: path})
}

func splitMuxPattern(pattern string) (method, path string, ok bool) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return "", "", false
	}
	method, path, found := strings.Cut(pattern, " ")
	if !found {
		return http.MethodGet, pattern, true
	}
	method = strings.ToUpper(strings.TrimSpace(method))
	path = strings.TrimSpace(path)
	if method == "" || path == "" || !strings.HasPrefix(path, "/") {
		return "", "", false
	}
	return method, path, true
}

func skipOpenAPIPath(path string) bool {
	switch path {
	case "/swagger", "/broker-swagger", "/api/openapi.yaml", "/api/broker-openapi.yaml":
		return true
	}
	return strings.HasPrefix(path, "/docs/")
}
