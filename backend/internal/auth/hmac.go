package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"time"
)

// HMAC signs the exact escaped request-target, including the raw query string.
// Expected headers when enabled:
//   X-Timestamp — unix seconds
//   X-Nonce     — unique request nonce
//   X-Signature — hex(HMAC-SHA256(secret, timestamp + "\n" + nonce + "\n" + method + "\n" + path + "\n" + body))

func BuildHMACPayload(timestamp, nonce, method, path, body string) string {
	return timestamp + "\n" + nonce + "\n" + method + "\n" + path + "\n" + body
}

func SignHMACSHA256(secret, payload string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

func VerifyHMACSHA256(secret, payload, signatureHex string) bool {
	expected := SignHMACSHA256(secret, payload)
	return hmac.Equal([]byte(expected), []byte(signatureHex))
}

func ValidateTimestamp(ts string, skew time.Duration) error {
	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid timestamp")
	}
	t := time.Unix(sec, 0)
	now := time.Now()
	if t.Before(now.Add(-skew)) || t.After(now.Add(skew)) {
		return fmt.Errorf("timestamp outside allowed skew")
	}
	return nil
}
