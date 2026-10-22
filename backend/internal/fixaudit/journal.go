// Package fixaudit journals FIX transport messages before application parsing.
package fixaudit

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/quickfixgo/quickfix"
)

const DefaultMaxBytes int64 = 4 << 30
const segmentBytes int64 = 64 << 20

type Record struct {
	Version   int       `json:"version"`
	Time      time.Time `json:"time"`
	Session   string    `json:"session"`
	Direction string    `json:"direction"`
	Type      string    `json:"message_type,omitempty"`
	Sequence  string    `json:"sequence,omitempty"`
	SHA256    string    `json:"raw_sha256"`
	Raw       []byte    `json:"raw_base64"`
	Redacted  bool      `json:"credentials_redacted,omitempty"`
}

type Factory struct {
	mu              sync.Mutex
	dir             string
	max, used, size int64
	file            *os.File
	day             string
	closed          bool
	fatal           func(error)
	onBytes         func(int64)
}

var _ quickfix.LogFactory = (*Factory)(nil)

// New validates the persistent directory before a FIX connection is opened.
// Files are never automatically removed: capacity exhaustion must be visible.
func New(dir string, max int64, fatal func(error), onBytes func(int64)) (*Factory, error) {
	if max <= 0 {
		return nil, fmt.Errorf("FIX audit capacity must be positive")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	f := &Factory{dir: dir, max: max, fatal: fatal, onBytes: onBytes}
	files, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, entry := range files {
		if !strings.HasSuffix(entry.Name(), ".audit.jsonl") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("non-regular FIX audit segment")
		}
		if info.Size() > 0 {
			previous, err := os.Open(filepath.Join(dir, entry.Name()))
			if err != nil {
				return nil, err
			}
			last := make([]byte, 1)
			_, err = previous.ReadAt(last, info.Size()-1)
			previous.Close()
			if err != nil || last[0] != '\n' {
				return nil, fmt.Errorf("incomplete FIX audit segment %s; preserve and reconcile before restart", entry.Name())
			}
		}
		f.used += info.Size()
	}
	if f.used >= max {
		return nil, fmt.Errorf("FIX audit capacity exhausted; archive journal before restart")
	}
	if err := f.rotate(time.Now().UTC()); err != nil {
		return nil, err
	}
	if onBytes != nil {
		onBytes(f.used)
	}
	return f, nil
}

func (f *Factory) rotate(now time.Time) error {
	if f.file != nil {
		if err := f.file.Close(); err != nil {
			return err
		}
		f.file = nil
	}
	name := now.Format("20060102T150405") + "-" + uuid.NewString() + ".audit.jsonl"
	file, err := os.OpenFile(filepath.Join(f.dir, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := syncDirectory(f.dir); err != nil {
		file.Close()
		return err
	}
	f.file, f.day, f.size = file, now.Format(time.DateOnly), 0
	return nil
}

func (f *Factory) Create() (quickfix.Log, error) {
	return &sessionLog{factory: f, session: "GLOBAL"}, nil
}
func (f *Factory) CreateSessionLog(id quickfix.SessionID) (quickfix.Log, error) {
	return &sessionLog{factory: f, session: id.String()}, nil
}
func (f *Factory) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	if f.file == nil {
		return nil
	}
	err := f.file.Close()
	f.file = nil
	return err
}

func (f *Factory) Usage() (int64, int64) { f.mu.Lock(); defer f.mu.Unlock(); return f.used, f.max }

type sessionLog struct {
	factory *Factory
	session string
}

func (l *sessionLog) OnIncoming(raw []byte) { l.write("IN", raw) }
func (l *sessionLog) OnOutgoing(raw []byte) { l.write("OUT", raw) }
func (l *sessionLog) OnEvent(event string)  { l.write("EVENT", []byte(event)) }
func (l *sessionLog) OnEventf(format string, args ...interface{}) {
	l.OnEvent(fmt.Sprintf(format, args...))
}
func (l *sessionLog) write(direction string, raw []byte) {
	if err := l.factory.append(l.session, direction, raw); err != nil {
		if l.factory.fatal != nil {
			l.factory.fatal(err)
		}
		// Log callbacks cannot return an error. Never continue with an unjournalled message.
		panic(fmt.Errorf("FIX audit persistence failure: %w", err))
	}
}

func (f *Factory) append(session, direction string, raw []byte) error {
	sum := sha256.Sum256(raw)
	r := Record{Version: 1, Time: time.Now().UTC(), Session: session, Direction: direction, SHA256: hex.EncodeToString(sum[:]), Raw: raw}
	// QuickFIX parse-error events may quote the original wire, including logon credentials.
	if direction == "EVENT" && (bytes.Contains(raw, []byte("554=")) || bytes.Contains(raw, []byte("925=")) || bytes.Contains(raw, []byte("96="))) {
		r.Raw = []byte("FIX event containing credentials redacted; see associated transport fingerprint")
		r.Redacted = true
	}
	if direction != "EVENT" {
		parts := bytes.Split(raw, []byte{1})
		for i, part := range parts {
			tag, value, ok := bytes.Cut(part, []byte{'='})
			if !ok {
				continue
			}
			switch string(tag) {
			case "35":
				r.Type = string(value)
			case "34":
				r.Sequence = string(value)
			case "554", "925", "96":
				parts[i] = append(append([]byte{}, tag...), []byte("=[REDACTED]")...)
				r.Redacted = true
			}
		}
		if r.Redacted {
			r.Raw = bytes.Join(parts, []byte{1})
		}
	}
	line, err := json.Marshal(r)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return fmt.Errorf("FIX audit journal is closed")
	}
	if int64(len(line)) > f.max-f.used {
		return fmt.Errorf("FIX audit capacity exhausted (%s bytes)", strconv.FormatInt(f.max, 10))
	}
	if f.day != r.Time.Format(time.DateOnly) || f.size+int64(len(line)) > segmentBytes {
		if err := f.rotate(r.Time); err != nil {
			return err
		}
	}
	n, err := f.file.Write(line)
	if err == nil && n != len(line) {
		err = io.ErrShortWrite
	}
	if err != nil {
		return err
	}
	if err := f.file.Sync(); err != nil {
		return err
	}
	f.used += int64(n)
	f.size += int64(n)
	if f.onBytes != nil {
		f.onBytes(f.used)
	}
	return nil
}
