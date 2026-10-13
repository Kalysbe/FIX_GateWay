// Package executioninbox durably accepts incoming FIX reports independently of
// PostgreSQL. A callback must not return to the FIX engine until Append succeeds.
package executioninbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fix-gateway/backend/internal/domain"
	"github.com/google/uuid"
)

const maxReportBytes = 1 << 20

type Entry struct {
	Checksum   string                 `json:"checksum,omitempty"`
	ID         uuid.UUID              `json:"id"`
	ReceivedAt time.Time              `json:"received_at"`
	Report     domain.ExecutionReport `json:"report"`
}

type pending struct {
	name     string
	size     int64
	received time.Time
}

type Status struct {
	Pending   int
	Bytes     int64
	OldestAge time.Duration
	Failed    bool
}

func (s Status) Healthy() bool { return !s.Failed && s.OldestAge < 30*time.Second }

type Inbox struct {
	dir        string
	lock       *os.File
	log        *slog.Logger
	mu         sync.Mutex
	queue      []pending
	bytes      int64
	maxBytes   int64
	maxEntries int
	lastStamp  int64
	failed     bool
	wake       chan struct{}
}

// Open acquires an OS lock for the lifetime of the process. The directory must
// reside on the same persistent, locally mounted volume as the FIX session store.
func Open(dir string, maxBytes int64, maxEntries int, log *slog.Logger) (*Inbox, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("execution inbox directory is required")
	}
	if maxBytes <= 0 || maxEntries <= 0 {
		return nil, errors.New("execution inbox limits must be positive")
	}
	if log == nil {
		log = slog.Default()
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(filepath.Join(dir, ".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := lockFile(lock); err != nil {
		lock.Close()
		return nil, fmt.Errorf("execution inbox already owned or unavailable: %w", err)
	}
	in := &Inbox{dir: dir, lock: lock, log: log, maxBytes: maxBytes, maxEntries: maxEntries, wake: make(chan struct{}, 1)}
	if err := in.load(); err != nil {
		in.Close()
		return nil, err
	}
	// Check write and sync capability before allowing any FIX session to log on.
	probe, err := os.CreateTemp(dir, ".probe-")
	if err != nil {
		in.Close()
		return nil, err
	}
	probeName := probe.Name()
	err = probe.Sync()
	closeErr := probe.Close()
	removeErr := os.Remove(probeName)
	if err = errors.Join(err, closeErr, removeErr, syncDirectory(dir)); err != nil {
		in.Close()
		return nil, err
	}
	return in, nil
}

func (in *Inbox) Close() error { return in.lock.Close() }

func (in *Inbox) load() error {
	files, err := os.ReadDir(in.dir)
	if err != nil {
		return err
	}
	for _, f := range files {
		if strings.HasPrefix(f.Name(), ".") && strings.HasSuffix(f.Name(), ".tmp") {
			if _, err := uuid.Parse(strings.TrimSuffix(strings.TrimPrefix(f.Name(), "."), ".tmp")); err == nil {
				if err := os.Remove(filepath.Join(in.dir, f.Name())); err != nil {
					return err
				}
			}
			continue
		}
		if !strings.HasSuffix(f.Name(), ".json") {
			continue
		}
		if !f.Type().IsRegular() {
			return fmt.Errorf("non-regular execution inbox entry %s", f.Name())
		}
		e, size, err := in.read(f.Name())
		if err != nil {
			return err
		}
		in.queue = append(in.queue, pending{f.Name(), size, e.ReceivedAt})
		in.bytes += size
		if len(in.queue) > in.maxEntries || in.bytes > in.maxBytes {
			return errors.New("existing execution inbox exceeds configured capacity; reconcile before restart")
		}
		var stamp int64
		if _, err := fmt.Sscanf(f.Name(), "%d_", &stamp); err != nil {
			return fmt.Errorf("invalid inbox filename %s", f.Name())
		}
		if stamp > in.lastStamp {
			in.lastStamp = stamp
		}
	}
	sort.Slice(in.queue, func(i, j int) bool { return in.queue[i].name < in.queue[j].name })
	return nil
}

func (in *Inbox) read(name string) (Entry, int64, error) {
	var e Entry
	f, err := os.Open(filepath.Join(in.dir, name))
	if err != nil {
		return e, 0, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxReportBytes+1))
	if err != nil {
		return e, 0, err
	}
	if len(raw) > maxReportBytes {
		return e, 0, errors.New("execution inbox entry exceeds size limit")
	}
	if err = json.Unmarshal(raw, &e); err != nil {
		return e, 0, fmt.Errorf("invalid inbox entry %s: %w", name, err)
	}
	if e.ID == uuid.Nil || e.ReceivedAt.IsZero() || !strings.HasSuffix(name, "_"+e.ID.String()+".json") {
		return e, 0, fmt.Errorf("incomplete inbox entry %s", name)
	}
	if e.Checksum == "" || e.Checksum != entryChecksum(e) {
		return e, 0, fmt.Errorf("execution inbox checksum mismatch: %s", name)
	}
	return e, int64(len(raw)), nil
}

