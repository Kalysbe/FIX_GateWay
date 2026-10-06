package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"testing"
)

func TestAPITokenEntropyAndUniqueness(t *testing.T) {
	t.Setenv("TOKEN_PREFIX", "sk_live_")
	seen := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		raw, prefix, err := GenerateAPIToken()
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := base64.RawURLEncoding.DecodeString(raw)
		if err != nil || len(decoded) != 48 {
			t.Fatal("token must encode 384 random bits")
		}
		if len(raw) != 64 || prefix != raw[:8] {
			t.Fatal("unexpected token or display prefix length")
		}
		if seen[raw] {
			t.Fatal("duplicate generated token")
		}
		seen[raw] = true
	}
}

func TestAPITokenStorageDigest(t *testing.T) {
	mac := hmac.New(sha256.New, []byte("pepper"))
	_, _ = mac.Write([]byte("token"))
	want := "hmac_sha256:" + hex.EncodeToString(mac.Sum(nil))
	if HashAPIToken("token", "pepper") != want {
		t.Fatal("storage digest must use versioned HMAC-SHA-256")
	}
	if HashAPIToken("token", "other") == want || HashAPIToken("other", "pepper") == want {
		t.Fatal("digest must bind both token and pepper")
	}
	// Legacy credentials must retain their original digest for lookup.
	sum := sha256.Sum256([]byte("peppertoken"))
	if HashToken("token", "pepper") != hex.EncodeToString(sum[:]) {
		t.Fatal("legacy digest changed")
	}
}
