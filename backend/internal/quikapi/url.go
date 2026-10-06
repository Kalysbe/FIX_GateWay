package quikapi

import (
	"errors"
	"net"
	"net/url"
	"strings"
)

const (
	SettingKey     = "quik_api_base_url"
	DefaultBaseURL = "http://192.168.0.89:5000"
)

var (
	ErrInvalidURL    = errors.New("invalid QUIK API base URL")
	ErrNotConfigured = errors.New("QUIK API base URL is not configured")
	ErrUpstream      = errors.New("QUIK API unavailable")
)

// NormalizeBaseURL accepts a host or full origin and returns scheme://host[:port].
func NormalizeBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ErrInvalidURL
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u == nil {
		return "", ErrInvalidURL
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", ErrInvalidURL
	}
	if u.User != nil {
		return "", ErrInvalidURL
	}
	host := strings.TrimSpace(u.Host)
	if host == "" {
		return "", ErrInvalidURL
	}
	hostname := u.Hostname()
	if hostname == "" || blockedHost(hostname) {
		return "", ErrInvalidURL
	}
	if u.Port() == "" {
		host = net.JoinHostPort(hostname, "5000")
	}
	out := &url.URL{Scheme: scheme, Host: host}
	return strings.TrimRight(out.String(), "/"), nil
}

func blockedHost(hostname string) bool {
	h := strings.ToLower(strings.TrimSpace(hostname))
	switch h {
	case "169.254.169.254", "metadata.google.internal", "metadata":
		return true
	}
	ip := net.ParseIP(h)
	if ip == nil {
		return false
	}
	if ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() {
		return true
	}
	return false
}
