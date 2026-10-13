package observability

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

type shortExportWriter struct{}

func (shortExportWriter) Write(b []byte) (int, error) { return len(b) - 1, nil }
func TestExportLimitsAndWriteFailures(t *testing.T) {
	var out bytes.Buffer
	w := &limitedExportWriter{dest: &out, remaining: 3}
	if _, err := w.Write([]byte("abc")); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("d")); err == nil || out.String() != "abc" {
		t.Fatal("quota discarded existing content or accepted overflow")
	}
	if _, err := (&limitedExportWriter{dest: shortExportWriter{}, remaining: 10}).Write([]byte("abc")); !errors.Is(err, io.ErrShortWrite) {
		t.Fatal(err)
	}
	p := NewPipeline(nil, nil, nil, DefaultSettings())
	p.settings.ExportDir = t.TempDir()
	p.exportCtx = context.Background()
	if err := p.reserveExport(); err != nil {
		t.Fatal(err)
	}
	if err := p.reserveExport(); err != nil {
		t.Fatal(err)
	}
	if err := p.reserveExport(); !errors.Is(err, ErrExportBusy) {
		t.Fatal("unbounded export concurrency")
	}
	p.releaseExport()
	p.releaseExport()
	for n := 0; n < 32; n++ {
		f, err := os.CreateTemp(p.settings.ExportDir, "quota-")
		if err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
	if err := p.reserveExport(); !errors.Is(err, ErrExportBusy) {
		t.Fatal("unbounded file count")
	}
	if _, err := os.Stat(filepath.Join(p.settings.ExportDir, "missing")); !os.IsNotExist(err) {
		t.Fatal(err)
	}
}
