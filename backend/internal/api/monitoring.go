package api

import (
	"net/http"
	"runtime"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// monitoring exposes process counters through the authenticated admin API.
// Rates are calculated from consecutive samples, never from daily totals.
func (s *Server) monitoring(w http.ResponseWriter, r *http.Request) {
	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		s.writeInternal(w, err)
		return
	}
	values := map[string]float64{}
	adapterValues := map[string]map[string]float64{}
	for _, family := range families {
		name := family.GetName()
		for _, metric := range family.Metric {
			value := metric.GetCounter().GetValue() + metric.GetGauge().GetValue()
			switch name {
			case "gateway_http_requests_total", "orders_received_total", "orders_processed_total", "orders_rejected_total", "orders_queue_size", "process_cpu_seconds_total", "process_resident_memory_bytes":
				values[name] += value
			case "adapter_requests_total", "adapter_errors_total", "adapter_active_orders":
				for _, label := range metric.Label {
					if label.GetName() == "adapter_id" {
						id := label.GetValue()
						if adapterValues[id] == nil {
							adapterValues[id] = map[string]float64{}
						}
						adapterValues[id][name] = value
					}
				}
			}
		}
	}
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	var database any
	if s.pool != nil {
		st := s.pool.Stat()
		database = map[string]any{"acquired": st.AcquiredConns(), "total": st.TotalConns(), "max": st.MaxConns()}
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{
		"timestamp": time.Now().UTC(), "counters": values, "adapters": adapterValues,
		"heap_bytes": mem.HeapAlloc, "goroutines": runtime.NumGoroutine(), "cpu_cores": runtime.NumCPU(),
		"database": database,
	})
}
