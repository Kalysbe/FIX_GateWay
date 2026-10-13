package observability

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

var ErrExportBusy = errors.New("export capacity exhausted")

const exportFileLimit int64 = 128 << 20
const exportDiskLimit int64 = 1 << 30
const exportTTL = 24 * time.Hour

func (p *Pipeline) StartExport(ctx context.Context, adminID, format string, f Filter) (ExportJob, error) {
	if err := p.reserveExport(); err != nil {
		return ExportJob{}, err
	}
	if format != "json" {
		format = "csv"
	}
	job := ExportJob{
		ID: uuid.New(), AdminUserID: adminID, Format: format, Status: "PENDING",
		CreatedAt: time.Now().UTC(),
	}
	raw, _ := json.Marshal(map[string]any{
		"from": f.From, "to": f.To, "broker_id": f.BrokerID, "type": f.Type, "subtype": f.Subtype,
		"severity": f.Severity, "request_id": f.RequestID, "order_id": f.OrderID, "client_ip": f.ClientIP,
	})
	job.Filter = raw
	if err := p.store.CreateExportJob(ctx, job); err != nil {
		p.releaseExport()
		return job, err
	}
	go p.runExport(job, f)
	return job, nil
}

func (p *Pipeline) InitExports(ctx context.Context) error {
	if p.settings.ExportDir == "" {
		p.settings.ExportDir = filepath.Join(os.TempDir(), "fix-gateway-exports")
	}
	if err := os.MkdirAll(p.settings.ExportDir, 0700); err != nil {
		return err
	}
	if _, err := p.store.pool.Exec(ctx, "UPDATE obs_export_jobs SET status='FAILED',error='interrupted by gateway restart; retry export',completed_at=now(),file_path=NULL WHERE status IN('PENDING','RUNNING')"); err != nil {
		return err
	}
	p.exportCtx = ctx
	if err := p.cleanupExports(ctx, true); err != nil {
		return err
	}
	go func() {
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
			work, cancel := context.WithTimeout(ctx, 5*time.Second)
			if err := p.cleanupExports(work, false); err != nil && p.log != nil {
				p.log.Warn("export cleanup failed", "error", err)
			}
			cancel()
		}
	}()
	return nil
}
func (p *Pipeline) cleanupExports(ctx context.Context, startup bool) error {
	p.exportMu.Lock()
	defer p.exportMu.Unlock()
	entries, err := os.ReadDir(p.settings.ExportDir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		name := e.Name()
		idPart, ext, ok := strings.Cut(name, ".")
		id, err := uuid.Parse(idPart)
		if err != nil || !ok || e.IsDir() || (ext != "csv" && ext != "json" && ext != "tmp") {
			continue
		}
		st, err := e.Info()
		if err != nil {
			return err
		}
		if (!startup || ext != "tmp") && time.Since(st.ModTime()) < exportTTL {
			continue
		}
		if err := os.Remove(filepath.Join(p.settings.ExportDir, name)); err != nil {
			return err
		}
		if ext != "tmp" {
			if _, err := p.store.pool.Exec(ctx, "UPDATE obs_export_jobs SET status='EXPIRED',file_path=NULL WHERE id=$1 AND status='DONE'", id); err != nil {
				return err
			}
		}
	}
	return nil
}
func (p *Pipeline) reserveExport() error {
	p.exportMu.Lock()
	defer p.exportMu.Unlock()
	if p.exportCtx == nil || p.exportCtx.Err() != nil {
		return ErrExportBusy
	}
	entries, err := os.ReadDir(p.settings.ExportDir)
	if err != nil {
		return err
	}
	var total int64
	for _, e := range entries {
		st, err := e.Info()
		if err != nil {
			return err
		}
		total += st.Size()
	}
	if len(entries)+len(p.exportSlots) >= 32 || total+p.exportReserved+exportFileLimit > exportDiskLimit {
		return ErrExportBusy
	}
	select {
	case p.exportSlots <- struct{}{}:
		p.exportReserved += exportFileLimit
		return nil
	default:
		return ErrExportBusy
	}
}
func (p *Pipeline) releaseExport() {
	p.exportMu.Lock()
	p.exportReserved -= exportFileLimit
	<-p.exportSlots
	p.exportMu.Unlock()
}

type limitedExportWriter struct {
	dest      io.Writer
	remaining int64
}

