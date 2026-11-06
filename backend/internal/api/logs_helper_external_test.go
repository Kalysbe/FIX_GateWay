package api_test

import (
	"testing"

	"github.com/fix-gateway/backend/internal/observability"
)

// testLogFiles is a fresh daily-log directory for one test.
func testLogFiles(t testing.TB) *observability.FileLog {
	t.Helper()
	logs, err := observability.OpenFileLog(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logs.Close() })
	return logs
}
