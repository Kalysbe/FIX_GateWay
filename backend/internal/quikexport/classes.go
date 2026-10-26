package quikexport

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Market data serves the Params rows of the QUIK classes chosen by an
// administrator. The choice is kept in the gateway's own gateway_settings,
// comma-separated; until it is changed, LISTBND is served.
const (
	ParamClassesKey     = "market_data_classes"
	DefaultParamClass   = "LISTBND"
	maxParamClasses     = 50
	paramClassSeparator = ","
)

var ErrInvalidClasses = errors.New("укажите от 1 до 50 кодов классов QUIK: латиница, цифры и _")

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
	if len(out) == 0 || len(out) > maxParamClasses {
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
		return []string{DefaultParamClass}, nil
	}
	if err != nil {
		return nil, err
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

// ListClasses returns ClassCode and ClassName of every QUIK class, from the
// latest TradeDate of each, for choosing the served classes.
func (s *Store) ListClasses(ctx context.Context) ([]Row, error) {
	return s.rows(ctx, `SELECT row_to_json(c) FROM (
SELECT DISTINCT ON (btrim(c."ClassCode"::text)) c."ClassCode", c."ClassName" FROM public."Classes" c
ORDER BY btrim(c."ClassCode"::text), c."TradeDate" DESC) c ORDER BY c."ClassCode"`)
}
