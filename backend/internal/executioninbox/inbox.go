// Package executioninbox durably accepts incoming FIX reports independently of
// PostgreSQL. A callback must not return to the FIX engine until Append succeeds.
package executioninbox

import (
	"bytes"
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
	"unicode/utf8"

	"github.com/fix-gateway/backend/internal/domain"
	"github.com/google/uuid"
)

const maxReportBytes = 1 << 20

// QuarantineDir holds entries an operator set aside. They are never replayed.
const QuarantineDir = "quarantine"

// ErrQuarantined is returned by an apply callback for an entry that has an
// operator quarantine case. The entry is moved aside instead of being deleted.
var ErrQuarantined = errors.New("execution inbox entry quarantined by operator")

// ErrNotBlocked means the entry is not the failed head of the queue.
var ErrNotBlocked = errors.New("execution inbox entry is not the blocked head")

type Entry struct {
	Checksum   string                 `json:"checksum,omitempty"`
	ID         uuid.UUID              `json:"id"`
	ReceivedAt time.Time              `json:"received_at"`
	Report     domain.ExecutionReport `json:"report"`
}

type pending struct {
	name      string
	size      int64
	received  time.Time
	recovered bool // present on disk at Open
}

type Status struct {
	Pending   int
	Bytes     int64
	OldestAge time.Duration
	Failed    bool
}

// Blocked describes the head entry that pauses trading.
type Blocked struct {
	Name      string
	Entry     Entry
	Error     string
	FailedAt  time.Time
	Attempts  int
	ReadError bool // the file itself is unreadable; Entry is empty
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

	// applyMu serialises the worker with operator actions on the head entry.
	applyMu   sync.Mutex
	failName  string
	failErr   string
	failAt    time.Time
	failCount int
	held      func(context.Context, uuid.UUID) (bool, error)
}

// SetQuarantineCheck installs the lookup of operator quarantine cases. It must
// be called before Run. An entry with a case is moved aside, never applied.
func (in *Inbox) SetQuarantineCheck(held func(context.Context, uuid.UUID) (bool, error)) {
	in.held = held
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
		in.queue = append(in.queue, pending{f.Name(), size, e.ReceivedAt, true})
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
	if e.Checksum == "" || (e.Checksum != entryChecksum(e) && e.Checksum != legacyEntryChecksum(e)) {
		return e, 0, fmt.Errorf("execution inbox checksum mismatch: %s", name)
	}
	return e, int64(len(raw)), nil
}

func entryChecksum(e Entry) string {
	e.Checksum = ""
	raw, _ := json.Marshal(e)
	return checksumOf(raw)
}

// legacyEntryChecksum verifies entries written before Append normalized
// reports: their checksum covered text with invalid UTF-8 (Windows-1251 from
// QUIK), which encoding/json writes as an escape (backslash-u-fffd), while the decoded
// entry holds U+FFFD, which it writes as UTF-8. Restoring the escape gives the
// bytes that were hashed, so a changed file still fails.
func legacyEntryChecksum(e Entry) string {
	e.Checksum = ""
	raw, _ := json.Marshal(e)
	if !bytes.ContainsRune(raw, utf8.RuneError) {
		return ""
	}
	return checksumOf(bytes.ReplaceAll(raw, []byte(string(utf8.RuneError)), jsonRuneErrorEscape))
}

// jsonRuneErrorEscape is the six bytes backslash, "u", "fffd" that
// encoding/json writes for invalid UTF-8.
var jsonRuneErrorEscape = []byte{'\\', 'u', 'f', 'f', 'f', 'd'}