func entryChecksum(e Entry) string {
	e.Checksum = ""
	raw, _ := json.Marshal(e)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// Append fsyncs the content, atomically publishes the file and fsyncs the parent
// directory. PostgreSQL availability does not affect acceptance of the report.
func (in *Inbox) Append(report domain.ExecutionReport) error {
	in.mu.Lock()
	defer in.mu.Unlock()
	e := Entry{ID: uuid.New(), ReceivedAt: time.Now().UTC(), Report: report}
	e.Checksum = entryChecksum(e)
	raw, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if len(raw) > maxReportBytes {
		return errors.New("incoming execution report exceeds size limit")
	}
	if len(in.queue) >= in.maxEntries || in.bytes+int64(len(raw)) > in.maxBytes {
		return errors.New("execution inbox capacity exhausted")
	}
	stamp := e.ReceivedAt.UnixNano()
	if stamp <= in.lastStamp {
		stamp = in.lastStamp + 1
	}
	name := fmt.Sprintf("%020d_%s.json", stamp, e.ID)
	temp := filepath.Join(in.dir, "."+e.ID.String()+".tmp")
	f, err := os.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer os.Remove(temp)
	_, writeErr := f.Write(raw)
	syncErr := f.Sync()
	closeErr := f.Close()
	if err = errors.Join(writeErr, syncErr, closeErr); err != nil {
		return err
	}
	if err = publishFile(temp, filepath.Join(in.dir, name)); err != nil {
		return err
	}
	if err = syncDirectory(in.dir); err != nil {
		return err
	}
	in.lastStamp = stamp
	in.queue = append(in.queue, pending{name, int64(len(raw)), e.ReceivedAt})
	in.bytes += int64(len(raw))
	select {
	case in.wake <- struct{}{}:
	default:
	}
	return nil
}

func (in *Inbox) Status() Status {
	in.mu.Lock()
	defer in.mu.Unlock()
	s := Status{Pending: len(in.queue), Bytes: in.bytes, Failed: in.failed}
	if len(in.queue) > 0 {
		s.OldestAge = time.Since(in.queue[0].received)
		if s.OldestAge < 0 {
			s.OldestAge = 0
		}
	}
	return s
}

func (in *Inbox) head() (pending, bool) {
	in.mu.Lock()
	defer in.mu.Unlock()
	if len(in.queue) == 0 {
		return pending{}, false
	}
	return in.queue[0], true
}

func (in *Inbox) acknowledge(p pending) error {
	in.mu.Lock()
	defer in.mu.Unlock()
	if err := os.Remove(filepath.Join(in.dir, p.name)); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := syncDirectory(in.dir); err != nil {
		return err
	}
	in.queue = in.queue[1:]
	in.bytes -= p.size
	if len(in.queue) == 0 {
		in.failed = false
	}
	return nil
}

// Run applies reports in durable reception order. The application must commit
// the entry ID in the same DB transaction as the order/trade changes. A failed
// entry remains on disk and pauses new trading until it can be applied safely.
func (in *Inbox) Run(ctx context.Context, apply func(context.Context, Entry) error, observe func(Status, error)) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		var failure error
		for n := 0; n < 100; n++ {
			if ctx.Err() != nil {
				return
			}
			p, ok := in.head()
			if !ok {
				break
			}
			e, _, err := in.read(p.name)
			if err == nil {
				work, cancel := context.WithTimeout(ctx, 5*time.Second)
				err = apply(work, e)
				cancel()
			}
			if err == nil {
				err = in.acknowledge(p)
			}
			if err != nil {
				in.mu.Lock()
				firstFailure := !in.failed
				in.failed = true
				in.mu.Unlock()
				if firstFailure {
					in.log.Error("execution inbox apply failed; report retained and new trading paused", "entry", p.name, "error", err)
				}
				failure = err
				break
			}
		}
		if observe != nil {
			observe(in.Status(), failure)
		}
		if failure == nil && in.Status().Pending > 0 {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-in.wake:
		case <-ticker.C:
		}
	}
}
