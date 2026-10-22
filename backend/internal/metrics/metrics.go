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
	FIXAuditBytes                     = promauto.NewGaugeVec(prometheus.GaugeOpts{Name: "gateway_fix_audit_bytes", Help: "Persisted FIX transport audit bytes, retained until operator archives them"}, []string{"adapter_id"})
	FIXAuditCapacity                  = promauto.NewGaugeVec(prometheus.GaugeOpts{Name: "gateway_fix_audit_capacity_bytes", Help: "Maximum retained FIX transport audit bytes per adapter"}, []string{"adapter_id"})
	FIXAuditFailures                  = promauto.NewCounterVec(prometheus.CounterOpts{Name: "gateway_fix_audit_failures_total", Help: "Fatal FIX transport audit persistence failures"}, []string{"adapter_id"})
	TradeReconciliationEnabled        = promauto.NewGauge(prometheus.GaugeOpts{Name: "gateway_trade_reconciliation_enabled", Help: "Whether the configured QUIK trade reconciliation worker is enabled"})
	TradeLastSuccess                  = promauto.NewGauge(prometheus.GaugeOpts{Name: "gateway_trade_last_success_timestamp_seconds", Help: "Last complete successful QUIK snapshot verification, Unix time"})
	TradeIncomplete                   = promauto.NewGauge(prometheus.GaugeOpts{Name: "gateway_trade_incomplete_total", Help: "Trades missing a real side for more than two minutes"})
	TradeReportErrors                 = promauto.NewGauge(prometheus.GaugeOpts{Name: "gateway_trade_report_errors", Help: "Persisted source records requiring reconciliation"})
	TradeSyncFailed                   = promauto.NewGauge(prometheus.GaugeOpts{Name: "gateway_trade_sync_failed", Help: "Whether the latest completed QUIK trade reconciliation failed"})
	TradeMonitorFailed                = promauto.NewGauge(prometheus.GaugeOpts{Name: "gateway_trade_monitor_failed", Help: "Whether trade integrity checks cannot read their database state"})
	DatabaseAvailable                 = promauto.NewGauge(prometheus.GaugeOpts{Name: "gateway_database_available", Help: "Whether the private metrics probe can reach PostgreSQL"})
	FIXAvailable                      = promauto.NewGauge(prometheus.GaugeOpts{Name: "gateway_fix_available_sessions", Help: "Runtime FIX sessions available for routing"})
	BackupAge                         = promauto.NewGauge(prometheus.GaugeOpts{Name: "gateway_backup_age_seconds", Help: "Age of latest successful encrypted database backup; configured production backups only"})
	ExecutionInboxPending             = promauto.NewGauge(prometheus.GaugeOpts{Name: "gateway_execution_inbox_pending", Help: "Durable incoming FIX reports waiting for DB commit"})
	ExecutionInboxBytes               = promauto.NewGauge(prometheus.GaugeOpts{Name: "gateway_execution_inbox_bytes", Help: "Pending incoming FIX journal size"})
	ExecutionInboxOldest              = promauto.NewGauge(prometheus.GaugeOpts{Name: "gateway_execution_inbox_oldest_seconds", Help: "Age of oldest unapplied incoming FIX report"})
	ExecutionInboxUnhealthy           = promauto.NewGauge(prometheus.GaugeOpts{Name: "gateway_execution_inbox_unhealthy", Help: "Incoming execution recovery pauses new trading"})
	ExecutionInboxApplyFailures       = promauto.NewCounter(prometheus.CounterOpts{Name: "gateway_execution_inbox_apply_failures_total", Help: "Failed attempts to apply a retained incoming report"})
	ExecutionInboxPersistenceFailures = promauto.NewCounter(prometheus.CounterOpts{Name: "gateway_execution_inbox_persistence_failures_total", Help: "Fatal failures to durably accept an incoming FIX report"})
	ObsQueueSize                      = promauto.NewGauge(prometheus.GaugeOpts{
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
