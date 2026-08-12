package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
)

func HashToken(raw, pepper string) string {
	sum := sha256.Sum256([]byte(pepper + raw))
	return hex.EncodeToString(sum[:])
}

// TokenPrefix returns sk_live_ in production and sk_test_ otherwise.
// Override with TOKEN_PREFIX if set.
func TokenPrefix() string {
	if p := os.Getenv("TOKEN_PREFIX"); p != "" {
		return p
	}
	env := strings.ToLower(strings.TrimSpace(os.Getenv("APP_ENV")))
	switch env {
	case "production", "prod":
		return "sk_live_"
	default:
		return "sk_test_"
	}
}

func GenerateAPIToken() (raw string, prefix string, err error) {
	buf := make([]byte, 32)
	if _, err = rand.Read(buf); err != nil {
		return "", "", err
	}
	head := TokenPrefix()
	raw = head + base64.RawURLEncoding.EncodeToString(buf)
	prefix = raw
	max := len(head) + 8
	if len(prefix) > max {
		prefix = prefix[:max]
	}
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
