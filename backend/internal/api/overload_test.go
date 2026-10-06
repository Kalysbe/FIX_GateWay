package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fix-gateway/backend/internal/config"
)

func TestOverloadReservesTradingAndReleasesCapacity(t *testing.T) {
	s := &Server{cfg: config.Config{APIReadConcurrency: 1, APITradeConcurrency: 1}}
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	h := s.protectOverload(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/orders" && r.Method == http.MethodGet && r.Header.Get("Hold") != "" {
			close(entered)
			<-release
		}
		w.WriteHeader(http.StatusOK)
	}))
	go func() {
		defer close(done)
		r := httptest.NewRequest(http.MethodGet, "/api/v1/orders", nil)
		r.Header.Set("Hold", "yes")
		h.ServeHTTP(httptest.NewRecorder(), r)
	}()
	<-entered
	for _, test := range []struct {
		method, path string
		status       int
	}{
		{http.MethodGet, "/api/v1/orders", 503},
		{http.MethodPost, "/api/v1/orders", 200},
		{http.MethodPost, "/api/v1/orders/id/cancel", 200},
		{http.MethodGet, "/health", 200},
		{http.MethodGet, "/ready", 200},
		{http.MethodGet, "/api/v1/admin/events/stream", 200},
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(test.method, test.path, nil))
		if w.Code != test.status {
			t.Fatalf("%s %s: got %d", test.method, test.path, w.Code)
		}
		if w.Code == 503 && w.Header().Get("Retry-After") != "1" {
			t.Fatal("missing retry guidance")
		}
	}
	close(release)
	<-done
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/orders", nil))
	if w.Code != 200 {
		t.Fatal("capacity not released")
	}
}

func TestRequestDeadlineCancelsWorkAndLeavesStreamUntouched(t *testing.T) {
	s := &Server{cfg: config.Config{APIRequestTimeout: 10 * time.Millisecond}}
	h := s.protectOverload(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/admin/events/stream" {
			if _, ok := r.Context().Deadline(); ok {
				t.Fatal("stream deadline added")
			}
			return
		}
		<-r.Context().Done()
		if r.Context().Err() != context.DeadlineExceeded {
			t.Fatal("unexpected cancellation")
		}
		s.writeInternal(w, r.Context().Err())
	}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/orders", nil))
	if w.Code != 503 || w.Header().Get("Retry-After") == "" {
		t.Fatal("deadline must be retryable")
	}
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/admin/events/stream", nil))
}

func TestRateLimitResponseHasRetryAfter(t *testing.T) {
	w := httptest.NewRecorder()
	writeErr(w, http.StatusTooManyRequests, "rate limit exceeded")
	if w.Header().Get("Retry-After") != "1" {
		t.Fatal("missing Retry-After")
	}
}
