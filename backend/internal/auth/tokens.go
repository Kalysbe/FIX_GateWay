package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

// APITokenRandomBytes provides 384 bits of entropy from the OS CSPRNG.
// The display prefix exposes eight base64 characters; 336 bits remain secret.
const APITokenRandomBytes = 48

// HashAPIToken is the versioned storage digest for newly issued API tokens.
// HashToken remains available for verification of previously issued tokens.
func HashAPIToken(raw, pepper string) string {
	mac := hmac.New(sha256.New, []byte(pepper))
	_, _ = mac.Write([]byte(raw))
	return "hmac_sha256:" + hex.EncodeToString(mac.Sum(nil))
}

func HashToken(raw, pepper string) string {
	sum := sha256.Sum256([]byte(pepper + raw))
	return hex.EncodeToString(sum[:])
}

func GenerateAPIToken() (raw string, prefix string, err error) {
	buf := make([]byte, APITokenRandomBytes)
	if _, err = rand.Read(buf); err != nil {
		return "", "", err
	}
	raw = base64.RawURLEncoding.EncodeToString(buf)
	prefix = raw[:8]
	return raw, prefix, nil
}

func GenerateOpaqueID(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func FixClOrdID(orderID string) string {
	clean := orderID
	if len(clean) > 32 {
		clean = clean[:32]
	}
	return fmt.Sprintf("G%s", clean)
}
