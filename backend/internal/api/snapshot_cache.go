package api

import (
	"context"
	"sync"
	"time"
)

// Shared per server: one refresh regardless of the number of dashboard viewers.
// Recent stale data is served immediately while a bounded refresh runs.
type snapshotCache struct {
	mu         sync.Mutex
	data       any
	updated    time.Time
	retryAfter time.Time
	pending    chan struct{}
	err        error
}

func (c *snapshotCache) get(ctx context.Context, ttl time.Duration, load func(context.Context) (any, error)) (any, error) {
	for {
		c.mu.Lock()
		age := time.Since(c.updated)
		if c.data != nil && age < ttl {
			data := c.data
			c.mu.Unlock()
			return data, nil
		}
		if c.pending == nil && time.Now().After(c.retryAfter) {
			c.pending = make(chan struct{})
			go func() {
				refreshCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
				defer cancel()
				data, err := load(refreshCtx)
				c.mu.Lock()
				if err == nil {
					c.data = data
					c.updated = time.Now()
				}
				c.err = err
				c.retryAfter = time.Now().Add(time.Second)
				close(c.pending)
				c.pending = nil
				c.mu.Unlock()
			}()
		}
		if c.data != nil && age < time.Minute {
			data := c.data
			c.mu.Unlock()
			return data, nil
		}
		pending, err := c.pending, c.err
		c.mu.Unlock()
		if pending == nil {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-pending:
		}
	}
}
