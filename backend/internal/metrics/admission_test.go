package metrics

import (
	"github.com/fix-gateway/backend/internal/ratelimit"
	"github.com/prometheus/client_golang/prometheus"
	"testing"
)

func TestAdmissionMetricContract(t *testing.T) {
	a := ratelimit.NewAdmission()
	p := ratelimit.Policy{Scope: "broker", Subject: "123", Operation: "Trading", Rate: 1, Burst: 1, Concurrent: 1, Enabled: true}
	a.Replace([]ratelimit.Policy{p})
	RegisterAdmission(a)
	release, r := a.Acquire("broker", "123", "Trading", "SubmitOrder")
	if r != nil {
		t.Fatal(r)
	}
	release()
	if _, r = a.Acquire("broker", "123", "Trading", "SubmitOrder"); r == nil {
		t.Fatal("expected denial")
	}
	p.Rate = 10
	a.Replace([]ratelimit.Policy{p})
	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]float64{}
	for _, family := range families {
		for _, m := range family.Metric {
			values[family.GetName()] += m.GetCounter().GetValue() + m.GetGauge().GetValue()
		}
	}
	for name, want := range map[string]float64{"grpc_rate_limit_requests_total": 2, "grpc_rate_limit_allowed_total": 1, "grpc_rate_limit_rejected_total": 1, "grpc_rate_limit_rejected_by_broker_total": 1, "grpc_rate_limit_configured_rps": 10, "grpc_rate_limit_concurrent": 0} {
		if values[name] != want {
			t.Fatal(name, values[name], want)
		}
	}
	foundLatency := false
	for _, family := range families {
		if family.GetName() == "grpc_rate_limit_latency_seconds" {
			foundLatency = true
			if family.Metric[0].Histogram.GetSampleCount() != 2 {
				t.Fatal("missing latency observations")
			}
		}
	}
	if !foundLatency {
		t.Fatal("missing histogram")
	}
}
