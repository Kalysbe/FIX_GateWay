package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestMonitoringSnapshot(t *testing.T) {
	w := httptest.NewRecorder()
	(&Server{}).monitoring(w, httptest.NewRequest(http.MethodGet, "/api/v1/admin/monitoring", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	var snapshot struct {
		Timestamp  time.Time          `json:"timestamp"`
		Counters   map[string]float64 `json:"counters"`
		Heap       uint64             `json:"heap_bytes"`
		Goroutines int                `json:"goroutines"`
		Cores      int                `json:"cpu_cores"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Timestamp.IsZero() || snapshot.Heap == 0 || snapshot.Goroutines < 1 || snapshot.Cores < 1 {
		t.Fatalf("invalid runtime snapshot: %+v", snapshot)
	}
	if _, ok := snapshot.Counters["orders_queue_size"]; !ok {
		t.Fatal("queue gauge missing")
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("monitoring snapshots must not be cached")
	}
}