func (w *limitedExportWriter) Write(b []byte) (int, error) {
	if int64(len(b)) > w.remaining {
		return 0, errors.New("export file quota exceeded")
	}
	n, err := w.dest.Write(b)
	w.remaining -= int64(n)
	if err == nil && n != len(b) {
		err = io.ErrShortWrite
	}
	return n, err
}
func (p *Pipeline) runExport(job ExportJob, f Filter) {
	defer p.releaseExport()
	ctx, cancel := context.WithTimeout(p.exportCtx, 5*time.Minute)
	defer cancel()
	job.Status = "RUNNING"
	path := filepath.Join(p.settings.ExportDir, job.ID.String()+"."+job.Format)
	tmp := filepath.Join(p.settings.ExportDir, job.ID.String()+".tmp")
	defer os.Remove(tmp)
	err := p.store.UpdateExportJob(ctx, job)
	n := 0
	if err == nil {
		var file *os.File
		file, err = os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err == nil {
			n, err = p.writeExport(ctx, job.Format, f, &limitedExportWriter{dest: file, remaining: exportFileLimit}, 100000)
			if err == nil {
				err = file.Sync()
			}
			err = errors.Join(err, file.Close())
			if err == nil {
				err = os.Rename(tmp, path)
			}
		}
	}
	now := time.Now().UTC()
	job.CompletedAt = &now
	if err != nil {
		job.Status = "FAILED"
		job.Error = "export failed; retry or contact operator"
		if p.log != nil {
			p.log.Error("export failed", "job_id", job.ID, "error", err)
		}
	} else {
		job.Status = "DONE"
		job.FilePath = path
		job.RowCount = &n
	}
	persist, done := context.WithTimeout(context.Background(), 5*time.Second)
	defer done()
	if err := p.store.UpdateExportJob(persist, job); err != nil {
		_ = os.Remove(path)
		if p.log != nil {
			p.log.Error("export result persistence failed", "job_id", job.ID, "error", err)
		}
	}
}

func csvRow(e Event) []string {
	bid, sc, ms, oid := "", "", "", ""
	if e.BrokerID != nil {
		bid = strconv.FormatInt(*e.BrokerID, 10)
	}
	if e.StatusCode != nil {
		sc = strconv.Itoa(*e.StatusCode)
	}
	if e.ResponseTimeMs != nil {
		ms = strconv.Itoa(*e.ResponseTimeMs)
	}
	if e.OrderID != nil {
		oid = e.OrderID.String()
	}
	row := []string{
		e.Timestamp.Format(time.RFC3339Nano), e.Severity, e.Type, e.Subtype, bid, e.BrokerName, e.ClientIP,
		e.HTTPMethod, e.Endpoint, sc, ms, e.RequestID, oid, e.Result, e.ErrorCode, e.Message,
	}
	for i, value := range row {
		trimmed := strings.TrimLeft(value, " \t\r\n")
		if trimmed != "" && strings.ContainsRune("=+-@", rune(trimmed[0])) {
			row[i] = "'" + value
		}
	}
	return row
}

func (p *Pipeline) WriteSyncExport(ctx context.Context, format string, f Filter, dest io.Writer) (int, error) {
	max := p.settings.ExportMaxSync
	if max <= 0 {
		max = 5000
	}
	return p.writeExport(ctx, format, f, dest, max)
}
func (p *Pipeline) writeExport(ctx context.Context, format string, f Filter, dest io.Writer, max int) (int, error) {
	f.Limit = 200
	f.CursorTS = time.Time{}
	f.CursorID = uuid.Nil
	n := 0
	var w *csv.Writer
	if format == "csv" {
		w = csv.NewWriter(dest)
		if err := w.Write([]string{"timestamp", "severity", "type", "subtype", "broker_id", "broker_name", "client_ip", "http_method", "endpoint", "status_code", "duration_ms", "request_id", "order_id", "result", "error_code", "message"}); err != nil {
			return 0, err
		}
	} else {
		if _, err := dest.Write([]byte("[\n")); err != nil {
			return 0, err
		}
	}
	first := true
	for n < max {
		list, more, err := p.store.List(ctx, f)
		if err != nil {
			return n, err
		}
		for _, e := range list {
			if n >= max {
				break
			}
			n++
			if format == "csv" {
				if err := w.Write(csvRow(e)); err != nil {
					return n, err
				}
			} else {
				if !first {
					if _, err := dest.Write([]byte(",\n")); err != nil {
						return n, err
					}
				}
				b, err := json.Marshal(e)
				if err != nil {
					return n, err
				}
				if _, err = dest.Write(b); err != nil {
					return n, err
				}
				first = false
			}
		}
		if !more || len(list) == 0 {
			break
		}
		last := list[len(list)-1]
		f.CursorTS = last.Timestamp
		f.CursorID = last.ID
	}
	if w != nil {
		w.Flush()
		return n, w.Error()
	}
	_, err := dest.Write([]byte("\n]\n"))
	return n, err
}

func ExportFilename(format string) string {
	return fmt.Sprintf("logs-%s.%s", time.Now().UTC().Format("20060102-150405"), format)
}
