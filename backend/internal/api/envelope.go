package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

// Every broker REST reply has one shape (поручение пользователя 30.10.2026):
//
//	{"success": bool, "code": "OK"|<error code>, "message": null|<reason>, "data": <payload>|null}
//
// Lists add has_more and next_offset. Payloads (QUIK rows) are copied byte for
// byte, so their numbers keep full precision.
type brokerEnvelope struct {
	Success    bool            `json:"success"`
	Code       string          `json:"code"`
	Message    *string         `json:"message"`
	Data       json.RawMessage `json:"data"`
	HasMore    *bool           `json:"has_more,omitempty"`
	NextOffset *int            `json:"next_offset,omitempty"`
}

// brokerPath selects the broker API: /api/v1/* except the admin API, and /health.
func brokerPath(path string) bool {
	return path == "/health" || (strings.HasPrefix(path, "/api/v1/") && !strings.HasPrefix(path, "/api/v1/admin/"))
}

var statusCodes = map[int]string{
	http.StatusBadRequest:            "VALIDATION_ERROR",
	http.StatusUnauthorized:          "UNAUTHORIZED",
	http.StatusForbidden:             "FORBIDDEN",
	http.StatusNotFound:              "NOT_FOUND",
	http.StatusMethodNotAllowed:      "METHOD_NOT_ALLOWED",
	http.StatusConflict:              "CONFLICT",
	http.StatusRequestEntityTooLarge: "PAYLOAD_TOO_LARGE",
	http.StatusUnprocessableEntity:   "UNPROCESSABLE",
	http.StatusTooManyRequests:       "RATE_LIMITED",
	http.StatusInternalServerError:   "INTERNAL_ERROR",
	http.StatusBadGateway:            "BAD_GATEWAY",
	http.StatusServiceUnavailable:    "SERVICE_UNAVAILABLE",
	http.StatusGatewayTimeout:        "TIMEOUT",
}

type bufferedResponse struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (b *bufferedResponse) Header() http.Header { return b.header }
func (b *bufferedResponse) WriteHeader(status int) {
	if b.status == 0 {
		b.status = status
	}
}
func (b *bufferedResponse) Write(p []byte) (int, error) {
	if b.status == 0 {
		b.status = http.StatusOK
	}
	return b.body.Write(p)
}

func (s *Server) brokerEnvelope(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !brokerPath(r.URL.Path) || r.Method == http.MethodOptions || r.Method == http.MethodHead {
			next.ServeHTTP(w, r)
			return
		}
		buf := &bufferedResponse{header: w.Header()}
		next.ServeHTTP(buf, r)
		status := buf.status
		if status == 0 {
			status = http.StatusOK
		}
		var encoded bytes.Buffer
		enc := json.NewEncoder(&encoded)
		enc.SetEscapeHTML(false) // keep the payload bytes as the handler wrote them
		if err := enc.Encode(envelopeFor(status, buf.header, buf.body.Bytes())); err != nil {
			encoded.Reset()
			encoded.WriteString(`{"success":false,"code":"INTERNAL_ERROR","message":"internal error","data":null}` + "\n")
			status = http.StatusInternalServerError
		}
		w.Header().Del("Content-Length")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write(encoded.Bytes())
	})
}

// envelopeFor: 200/201 are successes with the handler's JSON as data. Any
// other status (202: QUIK has not answered yet) is a failure; its "code" and
// "error" become code and message, and the remaining fields (an order) data.
func envelopeFor(status int, header http.Header, body []byte) brokerEnvelope {
	body = bytes.TrimSpace(body)
	isJSON := len(body) > 0 && json.Valid(body)
	if status == http.StatusOK || status == http.StatusCreated {
		out := brokerEnvelope{Success: true, Code: "OK", Data: json.RawMessage("null")}
		if isJSON {
			out.Data = json.RawMessage(body)
		}
		if raw := header.Get("X-Has-More"); raw != "" {
			more := raw == "true"
			out.HasMore = &more
			if next, err := strconv.Atoi(header.Get("X-Next-Offset")); err == nil {
				out.NextOffset = &next
			}
		}
		return out
	}
	out := brokerEnvelope{Code: statusCodes[status], Data: json.RawMessage("null")}
	if out.Code == "" {
		out.Code = "ERROR"
	}
	message := http.StatusText(status)
	var fields map[string]json.RawMessage
	if isJSON && json.Unmarshal(body, &fields) == nil {
		var text string
		if json.Unmarshal(fields["code"], &text) == nil && text != "" {
			out.Code = text
		}
		if json.Unmarshal(fields["error"], &text) == nil && text != "" {
			message = text
		}
		delete(fields, "code")
		delete(fields, "error")
		if len(fields) > 0 {
			if data, err := json.Marshal(fields); err == nil {
				out.Data = data
			}
		}
	} else if text := strings.TrimSpace(string(body)); text != "" && !isJSON {
		message = text
	}
	out.Message = &message
	return out
}
