//go:build integration

package fixaudit

import (
	"bytes"
	"os"
	"testing"
)

func TestActualDiskExhaustionNeverAcknowledgesTransport(t *testing.T) {
	// Only the disposable Compose service mounts this small tmpfs. Never fill
	// a directory supplied by an operator on a workstation or trading server.
	if os.Getenv("FIX_AUDIT_DISK_FAULT_DIR") != "/fault-disk" {
		t.Skip("dedicated disposable tmpfs required")
	}
	dir, err := os.MkdirTemp("/fault-disk", "journal-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	fatal := false
	f, err := New(dir, 1<<20, func(error) { fatal = true }, nil)
	if err != nil {
		t.Fatal(err)
	}
	l, _ := f.Create()
	func() {
		defer func() {
			if recover() == nil {
				t.Error("disk exhaustion returned success")
			}
		}()
		l.OnIncoming(bytes.Repeat([]byte("X"), 128<<10))
	}()
	f.Close()
	if !fatal {
		t.Fatal("actual disk failure did not stop callback")
	}
	if f, err := New(dir, 1<<20, nil, nil); err == nil {
		f.Close()
		t.Fatal("partial disk write allowed a new FIX logon")
	}
}
