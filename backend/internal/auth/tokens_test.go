package auth

import (
	"strconv"
	"testing"
	"time"
)

func TestHashTokenStable(t *testing.T) {
	a := HashToken("sk_live_abc", "pepper")
	b := HashToken("sk_live_abc", "pepper")
	c := HashToken("sk_live_abc", "other")
	if a != b {
		t.Fatal("hash not stable")
	}
	if a == c {
		t.Fatal("pepper must change hash")
	}
}

func TestGenerateAPITokenIgnoresEnvironment(t *testing.T) {
	t.Setenv("TOKEN_PREFIX", "sk_live_")
	t.Setenv("APP_ENV", "production")
	raw, prefix, err := GenerateAPIToken()
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != 64 || prefix != raw[:8] {
		t.Fatal("unexpected token format")
	}
}

func TestPermissions(t *testing.T) {
	_, ok := NormalizePermissions([]string{"orders:read", "orders:read", "bad"})
	if ok {
		t.Fatal("bad perm should fail")
	}
	p, ok := NormalizePermissions(nil)
	if !ok || len(p) != len(ReadOnlyPermissions) {
		t.Fatalf("default perms: %#v", p)
	}
	for _, write := range []string{PermOrdersCreate, PermOrdersCancel, PermOrdersReplace, PermPositionsWrite} {
		if HasPermission(p, write) {
			t.Fatalf("omitted permissions must not grant %s", write)
		}
	}
	if !HasPermission(ReadOnlyPermissions, PermOrdersRead) {
		t.Fatal("readonly should have orders:read")
	}
	if HasPermission(ReadOnlyPermissions, PermOrdersCreate) {
		t.Fatal("readonly must not create")
	}
}

func TestHMACRoundTrip(t *testing.T) {
	payload := BuildHMACPayload("1710000000", "n1", "POST", "/api/v1/orders", `{"a":1}`)
	sig := SignHMACSHA256("secret", payload)
	if !VerifyHMACSHA256("secret", payload, sig) {
		t.Fatal("verify failed")
	}
	if VerifyHMACSHA256("other", payload, sig) {
		t.Fatal("wrong secret accepted")
	}
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	if err := ValidateTimestamp(ts, time.Minute); err != nil {
		t.Fatal(err)
	}
}

func TestNonceCacheRejectsReplay(t *testing.T) {
	c := NewNonceCache(time.Minute)
	if err := c.Use("nonce-ok-1"); err != nil {
		t.Fatal(err)
	}
	if err := c.Use("nonce-ok-1"); err != ErrNonceReplay {
		t.Fatalf("got %v", err)
	}
	if err := c.Use("short"); err != ErrNonceInvalid {
		t.Fatalf("got %v", err)
	}
}

func TestPasswordRoundTrip(t *testing.T) {
	h, err := HashPassword("admin123")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword(h, "admin123") {
		t.Fatal("password mismatch")
	}
	if CheckPassword(h, "wrong") {
		t.Fatal("wrong password accepted")
	}
}
