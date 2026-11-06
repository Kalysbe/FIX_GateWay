package quikexport

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The gateway serves only the QUIK classes chosen by an administrator: rows of
// Params, Trades, Orders, Securities and Classes of other classes are not read
// out. The choice is kept in the gateway's own gateway_settings, comma-separated;
// an empty choice, the default, serves every class.
const (
	ParamClassesKey     = "market_data_classes"
	maxParamClasses     = 50
	paramClassSeparator = ","
)

var ErrInvalidClasses = errors.New("укажите до 50 кодов классов QUIK: латиница, цифры и _")

var classCodePattern = regexp.MustCompile(`^[A-Za-z0-9_]{1,32}$`)

// NormalizeClasses trims the codes and drops repeats, keeping their order.
func NormalizeClasses(list []string) ([]string, error) {
	out := []string{}
	seen := map[string]bool{}
	for _, code := range list {
		code = strings.TrimSpace(code)
		if !classCodePattern.MatchString(code) {
			return nil, ErrInvalidClasses
		}
		if !seen[code] {
			seen[code] = true
			out = append(out, code)
		}
	}
	if len(out) > maxParamClasses {
		return nil, ErrInvalidClasses
	}
	return out, nil
}

type ClassSettings struct {
	pool *pgxpool.Pool
}

// NewClassSettings uses the gateway database, not the read-only QUIK source.
func NewClassSettings(pool *pgxpool.Pool) *ClassSettings {
	return &ClassSettings{pool: pool}
}

func (c *ClassSettings) Classes(ctx context.Context) ([]string, error) {
	if c == nil || c.pool == nil {
		return nil, ErrNotConfigured
	}
	var value string
	err := c.pool.QueryRow(ctx, `SELECT value FROM gateway_settings WHERE key=$1`, ParamClassesKey).Scan(&value)
	if errors.Is(err, pgx.ErrNoRows) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(value) == "" {
		return []string{}, nil
	}
	return NormalizeClasses(strings.Split(value, paramClassSeparator))
}

func (c *ClassSettings) SetClasses(ctx context.Context, list []string, updatedBy string) ([]string, error) {
	list, err := NormalizeClasses(list)
	if err != nil {
		return nil, err
	}
	if c == nil || c.pool == nil {
		return nil, ErrNotConfigured
	}
	_, err = c.pool.Exec(ctx, `INSERT INTO gateway_settings (key, value, updated_at, updated_by)
VALUES ($1, $2, now(), $3)
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now(), updated_by = EXCLUDED.updated_by`,
		ParamClassesKey, strings.Join(list, paramClassSeparator), strings.TrimSpace(updatedBy))
	if err != nil {
		return nil, err
	}
	return list, nil
}

// SetServedClasses sets where the served classes come from; it is called once
// before the store is used. Without it, or with an empty choice, every class is served.
func (s *Store) SetServedClasses(classes func(context.Context) ([]string, error)) {
	s.classes = classes
}

type allClassesKey struct{}

// WithAllClasses lifts the served classes for one operation: an active order
// must stay cancellable after its class is taken out of the choice.
func WithAllClasses(ctx context.Context) context.Context {
	return context.WithValue(ctx, allClassesKey{}, true)
}

// servedClasses returns nil when every class is served: the queries read NULL so.
func (s *Store) servedClasses(ctx context.Context) ([]string, error) {
	if s.classes == nil || ctx.Value(allClassesKey{}) != nil {
		return nil, nil
	}
	list, err := s.classes(ctx)
	if err != nil || len(list) == 0 {
		return nil, err
	}
	return list, nil
}

// ListClasses returns ClassCode and ClassName of every QUIK class, from the
// latest TradeDate of each. It is the list the served classes are chosen from,
// so it is the one read that the choice does not limit.
func (s *Store) ListClasses(ctx context.Context) ([]Row, error) {
	return s.rows(ctx, `SELECT row_to_json(c) FROM (
SELECT DISTINCT ON (btrim(c."ClassCode"::text)) c."ClassCode", c."ClassName" FROM public."Classes" c
ORDER BY btrim(c."ClassCode"::text), c."TradeDate" DESC) c ORDER BY c."ClassCode"`)
}
