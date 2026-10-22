package observability

import (
	"bufio"
	"context"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

type HTTPConfig struct {
	TrustedProxies []*net.IPNet
	FastMS         int
	NormalMS       int
	SlowMS         int
	SkipOrigins    []string
}

func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := r.Header.Get("X-Request-ID")
		if reqID == "" {
			reqID = NewRequestID()
		}
		traceID := r.Header.Get("X-Trace-ID")
		if traceID == "" {
			traceID = r.Header.Get("Traceparent")
			if traceID == "" {
				traceID = NewTraceID()
			}
		}
		w.Header().Set("X-Request-ID", reqID)
		w.Header().Set("X-Trace-ID", traceID)
		ctx := WithBag(WithIDs(r.Context(), reqID, traceID), &Bag{})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

type captureWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *captureWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *captureWriter) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	return n, err
}

func (w *captureWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	return h.Hijack()
}

func (w *captureWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *captureWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (p *Pipeline) HTTPMiddleware(cfg HTTPConfig, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if SkipHTTPLog(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		cw := &captureWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(cw, r)
		if cw.status == http.StatusSwitchingProtocols {
			return
		}
		bag := BagFrom(r.Context())
		if bag != nil && bag.SkipAPILog {
			return
		}
		class := ClassifyRequest(r.Method, r.URL.Path)
		ip, xff := ClientIP(r, cfg.TrustedProxies)
		ms := int(time.Since(start).Milliseconds())
		status := cw.status
		reqSize := int(r.ContentLength)
		if reqSize < 0 {
			reqSize = 0
		}
		perf := PerfClass(ms, cfg.FastMS, cfg.NormalMS, cfg.SlowMS)
		if status == http.StatusGatewayTimeout || status == http.StatusRequestTimeout {
			perf = PerfTimeout
		}
		result := ResultSuccess
		if status >= 400 {
			result = ResultFailure
		}
		if bag != nil && bag.Result != "" {
			result = bag.Result
		}
		e := Event{
			Type:           TypeAPI,
			Subtype:        class.Subtype,
			RequestID:      RequestIDFrom(r.Context()),
			TraceID:        TraceIDFrom(r.Context()),
			ClientIP:       ip,
			ForwardedFor:   xff,
			UserAgent:      r.UserAgent(),
			HTTPMethod:     r.Method,
			Endpoint:       r.URL.Path,
			Route:          class.Route,
			QueryParams:    RedactQuery(r.URL.RawQuery),
			Protocol:       r.Proto,
			StatusCode:     &status,
			ResponseTimeMs: &ms,
			RequestSize:    &reqSize,
			ResponseSize:   &cw.bytes,
			PerfClass:      perf,
			Result:         result,
		}
		if bag != nil {
			e.BrokerID = bag.BrokerID
			e.BrokerCode = bag.BrokerCode
			e.BrokerName = bag.BrokerName
			e.TokenID = bag.TokenID
			e.OrderID = bag.OrderID
			e.ClientOrderID = bag.ClientOrderID
			e.AccountID = bag.AccountID
			e.Instrument = bag.Instrument
			e.Side = bag.Side
			e.Quantity = bag.Quantity
			e.Price = bag.Price
			e.AdapterID = bag.AdapterID
			e.ErrorCode = bag.ErrorCode
			e.ErrorMessage = bag.ErrorMessage
			e.FailureStage = bag.FailureStage
		}
		if result == ResultFailure && e.ErrorCode == "" {
			e.ErrorCode = class.Subtype
			if e.ErrorMessage == "" {
				e.ErrorMessage = http.StatusText(status)
			}
			e.FailureStage = "api"
		}
		p.Emit(e)
		if result == ResultFailure && status >= 400 {
			p.Emit(Event{
				Type: TypeError, Subtype: ErrorSubtypeForStatus(status, class.Subtype),
				RequestID: e.RequestID, TraceID: e.TraceID, BrokerID: e.BrokerID, BrokerName: e.BrokerName,
				TokenID: e.TokenID, ClientIP: ip, ForwardedFor: xff, UserAgent: e.UserAgent,
				Endpoint: e.Endpoint, HTTPMethod: e.HTTPMethod,
				StatusCode: &status, OrderID: e.OrderID, ClientOrderID: e.ClientOrderID, AdapterID: e.AdapterID,
				ErrorCode: firstNonEmpty(e.ErrorCode, strconv.Itoa(status)), ErrorMessage: e.ErrorMessage,
				FailureStage: firstNonEmpty(e.FailureStage, "api"), Message: e.ErrorMessage,
			})
		}
	})
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

// Annotate fills the request bag from a successful order operation.
func AnnotateOrder(r *http.Request, orderID uuid.UUID, clientOrderID, account, instrument, side, qty, price string) {
	bag := BagFrom(r.Context())
	if bag == nil {
		return
	}
	bag.OrderID = &orderID
	bag.ClientOrderID = clientOrderID
	bag.AccountID = account
	bag.Instrument = instrument
	bag.Side = side
	bag.Quantity = qty
	bag.Price = price
}

func AnnotateError(r *http.Request, code, message, stage string) {
	bag := BagFrom(r.Context())
	if bag == nil {
		return
	}
	bag.ErrorCode = code
	bag.ErrorMessage = RedactSecrets(message)
	bag.FailureStage = stage
	bag.Result = ResultFailure
}

func AnnotateBroker(r *http.Request, id int64, code, name string, tokenID uuid.UUID) {
	bag := BagFrom(r.Context())
	if bag == nil {
		return
	}
	bag.BrokerID = &id
	bag.BrokerCode = code
	bag.BrokerName = name
	if tokenID != uuid.Nil {
		tid := tokenID
		bag.TokenID = &tid
	}
}

func EventFromContext(ctx context.Context) Event {
	e := Event{RequestID: RequestIDFrom(ctx), TraceID: TraceIDFrom(ctx)}
	if bag := BagFrom(ctx); bag != nil {
		e.BrokerID = bag.BrokerID
		e.BrokerCode = bag.BrokerCode
		e.BrokerName = bag.BrokerName
		e.TokenID = bag.TokenID
		e.OrderID = bag.OrderID
		e.ClientOrderID = bag.ClientOrderID
		e.AccountID = bag.AccountID
		e.Instrument = bag.Instrument
		e.Side = bag.Side
		e.AdapterID = bag.AdapterID
	}
	return e
}

var defaultSkipHTTPHosts = []string{"localhost:5173", "127.0.0.1:5173", "[::1]:5173"}

func skipHTTPHosts(origins []string) map[string]bool {
	out := map[string]bool{}
	for _, h := range defaultSkipHTTPHosts {
		out[h] = true
	}
	for _, raw := range origins {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if host := originHost(raw); host != "" {
			out[strings.ToLower(host)] = true
		}
	}
	return out
}

// IsInternalUIRequest is true for the admin console (Vite :5173), including
// same-origin non-API requests proxied to the gateway. API requests are always logged.
func IsInternalUIRequest(r *http.Request, skipHosts map[string]bool) bool {
	// API logging must never depend on caller-controlled browser/proxy headers.
	if strings.HasPrefix(r.URL.Path, "/api/") {
		return false
	}
	if len(skipHosts) == 0 {
		skipHosts = skipHTTPHosts(nil)
	}
	if skipHosts[hostKey(r.Host)] {
		return true
	}
	if skipHosts[hostKey(r.Header.Get("X-Forwarded-Host"))] {
		return true
	}
	if skipHosts[originHost(r.Header.Get("Origin"))] {
		return true
	}
	if skipHosts[originHost(r.Header.Get("Referer"))] {
		return true
	}
	return false
}

func originHost(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") && !strings.Contains(raw, "/") {
		return hostKey(raw)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return hostKey(u.Host)
}

func hostKey(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	if i := strings.Index(host, ","); i >= 0 {
		host = strings.TrimSpace(host[:i])
	}
	return host
}
