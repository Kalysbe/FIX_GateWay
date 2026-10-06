package metrics

import (
	"github.com/fix-gateway/backend/internal/ratelimit"
	"github.com/prometheus/client_golang/prometheus"
	"time"
)

type admissionCollector struct {
	admission *ratelimit.Admission
	desc      map[string]*prometheus.Desc
}

func RegisterAdmission(a *ratelimit.Admission) {
	hist := prometheus.NewHistogram(prometheus.HistogramOpts{Name: "grpc_rate_limit_latency_seconds", Help: "Admission decision overhead", Buckets: []float64{0.000001, 0.000005, 0.00001, 0.00005, 0.0001, 0.001, 0.01}})
	prometheus.MustRegister(hist)
	a.Latency = func(d time.Duration) { hist.Observe(d.Seconds()) }
	c := &admissionCollector{admission: a, desc: map[string]*prometheus.Desc{}}
	for _, name := range []string{"requests_total", "allowed_total", "rejected_total", "current_rps", "peak_rps", "configured_rps", "burst", "concurrent", "configured_concurrent", "enabled", "rejected_by_broker_total", "rejected_by_method_total", "concurrency_rejected_total", "rate_rejected_total"} {
		c.desc[name] = prometheus.NewDesc("grpc_rate_limit_"+name, "Gateway admission across REST and gRPC; local instance statistics", []string{"scope", "broker_id", "method"}, nil)
	}
	prometheus.MustRegister(c)
}
func (c *admissionCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range c.desc {
		ch <- d
	}
}
func (c *admissionCollector) Collect(ch chan<- prometheus.Metric) {
	for _, u := range c.admission.Snapshot() {
		values := map[string]float64{"requests_total": float64(u.Requests), "allowed_total": float64(u.Allowed), "rejected_total": float64(u.Rejected), "current_rps": float64(u.CurrentRPS), "peak_rps": float64(u.PeakRPS), "configured_rps": float64(u.Rate), "burst": float64(u.Burst), "concurrent": float64(u.Active), "configured_concurrent": float64(u.Concurrent), "concurrency_rejected_total": float64(u.ConcurrencyRejected), "rate_rejected_total": float64(u.RateRejected)}
		if u.Enabled {
			values["enabled"] = 1
		} else {
			values["enabled"] = 0
		}
		if u.Scope == "broker" {
			values["rejected_by_broker_total"] = float64(u.Rejected)
		}
		values["rejected_by_method_total"] = float64(u.Rejected)
		for name, value := range values {
			typ := prometheus.GaugeValue
			if len(name) > 6 && name[len(name)-6:] == "_total" {
				typ = prometheus.CounterValue
			}
			ch <- prometheus.MustNewConstMetric(c.desc[name], typ, value, u.Scope, u.Subject, u.Operation)
		}
	}
}
