package auth

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PersistentNonceStore shares replay protection across REST, gRPC and process
// restarts. Only a hash of the token scope/nonce is stored, never credentials.
type PersistentNonceStore struct {
	pool *pgxpool.Pool
	ttl  time.Duration
}

func NewPersistentNonceStore(pool *pgxpool.Pool, ttl time.Duration) *PersistentNonceStore {
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	return &PersistentNonceStore{pool: pool, ttl: ttl}
}

func (s *PersistentNonceStore) UseScopedContext(ctx context.Context, scope, nonce string) error {
	nonce = strings.TrimSpace(nonce)
	if len(nonce) < nonceMinLen || len(nonce) > nonceMaxLen {
		return ErrNonceInvalid
	}
	hash := sha256.Sum256([]byte(scope + "\x00" + nonce))
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	r, err := s.pool.Exec(ctx, `INSERT INTO hmac_nonces(nonce_hash,expires_at)
	VALUES($1,clock_timestamp()+($2 * interval '1 second'))
	ON CONFLICT(nonce_hash) DO UPDATE SET expires_at=EXCLUDED.expires_at
	WHERE hmac_nonces.expires_at<=clock_timestamp()`, hash[:], s.ttl.Seconds())
	if err != nil {
		return fmt.Errorf("%w", ErrNonceUnavailable)
	}
	if r.RowsAffected() == 0 {
		return ErrNonceReplay
	}
	return nil
}

func (s *PersistentNonceStore) RunCleanup(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		work, cancel := context.WithTimeout(ctx, 5*time.Second)
		for n := 0; n < 5; n++ {
			r, err := s.pool.Exec(work, `DELETE FROM hmac_nonces WHERE nonce_hash IN(SELECT nonce_hash FROM hmac_nonces WHERE expires_at<clock_timestamp() ORDER BY expires_at LIMIT 10000)`)
			if err != nil || r.RowsAffected() < 10000 {
				break
			}
		}
		cancel()
	}
}
