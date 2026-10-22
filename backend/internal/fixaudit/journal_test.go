package fixaudit

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/quickfixgo/quickfix"
)

func records(t *testing.T, dir string) []Record {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "*.audit.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var out []Record
	for _, path := range files {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range bytes.Split(raw, []byte{'\n'}) {
			if len(line) == 0 {
				continue
			}
			var r Record
			if err := json.Unmarshal(line, &r); err != nil {
				t.Fatal(err)
			}
			out = append(out, r)
		}
	}
	return out
}

func TestJournalPreservesTransportBytesBeforeValidation(t *testing.T) {
	dir := t.TempDir()
	f, err := New(dir, 1<<20, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	l, _ := f.CreateSessionLog(quickfix.SessionID{BeginString: "FIX.4.2", SenderCompID: "TEST", TargetCompID: "VENUE"})
	raw := []byte("8=FIX.4.2\x0135=8\x0134=29\x01880=261019-LIST_GBA-123\x01invalid-\xff\x0110=wrong\x01")
	l.OnIncoming(raw)
	l.OnOutgoing([]byte("35=2\x0134=30\x01"))
	l.OnEventf("rejected sequence %d", 29)
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	rows := records(t, dir)
	if len(rows) != 3 {
		t.Fatal(len(rows))
	}
	r := rows[0]
	sum := sha256.Sum256(raw)
	if !bytes.Equal(r.Raw, raw) || r.SHA256 != hex.EncodeToString(sum[:]) || r.Sequence != "29" || r.Type != "8" || r.Direction != "IN" || r.Session == "" {
		t.Fatal(r)
	}
	if rows[1].Direction != "OUT" || rows[2].Direction != "EVENT" {
		t.Fatal(rows)
	}
}

func TestJournalRedactsCredentialsButKeepsOriginalFingerprint(t *testing.T) {
	dir := t.TempDir()
	f, err := New(dir, 1<<20, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	l, _ := f.Create()
	raw := []byte("35=A\x0134=1\x01554=private-password\x01925=replacement\x0196=secret\x01")
	l.OnOutgoing(raw)
	l.OnEventf("Msg Parse Error: %q", raw)
	r := records(t, dir)[0]
	sum := sha256.Sum256(raw)
	if !r.Redacted || bytes.Contains(r.Raw, []byte("private-password")) || bytes.Contains(r.Raw, []byte("replacement")) || bytes.Contains(r.Raw, []byte("=secret")) || r.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatal("credential redaction failed")
	}
	if e := records(t, dir)[1]; !e.Redacted || bytes.Contains(e.Raw, []byte("private-password")) {
		t.Fatal("parse error event leaked credentials")
	}
}

func TestJournalCapacityNeverDeletesEarlierEvidence(t *testing.T) {
	dir := t.TempDir()
	fatal := false
	f, err := New(dir, 600, func(error) { fatal = true }, nil)
	if err != nil {
		t.Fatal(err)
	}
	l, _ := f.Create()
	l.OnIncoming([]byte("35=8\x0134=1\x01"))
	func() {
		defer func() {
			if recover() == nil {
				t.Error("unpersisted message was accepted")
			}
		}()
		l.OnIncoming(bytes.Repeat([]byte("X"), 600))
	}()
	if !fatal || len(records(t, dir)) != 1 {
		t.Fatal("capacity failure discarded evidence")
	}
	f.Close()
	f, err = New(dir, 1<<20, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	l, _ = f.Create()
	l.OnIncoming([]byte("35=8\x0134=2\x01"))
	f.Close()
	if len(records(t, dir)) != 2 {
		t.Fatal("restart lost audit history")
	}
}

func TestJournalWriteFailureStopsCallbackAndCorruptionBlocksRestart(t *testing.T) {
	dir := t.TempDir()
	fatal := false
	f, err := New(dir, 1<<20, func(error) { fatal = true }, nil)
	if err != nil {
		t.Fatal(err)
	}
	l, _ := f.Create()
	f.file.Close()
	func() {
		defer func() {
			if recover() == nil {
				t.Error("disk write failure was ignored")
			}
		}()
		l.OnIncoming([]byte("35=8\x01"))
	}()
	if !fatal {
		t.Fatal("fatal handler not called")
	}
	f.Close()
	if err := os.WriteFile(filepath.Join(dir, "corrupt.audit.jsonl"), []byte("partial"), 0600); err != nil {
		t.Fatal(err)
	}
	if f, err := New(dir, 1<<20, nil, nil); err == nil {
		f.Close()
		t.Fatal("partial audit accepted")
	}
}

func TestJournalConcurrentCallbacksRemainWhole(t *testing.T) {
	dir := t.TempDir()
	f, err := New(dir, 1<<20, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	l, _ := f.Create()
	var wg sync.WaitGroup
	for n := 0; n < 20; n++ {
		wg.Add(1)
		go func() { defer wg.Done(); l.OnIncoming([]byte("35=8\x0134=9\x01")) }()
	}
	wg.Wait()
	if len(records(t, dir)) != 20 {
		t.Fatal("concurrent records lost")
	}
}