func checksumOf(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// normalizeEntry gives the entry the form it has after it is read back:
// encoding/json replaces invalid UTF-8, so the checksum must cover the
// replaced text, not the bytes received.
func normalizeEntry(e Entry) (Entry, error) {
	raw, err := json.Marshal(e)
	if err != nil {
		return e, err
	}
	var out Entry
	err = json.Unmarshal(raw, &out)
	return out, err
}

// Append fsyncs the content, atomically publishes the file and fsyncs the parent
// directory. PostgreSQL availability does not affect acceptance of the report.
func (in *Inbox) Append(report domain.ExecutionReport) error {
	in.mu.Lock()
	defer in.mu.Unlock()
	e, err := normalizeEntry(Entry{ID: uuid.New(), ReceivedAt: time.Now().UTC(), Report: report})
	if err != nil {
		return err
	}
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
	in.queue = append(in.queue, pending{name, int64(len(raw)), e.ReceivedAt, false})
	in.bytes += int64(len(raw))
	in.Wake()
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
	if in.failName == p.name {
		in.failName, in.failErr, in.failAt, in.failCount = "", "", time.Time{}, 0
	}
	return nil
}

// moveToQuarantine durably moves the head entry aside and removes it from the
// queue. The next entry is retried before trading is considered healthy.
func (in *Inbox) moveToQuarantine(p pending) error {
	in.mu.Lock()
	defer in.mu.Unlock()
	qdir := filepath.Join(in.dir, QuarantineDir)
	if err := os.MkdirAll(qdir, 0700); err != nil {
		return err
	}
	dst := filepath.Join(qdir, p.name)
	if err := os.Rename(filepath.Join(in.dir, p.name), dst); err != nil {
		// A previous attempt may have moved the file and failed to sync.
		if _, statErr := os.Stat(dst); !os.IsNotExist(err) || statErr != nil {
			return err
		}
	}
	if err := errors.Join(syncDirectory(qdir), syncDirectory(in.dir)); err != nil {
		return err
	}
	in.queue = in.queue[1:]
	in.bytes -= p.size
	in.failed = false
	in.failName, in.failErr, in.failAt, in.failCount = "", "", time.Time{}, 0
	return nil
}

// Blocked returns the head entry that failed to apply, if trading is paused.
// When the file itself is unreadable, Entry is empty and ReadError is set.
func (in *Inbox) Blocked() (Blocked, bool) {
	in.mu.Lock()
	if !in.failed || len(in.queue) == 0 || in.queue[0].name != in.failName {
		in.mu.Unlock()
		return Blocked{}, false
	}
	b := Blocked{Name: in.failName, Error: in.failErr, FailedAt: in.failAt, Attempts: in.failCount}
	in.mu.Unlock()
	e, _, err := in.read(b.Name)
	if err != nil {
		b.ReadError = true
		b.Error = err.Error()
	}
	b.Entry = e
	return b, true
}

// WithBlocked runs fn while the worker is paused, but only if id is still the
// failed head entry. fn typically records an operator quarantine case; after
// it succeeds the worker is woken to move the entry aside.
func (in *Inbox) WithBlocked(id uuid.UUID, fn func(Blocked) error) error {
	in.applyMu.Lock()
	defer in.applyMu.Unlock()
	b, ok := in.Blocked()
	if !ok || !strings.HasSuffix(b.Name, "_"+id.String()+".json") {
		return ErrNotBlocked
	}
	if err := fn(b); err != nil {
		return err
	}
	in.Wake()
	return nil
}

// Wake makes the worker retry the head entry without waiting for its ticker.
func (in *Inbox) Wake() {
	select {
	case in.wake <- struct{}{}:
	default:
	}
}

// EntryID extracts the entry UUID from an inbox file name, also when the
// file content is unreadable.
func EntryID(name string) (uuid.UUID, error) {
	_, rest, ok := strings.Cut(strings.TrimSuffix(name, ".json"), "_")
	if !ok {
		return uuid.Nil, fmt.Errorf("invalid inbox filename %s", name)
	}
	return uuid.Parse(rest)
}

// Run applies reports in durable reception order. The application must commit
// the entry ID in the same DB transaction as the order/trade changes. A failed
// entry remains on disk and pauses new trading until it can be applied safely
// or an operator quarantines it (see SetQuarantineCheck and WithBlocked).
func (in *Inbox) Run(ctx context.Context, apply func(context.Context, Entry) error, observe func(Status, error)) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		var failure error
		for n := 0; n < 100; n++ {
			if ctx.Err() != nil {
				return
			}
			if failure = in.step(ctx, apply); failure != nil || in.Status().Pending == 0 {
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

func (in *Inbox) step(ctx context.Context, apply func(context.Context, Entry) error) error {
	in.applyMu.Lock()
	defer in.applyMu.Unlock()
	p, ok := in.head()
	if !ok {
		return nil
	}
	in.mu.Lock()
	retry := p.recovered || in.failName == p.name
	in.mu.Unlock()
	work, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var err error
	// Only an entry that already failed, or survived a restart, can have an
	// operator case; ordinary deliveries skip the lookup.
	if retry && in.held != nil {
		var id uuid.UUID
		if id, err = EntryID(p.name); err == nil {
			var held bool
			if held, err = in.held(work, id); err == nil && held {
				err = ErrQuarantined
			}
		}
	}
	if err == nil {
		var e Entry
		if e, _, err = in.read(p.name); err == nil {
			err = apply(work, e)
		}
	}
	switch {
	case err == nil:
		err = in.acknowledge(p)
	case errors.Is(err, ErrQuarantined):
		if err = in.moveToQuarantine(p); err == nil {
			in.log.Warn("execution inbox entry moved to quarantine by operator case", "entry", p.name)
			return nil
		}
	}
	if err != nil {
		in.mu.Lock()
		firstFailure := !in.failed || in.failName != p.name
		in.failed = true
		if in.failName != p.name {
			in.failName, in.failAt, in.failCount = p.name, time.Now().UTC(), 0
		}
		in.failErr = err.Error()
		in.failCount++
		in.mu.Unlock()
		if firstFailure {
			in.log.Error("execution inbox apply failed; report retained and new trading paused", "entry", p.name, "error", err)
		}
		return err
	}
	return nil
}
