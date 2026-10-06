package metrics

import (
	"bufio"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	ObsQueueSize = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "gateway_observability_queue_size",
		Help: "Diagnostic events waiting for batch persistence",
	})
	ObsDropped = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "gateway_observability_dropped_events_total",
		Help: "Diagnostic events or notification summaries lost by reason; not accepted orders",
	}, []string{"reason"})
	HTTPRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "gateway_http_requests_total",
		Help: "Total HTTP requests",
	}, []string{"method", "path", "status"})

	HTTPDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "gateway_http_request_duration_seconds",
		Help:    "HTTP request duration",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "path"})

	OrdersReceived = promauto.NewCounter(prometheus.CounterOpts{
		Name: "orders_received_total",
		Help: "Orders accepted by API",
	})

	OrdersProcessed = promauto.NewCounter(prometheus.CounterOpts{
		Name: "orders_processed_total",
		Help: "Orders sent to adapters",
	})

	OrdersRejected = promauto.NewCounter(prometheus.CounterOpts{
		Name: "orders_rejected_total",
		Help: "Orders rejected",
	})

	OutboxQueueSize = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "orders_queue_size",
		Help: "Pending/processing outbox events",
	})

	AdapterRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "adapter_requests_total",
		Help: "Adapter send attempts",
	}, []string{"adapter_id"})

	AdapterErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "adapter_errors_total",
		Help: "Adapter errors",
	}, []string{"adapter_id"})

	AdapterActive = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "adapter_active_orders",
		Help: "Active orders per adapter",
	}, []string{"adapter_id"})
)

var (
	uuidPath = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)
	idPath   = regexp.MustCompile(`/[0-9]+`)
)

func routeLabel(path string) string {
	path = uuidPath.ReplaceAllString(path, "/{id}")
	return idPath.ReplaceAllString(path, "/{id}")
}

func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := &statusWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(ww, r)
		path := "unmatched"
		if r.Pattern != "" {
			_, path, _ = strings.Cut(r.Pattern, " ")
		}
		method := r.Method
		switch method {
		case "GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "CONNECT", "TRACE":
		default:
			method = "OTHER"
		}
		HTTPRequests.WithLabelValues(method, path, strconv.Itoa(ww.status)).Inc()
		HTTPDuration.WithLabelValues(method, path).Observe(time.Since(start).Seconds())
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, rw, err := http.NewResponseController(w.ResponseWriter).Hijack()
	if err == nil {
		w.status = http.StatusSwitchingProtocols
	}
	return conn, rw, err
}

func (w *statusWriter) Flush() { _ = http.NewResponseController(w.ResponseWriter).Flush() }
