package observability

import (
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"strings"
)

func marshalJSON(v any) ([]byte, error) { return json.Marshal(v) }

func ParseTrustedProxies(csv string) []*net.IPNet {
	var out []*net.IPNet
	for _, p := range strings.Split(csv, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if !strings.Contains(p, "/") {
			if ip := net.ParseIP(p); ip != nil {
				bits := 32
				if ip.To4() == nil {
					bits = 128
				}
				p = p + "/" + strconv.Itoa(bits)
			}
		}
		_, n, err := net.ParseCIDR(p)
		if err == nil {
			out = append(out, n)
		}
	}
	return out
}

func RemoteIP(r *http.Request) string {
	host := r.RemoteAddr
	if host == "" {
		return ""
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return strings.Trim(host, "[]")
}

func ipInNets(ip net.IP, nets []*net.IPNet) bool {
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// ClientIP returns the peer IP. X-Forwarded-For is used only when the immediate
// peer is in the trusted proxy list. forwardedFor is always recorded as received.
func ClientIP(r *http.Request, trusted []*net.IPNet) (clientIP, forwardedFor string) {
	forwardedFor = strings.TrimSpace(r.Header.Get("X-Forwarded-For"))
	remote := RemoteIP(r)
	if forwardedFor == "" || len(trusted) == 0 {
		return remote, forwardedFor
	}
	peer := net.ParseIP(remote)
	if peer == nil || !ipInNets(peer, trusted) {
		return remote, forwardedFor
	}
	parts := strings.Split(forwardedFor, ",")
	for i := len(parts) - 1; i >= 0; i-- {
		cand := net.ParseIP(strings.TrimSpace(parts[i]))
		if cand == nil {
			continue
		}
		if !ipInNets(cand, trusted) {
			return cand.String(), forwardedFor
		}
	}
	first := strings.TrimSpace(parts[0])
	if ip := net.ParseIP(first); ip != nil {
		return ip.String(), forwardedFor
	}
	return remote, forwardedFor
}
