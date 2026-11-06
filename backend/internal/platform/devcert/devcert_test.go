package devcert

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func read(t *testing.T, dir, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestEnsureCreatesThenKeepsAValidPKI(t *testing.T) {
	dir := t.TempDir()
	if r, err := Ensure(dir, []string{"localhost"}); err != nil || r != Created {
		t.Fatalf("first run: %v %v", r, err)
	}
	ca := read(t, dir, "ca.crt")
	if r, err := Ensure(dir, []string{"localhost"}); err != nil || r != Kept {
		t.Fatalf("second run: %v %v", r, err)
	}
	if !bytes.Equal(ca, read(t, dir, "ca.crt")) {
		t.Fatal("a valid CA must never change: brokers trust it")
	}
	until, err := ServerExpiry(dir)
	if err != nil || time.Until(until) < LeafLifetime-time.Hour {
		t.Fatalf("server certificate lifetime: %v %v", until, err)
	}
}

func TestEnsureReplacesAnExpiredPKI(t *testing.T) {
	dir := t.TempDir()
	// A PKI issued long enough ago that its leaves have expired.
	if err := create(dir, []string{"localhost"}, time.Now().Add(-LeafLifetime-time.Hour)); err != nil {
		t.Fatal(err)
	}
	old := read(t, dir, "ca.crt")
	r, err := Ensure(dir, []string{"localhost"})
	if err != nil || r != Replaced {
		t.Fatalf("expired PKI: %v %v", r, err)
	}
	if bytes.Equal(old, read(t, dir, "ca.crt")) {
		t.Fatal("the replacement must have a new CA")
	}
	if err := verify(dir, time.Now()); err != nil {
		t.Fatalf("new PKI does not verify: %v", err)
	}
}

func TestEnsureNeverTouchesAPartialOrForeignPKI(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ca.crt"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Ensure(dir, nil); err == nil {
		t.Fatal("partial PKI accepted")
	}
	other := t.TempDir()
	if err := create(other, []string{"localhost"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	// A server certificate from another CA is invalid but not expired: keep it, report it.
	foreign := t.TempDir()
	if err := create(foreign, []string{"localhost"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, "ca.crt"), read(t, foreign, "ca.crt"), 0600); err != nil {
		t.Fatal(err)
	}
	before := read(t, other, "server.crt")
	if _, err := Ensure(other, []string{"localhost"}); err == nil {
		t.Fatal("a PKI signed by another CA must be reported, not replaced")
	}
	if !bytes.Equal(before, read(t, other, "server.crt")) {
		t.Fatal("an invalid but unexpired PKI was overwritten")
	}
}
