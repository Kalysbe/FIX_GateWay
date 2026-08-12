package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
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

func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := &statusWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(ww, r)
		path := r.URL.Path
		HTTPRequests.WithLabelValues(r.Method, path, strconv.Itoa(ww.status)).Inc()
		HTTPDuration.WithLabelValues(r.Method, path).Observe(time.Since(start).Seconds())
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
