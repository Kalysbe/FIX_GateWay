package observability

import (
	"regexp"
	"strings"
)

var (
	bearerRe = regexp.MustCompile(`(?i)(bearer\s+)[A-Za-z0-9\-\._~\+\/]+=*`)
	skRe     = regexp.MustCompile(`(?i)\b(sk_(?:live|test)_)[A-Za-z0-9\-_]+`)
	keyValRe = regexp.MustCompile(`(?i)(authorization|password|passwd|secret|api[_-]?token|api[_-]?key|private[_-]?key|hmac|jwt[_-]?secret|fix[_-]?password)\s*[:=]\s*([^\s,;]+)`)
)

func RedactSecrets(s string) string {
	if s == "" {
		return s
	}
	out := bearerRe.ReplaceAllString(s, "${1}***")
	out = skRe.ReplaceAllString(out, "${1}***")
	out = keyValRe.ReplaceAllString(out, "$1=***")
	return out
}

func RedactQuery(raw string) string {
	if raw == "" {
		return ""
	}
	parts := strings.Split(raw, "&")
	for i, p := range parts {
		kv := strings.SplitN(p, "=", 2)
		if len(kv) != 2 {
			continue
		}
		k := strings.ToLower(kv[0])
		if secretKey(k) {
			parts[i] = kv[0] + "=***"
		}
	}
	return strings.Join(parts, "&")
}

func secretKey(k string) bool {
	k = strings.ToLower(strings.ReplaceAll(k, "-", "_"))
	switch k {
	case "authorization", "token", "access_token", "refresh_token", "password", "passwd",
		"secret", "api_token", "api_key", "private_key", "hmac", "jwt", "jwt_secret", "fix_password":
		return true
	}
	return strings.Contains(k, "secret") || strings.Contains(k, "password") || strings.HasSuffix(k, "_token")
}

func SafeJSON(v any) []byte {
	if v == nil {
		return nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	clean := map[string]any{}
	for k, val := range m {
		if secretKey(k) {
			continue
		}
		if s, ok := val.(string); ok {
			clean[k] = RedactSecrets(s)
			continue
		}
		clean[k] = val
	}
	b, err := marshalJSON(clean)
	if err != nil {
		return nil
	}
	return b
}
