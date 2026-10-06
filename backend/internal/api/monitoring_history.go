package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type monitoringPoint struct {
	Time  string   `json:"time"`
	Value *float64 `json:"value"`
}
type monitoringHistory struct {
	GeneratedAt time.Time                    `json:"generated_at"`
	Series      map[string][]monitoringPoint `json:"series"`
	Adapters    map[string][]monitoringPoint `json:"adapters"`
}

func (s *Server) monitoringHistory(w http.ResponseWriter, r *http.Request) {
	data, err := s.monitoringCache.get(r.Context(), 15*time.Second, s.loadMonitoringHistory)
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, "История метрик недоступна: проверьте Prometheus")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, data)
}

func (s *Server) loadMonitoringHistory(ctx context.Context) (any, error) {
	if s.cfg.PrometheusURL == "" {
		return nil, fmt.Errorf("PROMETHEUS_URL is not configured")
	}
	// Preserve instance filtering: unrelated scrape jobs must never enter this dashboard.
	expressions := []string{
		`label_replace(100 * sum(rate(process_cpu_seconds_total{job="fix-gateway"}[1m])), "series", "cpu_pct", "", "")`,
		`label_replace(sum(process_resident_memory_bytes{job="fix-gateway"}) / 1048576, "series", "rss_mb", "", "")`,
		`label_replace(sum(rate(gateway_http_requests_total{job="fix-gateway"}[1m])), "series", "http_rps", "", "")`,
		`label_replace(sum(orders_queue_size{job="fix-gateway"}), "series", "queue", "", "")`,
		`label_replace(sum by(adapter_id)(rate(adapter_requests_total{job="fix-gateway"}[1m])), "series", "adapter_rps", "", "")`,
	}
	end := time.Now().UTC().Truncate(15 * time.Second)
	params := url.Values{"query": {strings.Join(expressions, " or ")}, "start": {strconv.FormatInt(end.Add(-10*time.Minute).Unix(), 10)}, "end": {strconv.FormatInt(end.Unix(), 10)}, "step": {"15"}, "timeout": {"3s"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(s.cfg.PrometheusURL, "/")+"/api/v1/query_range?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	client := http.Client{Timeout: 4 * time.Second}
	response, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("prometheus status %d", response.StatusCode)
	}
	var result struct {
		Status string `json:"status"`
		Data   struct {
			Result []struct {
				Metric map[string]string   `json:"metric"`
				Values [][]json.RawMessage `json:"values"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&result); err != nil {
		return nil, err
	}
	if result.Status != "success" {
		return nil, fmt.Errorf("prometheus query failed")
	}
	history := monitoringHistory{GeneratedAt: end, Series: map[string][]monitoringPoint{}, Adapters: map[string][]monitoringPoint{}}
	for _, series := range result.Data.Result {
		points := make([]monitoringPoint, 0, len(series.Values))
		for _, pair := range series.Values {
			if len(pair) != 2 {
				return nil, fmt.Errorf("invalid prometheus point")
			}
			var timestamp float64
			var raw string
			if err := json.Unmarshal(pair[0], &timestamp); err != nil {
				return nil, err
			}
			if err := json.Unmarshal(pair[1], &raw); err != nil {
				return nil, err
			}
			value, err := strconv.ParseFloat(raw, 64)
			point := monitoringPoint{Time: time.UnixMilli(int64(timestamp * 1000)).UTC().Format(time.RFC3339Nano)}
			if err == nil && !math.IsNaN(value) && !math.IsInf(value, 0) {
				point.Value = &value
			}
			points = append(points, point)
		}
		if series.Metric["series"] == "adapter_rps" {
			history.Adapters[series.Metric["adapter_id"]] = points
		} else {
			history.Series[series.Metric["series"]] = points
		}
	}
	return history, nil
}
