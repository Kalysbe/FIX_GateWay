package auth

import (
	"strings"
	"testing"
	"time"
)

func TestScopedNonceIsolation(t *testing.T) {
	cache := NewNonceCache(10 * time.Minute)
	nonce := strings.Repeat("a", 128)
	if err := cache.UseScoped("token-A", nonce); err != nil {
		t.Fatal(err)
	}
	if err := cache.UseScoped("token-B", nonce); err != nil {
		t.Fatal(err)
	}
	if err := cache.UseScoped("token-A", nonce); err != ErrNonceReplay {
		t.Fatalf("replay: %v", err)
	}
	if err := cache.UseScoped("token-A", "short"); err != ErrNonceInvalid {
		t.Fatalf("validation: %v", err)
	}
}
