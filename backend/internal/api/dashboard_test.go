package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fix-gateway/backend/internal/config"
)

func TestDashboardMissingSnapshotIsPending(t *testing.T) {
	for _, raw := range [][]byte{nil, {}, []byte("null")} {
		if _, err := decodeDashboardSnapshot(raw); !errors.Is(err, errDashboardPending) {
			t.Fatalf("missing snapshot error = %v", err)
		}
	}
	snapshot, err := decodeDashboardSnapshot([]byte(`{"stats":{"total":7},"stale":true}`))
	if err != nil || snapshot["stale"] != true {
		t.Fatalf("stale snapshot was discarded: %v, %v", snapshot, err)
	}
}

func TestDashboardPendingReturnsRetryableResponse(t *testing.T) {
	server := Server{dashboardCache: snapshotCache{err: errDashboardPending, retryAfter: time.Now().Add(time.Minute)}}
	response := httptest.NewRecorder()
	server.dashboard(response, httptest.NewRequest(http.MethodGet, "/api/v1/admin/dashboard", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("pending status = %d: %s", response.Code, response.Body.String())
	}
}

func TestSnapshotCacheSharesRefreshAndServesStale(t *testing.T) {
	var cache snapshotCache
	var calls atomic.Int32
	release := make(chan struct{})
	started := make(chan struct{}, 2)
	load := func(ctx context.Context) (any, error) {
		calls.Add(1)
		started <- struct{}{}
		select {
		case <-release:
			return "snapshot", nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	done := make(chan error, 8)
	for i := 0; i < 8; i++ {
		go func() {
			data, err := cache.get(context.Background(), time.Second, load)
			if err == nil && data != "snapshot" {
				err = fmt.Errorf("invalid data")
			}
			done <- err
		}()
	}
	<-started
	close(release)
	for i := 0; i < 8; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("refreshes = %d", calls.Load())
	}
	cache.mu.Lock()
	cache.updated = time.Now().Add(-2 * time.Second)
	cache.retryAfter = time.Time{}
	cache.mu.Unlock()
	release = make(chan struct{})
	data, err := cache.get(context.Background(), time.Second, load)
	if err != nil || data != "snapshot" {
		t.Fatalf("stale snapshot = %v, %v", data, err)
	}
	<-started
	close(release)
	cache.mu.Lock()
	pending := cache.pending
	cache.mu.Unlock()
	if pending != nil {
		<-pending
	}
}

func TestSnapshotCacheDoesNotCacheFailures(t *testing.T) {
	var cache snapshotCache
	_, err := cache.get(context.Background(), time.Second, func(context.Context) (any, error) { return nil, fmt.Errorf("database unavailable") })
	if err == nil {
		t.Fatal("failure was hidden")
	}
	cache.mu.Lock()
	cache.retryAfter = time.Time{}
	cache.mu.Unlock()
	data, err := cache.get(context.Background(), time.Second, func(context.Context) (any, error) { return "recovered", nil })
	if err != nil || data != "recovered" {
		t.Fatalf("recovery = %v, %v", data, err)
	}
}

func TestSnapshotCacheCallerCancellationDoesNotCancelSharedRefresh(t *testing.T) {
	var cache snapshotCache
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	load := func(ctx context.Context) (any, error) {
		close(started)
		select {
		case <-release:
			return "shared", nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	go func() { _, err := cache.get(ctx, time.Second, load); done <- err }()
	<-started
	cancel()
	if err := <-done; err != context.Canceled {
		t.Fatalf("cancel = %v", err)
	}
	close(release)
	data, err := cache.get(context.Background(), time.Second, load)
	if err != nil || data != "shared" {
		t.Fatalf("shared refresh = %v, %v", data, err)
	}
}

func TestMonitoringHistoryParsesRatesAndGaps(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/query_range" || r.URL.Query().Get("step") != "15" {
			t.Errorf("unexpected request %s", r.URL)
		}
		fmt.Fprint(w, `{"status":"success","data":{"result":[{"metric":{"series":"cpu_pct"},"values":[[1700000000,"12.5"],[1700000015,"NaN"]]},{"metric":{"series":"adapter_rps","adapter_id":"a1"},"values":[[1700000000,"2"]]}]}}`)
	}))
	defer upstream.Close()
	server := Server{cfg: config.Config{PrometheusURL: upstream.URL}}
	data, err := server.loadMonitoringHistory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	history := data.(monitoringHistory)
	if *history.Series["cpu_pct"][0].Value != 12.5 || history.Series["cpu_pct"][1].Value != nil || *history.Adapters["a1"][0].Value != 2 {
		t.Fatalf("invalid history: %+v", history)
	}
}
