package auth

import (
	"errors"
	"strings"
	"sync"
	"time"
)

var (
	ErrNonceInvalid = errors.New("invalid nonce")
	ErrNonceReplay  = errors.New("nonce reused")
)

const (
	nonceMinLen = 8
	nonceMaxLen = 128
)

// NonceCache rejects reused nonces for ttl. In-memory; one process only.
type NonceCache struct {
	ttl  time.Duration
	mu   sync.Mutex
	seen map[string]time.Time
	last time.Time
}

func NewNonceCache(ttl time.Duration) *NonceCache {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &NonceCache{ttl: ttl, seen: make(map[string]time.Time)}
}

func (c *NonceCache) Use(nonce string) error {
	return c.UseScoped("", nonce)
}

func (c *NonceCache) UseScoped(scope, nonce string) error {
	nonce = strings.TrimSpace(nonce)
	if len(nonce) < nonceMinLen || len(nonce) > nonceMaxLen {
		return ErrNonceInvalid
	}
	nonce = scope + ":" + nonce
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	if now.Sub(c.last) > time.Minute || len(c.seen) > 50_000 {
		c.gc(now)
		c.last = now
	}
	if exp, ok := c.seen[nonce]; ok && now.Before(exp) {
		return ErrNonceReplay
	}
	c.seen[nonce] = now.Add(c.ttl)
	return nil
}

func (c *NonceCache) gc(now time.Time) {
	for k, exp := range c.seen {
		if !now.Before(exp) {
			delete(c.seen, k)
		}
	}
}
