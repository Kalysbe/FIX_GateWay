package api

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"gopkg.in/yaml.v3"
)

func (s *Server) serveMergedOpenAPI(w http.ResponseWriter, r *http.Request, routes []docRoute, baseFile string, brokerOnly bool) {
	if !s.docsEnabled() {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	body, err := mergeOpenAPI(baseFile, routes, brokerOnly, r)
	if err != nil {
		s.writeInternal(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func mergeOpenAPI(baseFile string, routes []docRoute, brokerOnly bool, r *http.Request) ([]byte, error) {
	doc, err := loadOpenAPIBase(baseFile)
	if err != nil {
		return nil, err
	}
	ensureSecurityScheme(doc)
	if doc.Paths == nil {
		doc.Paths = openapi3.NewPaths()
	}
	for _, rt := range routes {
		if brokerOnly && !brokerVisiblePath(rt.Path) {
			continue
		}
		ensureOperation(doc, rt)
	}
	if r != nil && r.Host != "" {
		scheme := "http"
		if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
			scheme = "https"
		}
		doc.Servers = openapi3.Servers{{URL: scheme + "://" + r.Host}}
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	var n any
	if err := json.Unmarshal(raw, &n); err != nil {
		return nil, err
	}
	return yaml.Marshal(n)
}

func loadOpenAPIBase(path string) (*openapi3.T, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return emptyOpenAPI(), nil
		}
		return nil, err
	}
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(raw)
	if err != nil {
		return nil, err
	}
	if doc.Info != nil && doc.Info.Description != "" && !strings.Contains(doc.Info.Description, "ServeMux") {
		doc.Info.Description += "\n\nНедостающие пути дополняются автоматически из зарегистрированных HTTP-маршрутов."
	}
	return doc, nil
}

func emptyOpenAPI() *openapi3.T {
	return &openapi3.T{
		OpenAPI: "3.0.3",
		Info: &openapi3.Info{
			Title:       "FIX Gateway API",
			Version:     "0.4.0",
			Description: "Сгенерировано автоматически из HTTP-маршрутов.",
		},
		Paths: openapi3.NewPaths(),
	}
}

func ensureSecurityScheme(doc *openapi3.T) {
	if doc.Components == nil {
		doc.Components = &openapi3.Components{}
	}
	if doc.Components.SecuritySchemes == nil {
		doc.Components.SecuritySchemes = openapi3.SecuritySchemes{}
	}
	if _, ok := doc.Components.SecuritySchemes["bearerAuth"]; ok {
		return
	}
	doc.Components.SecuritySchemes["bearerAuth"] = &openapi3.SecuritySchemeRef{
		Value: &openapi3.SecurityScheme{
			Type:         "http",
			Scheme:       "bearer",
			BearerFormat: "API token or JWT",
		},
	}
}

func ensureOperation(doc *openapi3.T, rt docRoute) {
	item := doc.Paths.Value(rt.Path)
	if item == nil {
		item = &openapi3.PathItem{}
		doc.Paths.Set(rt.Path, item)
	}
	if item.GetOperation(rt.Method) != nil {
		return
	}
	op := &openapi3.Operation{
		Summary:     autoSummary(rt.Method, rt.Path),
		Description: "Операция зарегистрирована в HTTP mux и добавлена в Swagger автоматически.",
		OperationID: autoOperationID(rt.Method, rt.Path),
		Tags:        []string{autoTag(rt.Path)},
		Responses:   openapi3.NewResponses(openapi3.WithStatus(http.StatusOK, &openapi3.ResponseRef{Value: openapi3.NewResponse().WithDescription("OK")})),
	}
	for _, name := range pathParamNames(rt.Path) {
		op.AddParameter(openapi3.NewPathParameter(name).WithSchema(openapi3.NewStringSchema()))
	}
	if needsBearer(rt.Path) {
		op.Security = &openapi3.SecurityRequirements{{"bearerAuth": []string{}}}
	}
	item.SetOperation(rt.Method, op)
}

func brokerVisiblePath(path string) bool {
	if path == "/health" || path == "/ready" {
		return true
	}
	if strings.HasPrefix(path, "/api/v1/admin") || path == "/metrics" {
		return false
	}
	return strings.HasPrefix(path, "/api/v1/")
}

func needsBearer(path string) bool {
	if path == "/health" || path == "/ready" || path == "/metrics" {
		return false
	}
	if path == "/api/v1/admin/login" {
		return false
	}
	return strings.HasPrefix(path, "/api/v1/")
}

func pathParamNames(path string) []string {
	var out []string
	for _, part := range strings.Split(path, "/") {
		if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
			name := strings.TrimSuffix(strings.TrimPrefix(part, "{"), "}")
			if name != "" {
				out = append(out, name)
			}
		}
	}
	return out
}

func autoTag(path string) string {
	switch {
	case path == "/health" || path == "/ready" || path == "/metrics":
		return "Ops"
	case strings.Contains(path, "/quik-api"):
		return "QUIK API"
	case strings.HasPrefix(path, "/api/v1/admin"):
		return "Admin"
	case strings.HasPrefix(path, "/api/v1/orders"):
		return "Orders"
	case strings.HasPrefix(path, "/api/v1/trades"):
		return "Trades"
	case strings.HasPrefix(path, "/api/v1/positions"):
		return "Positions"
	case strings.HasPrefix(path, "/api/v1/accounts"), strings.HasPrefix(path, "/api/v1/instruments"), strings.HasPrefix(path, "/api/v1/orderbook"), path == "/api/v1/firm":
		return "Reference"
	default:
		return "API"
	}
}

func autoSummary(method, path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	var words []string
	for _, p := range parts {
		if p == "api" || p == "v1" || strings.HasPrefix(p, "{") {
			continue
		}
		words = append(words, strings.ReplaceAll(p, "-", " "))
	}
	label := strings.Join(words, " ")
	verb := method
	switch method {
	case http.MethodGet:
		if strings.Contains(path, "{") {
			verb = "Get"
		} else {
			verb = "List"
		}
	case http.MethodPost:
		switch {
		case strings.Contains(path, "cancel"):
			verb = "Cancel"
		case strings.Contains(path, "login"):
			verb = "Login"
		case strings.Contains(path, "sync"):
			verb = "Sync"
		case strings.Contains(path, "revoke"):
			verb = "Revoke"
		case strings.Contains(path, "rotate"):
			verb = "Rotate"
		default:
			verb = "Create"
		}
	case http.MethodPatch, http.MethodPut:
		verb = "Update"
	case http.MethodDelete:
		verb = "Delete"
	}
	if label == "" {
		return verb
	}
	return verb + " " + label
}

func autoOperationID(method, path string) string {
	p := strings.Trim(path, "/")
	p = strings.ReplaceAll(p, "/", "_")
	p = strings.ReplaceAll(p, "{", "")
	p = strings.ReplaceAll(p, "}", "")
	p = strings.ReplaceAll(p, "-", "_")
	return strings.ToLower(method) + "_" + p
}
