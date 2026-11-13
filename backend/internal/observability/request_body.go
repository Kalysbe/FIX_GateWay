package observability

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// maxLoggedBody caps the broker request body kept for a failed request's event.
const maxLoggedBody = 16 << 10

// bodyRecorder keeps the first maxLoggedBody bytes the handler reads, so a
// failed broker request's event shows the JSON the broker actually sent.
type bodyRecorder struct {
	io.ReadCloser
	buf       bytes.Buffer
	truncated bool
}

func (b *bodyRecorder) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if n > 0 {
		room := maxLoggedBody - b.buf.Len()
		switch {
		case room >= n:
			b.buf.Write(p[:n])
		case room > 0:
			b.buf.Write(p[:room])
			b.truncated = true
		default:
			b.truncated = true
		}
	}
	return n, err
}

// recordsBody: only broker write requests. Admin requests are skipped
// because their bodies carry passwords (login, password change).
func recordsBody(r *http.Request) bool {
	if r.Body == nil || r.Body == http.NoBody {
		return false
	}
	switch r.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
	default:
		return false
	}
	p := r.URL.Path
	return strings.HasPrefix(p, "/api/v1/") && !strings.HasPrefix(p, "/api/v1/admin/")
}

// loggedBody returns the recorded body text for the event: the JSON as sent
// (compacted, numbers untouched), with values of secret-looking keys masked.
// A body that is not valid JSON, or was cut at the limit, is kept as text.
func (b *bodyRecorder) loggedBody() string {
	raw := b.buf.Bytes()
	if len(bytes.TrimSpace(raw)) == 0 {
		return ""
	}
	if !b.truncated && json.Valid(raw) {
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		var v any
		if dec.Decode(&v) == nil && maskSecrets(v) {
			if out, err := json.Marshal(v); err == nil {
				return string(out)
			}
		}
		var out bytes.Buffer
		if json.Compact(&out, raw) == nil {
			return out.String()
		}
	}
	s := RedactSecrets(strings.ToValidUTF8(string(raw), "�"))
	if b.truncated {
		s += "…"
	}
	return s
}

// maskSecrets replaces values of secret-looking keys in place and reports
// whether anything was masked.
func maskSecrets(v any) bool {
	masked := false
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			if secretKey(k) {
				t[k] = "***"
				masked = true
				continue
			}
			if maskSecrets(val) {
				masked = true
			}
		}
	case []any:
		for _, val := range t {
			if maskSecrets(val) {
				masked = true
			}
		}
	}
	return masked
}
