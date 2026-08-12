package auth

import (
	"strconv"
	"strings"
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

func TestGenerateAPITokenPrefix(t *testing.T) {
	t.Setenv("TOKEN_PREFIX", "sk_live_")
	raw, prefix, err := GenerateAPIToken()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(raw, "sk_live_") {
		t.Fatalf("expected sk_live_ prefix, got %s", raw)
	}
	if len(raw) < 40 || len(prefix) == 0 {
		t.Fatalf("unexpected token raw=%s prefix=%s", raw, prefix)
	}
}

func TestTokenPrefixEnv(t *testing.T) {
	t.Setenv("TOKEN_PREFIX", "")
	t.Setenv("APP_ENV", "production")
	if TokenPrefix() != "sk_live_" {
		t.Fatalf("prod prefix: %s", TokenPrefix())
	}
	t.Setenv("APP_ENV", "development")
	if TokenPrefix() != "sk_test_" {
		t.Fatalf("dev prefix: %s", TokenPrefix())
	}
}

func TestPermissions(t *testing.T) {
	_, ok := NormalizePermissions([]string{"orders:read", "orders:read", "bad"})
	if ok {
		t.Fatal("bad perm should fail")
	}
	p, ok := NormalizePermissions(nil)
	if !ok || len(p) != len(AllPermissions) {
		t.Fatalf("default perms: %#v", p)
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
