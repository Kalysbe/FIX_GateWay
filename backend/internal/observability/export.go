package observability

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/google/uuid"
)

func (p *Pipeline) StartExport(ctx context.Context, adminID, format string, f Filter) (ExportJob, error) {
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
		return job, err
	}
	go p.runExport(job, f)
	return job, nil
}

func (p *Pipeline) runExport(job ExportJob, f Filter) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	job.Status = "RUNNING"
	_ = p.store.UpdateExportJob(ctx, job)

	dir := filepath.Join(os.TempDir(), "fix-gateway-exports")
	_ = os.MkdirAll(dir, 0o700)
	path := filepath.Join(dir, job.ID.String()+"."+job.Format)
	file, err := os.Create(path)
	if err != nil {
		job.Status = "FAILED"
		job.Error = err.Error()
		now := time.Now().UTC()
		job.CompletedAt = &now
		_ = p.store.UpdateExportJob(ctx, job)
		return
	}
	defer file.Close()

	f.Limit = 200
	f.CursorTS = time.Time{}
	f.CursorID = uuid.Nil
	n := 0
	max := 100000
	var w *csv.Writer
	if job.Format == "csv" {
		w = csv.NewWriter(file)
		_ = w.Write([]string{"timestamp", "severity", "type", "subtype", "broker_id", "broker_name", "client_ip", "http_method", "endpoint", "status_code", "duration_ms", "request_id", "order_id", "result", "error_code", "message"})
	} else {
		_, _ = file.WriteString("[\n")
	}
	first := true
	for n < max {
		list, more, err := p.store.List(ctx, f)
		if err != nil {
			job.Status = "FAILED"
			job.Error = err.Error()
			now := time.Now().UTC()
			job.CompletedAt = &now
			_ = p.store.UpdateExportJob(ctx, job)
			return
		}
		for _, e := range list {
			n++
			if job.Format == "csv" {
				_ = w.Write(csvRow(e))
			} else {
				if !first {
					_, _ = file.WriteString(",\n")
				}
				b, _ := json.Marshal(e)
				_, _ = file.Write(b)
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
	} else {
		_, _ = file.WriteString("\n]\n")
	}
	now := time.Now().UTC()
	job.Status = "DONE"
	job.FilePath = path
	job.RowCount = &n
	job.CompletedAt = &now
	_ = p.store.UpdateExportJob(ctx, job)
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
	return []string{
		e.Timestamp.Format(time.RFC3339Nano), e.Severity, e.Type, e.Subtype, bid, e.BrokerName, e.ClientIP,
		e.HTTPMethod, e.Endpoint, sc, ms, e.RequestID, oid, e.Result, e.ErrorCode, e.Message,
	}
}

func (p *Pipeline) WriteSyncExport(ctx context.Context, format string, f Filter, dest io.Writer) (int, error) {
	f.Limit = 200
	n := 0
	max := p.settings.ExportMaxSync
	if max <= 0 {
		max = 5000
	}
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
					_, _ = dest.Write([]byte(",\n"))
				}
				b, _ := json.Marshal(e)
				_, _ = dest.Write(b)
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
