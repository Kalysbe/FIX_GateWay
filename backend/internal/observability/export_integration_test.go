//go:build integration

package observability

import (
	"context"
	"encoding/json"
	"github.com/fix-gateway/backend/internal/platform/migrate"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestExportRestartRecoveryAndFileExpiry(t *testing.T) {
	dsn := os.Getenv("SECURITY_AUDIT_DATABASE_URL")
	if dsn == "" {
		t.Skip("disposable security database required")
	}
	if err := migrate.Up(dsn, "../../migrations"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	store := NewStore(pool)
	if err := store.EnsurePartitions(ctx); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	stale := ExportJob{ID: uuid.New(), Format: "json", Status: "RUNNING", Filter: json.RawMessage(`{}`)}
	if err := store.CreateExportJob(ctx, stale); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, stale.ID.String()+".tmp"), []byte("partial"), 0600); err != nil {
		t.Fatal(err)
	}
	settings := DefaultSettings()
	settings.ExportDir = dir
	p := NewPipeline(store, nil, nil, settings)
	if err := p.InitExports(ctx); err != nil {
		t.Fatal(err)
	}
	recovered, err := store.GetExportJob(ctx, stale.ID)
	if err != nil || recovered.Status != "FAILED" {
		t.Fatalf("interrupted job not recovered: %+v %v", recovered, err)
	}
	if _, err := os.Stat(filepath.Join(dir, stale.ID.String()+".tmp")); !os.IsNotExist(err) {
		t.Fatal("orphaned partial export retained")
	}
	job, err := p.StartExport(ctx, "", "json", Filter{From: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		job, err = store.GetExportJob(ctx, job.ID)
		if err != nil {
			t.Fatal(err)
		}
		if job.Status == "DONE" || job.Status == "FAILED" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if job.Status != "DONE" {
		t.Fatalf("export did not complete: %+v", job)
	}
	b, err := os.ReadFile(job.FilePath)
	if err != nil || !json.Valid(b) {
		t.Fatalf("partial or missing DONE export: %s %v", b, err)
	}
	old := time.Now().Add(-25 * time.Hour)
	if err := os.Chtimes(job.FilePath, old, old); err != nil {
		t.Fatal(err)
	}
	if err := p.cleanupExports(ctx, false); err != nil {
		t.Fatal(err)
	}
	expired, err := store.GetExportJob(ctx, job.ID)
	if err != nil || expired.Status != "EXPIRED" || expired.FilePath != "" {
		t.Fatalf("expired export not recorded: %+v %v", expired, err)
	}
	if _, err := os.Stat(job.FilePath); !os.IsNotExist(err) {
		t.Fatal("expired file remains on disk")
	}
}
