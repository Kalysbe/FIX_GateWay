package brokers

import (
	"context"
	"errors"
	"strings"

	"github.com/fix-gateway/backend/internal/auth"
	"github.com/google/uuid"
)

var ErrTokenLookup = errors.New("enter a full API token, UUID, or 8-character prefix")

const TokenLookupLimit = 20

// NormalizeTokenLookup never includes the supplied credential in its errors.
func NormalizeTokenLookup(input string) (string, error) {
	if len(input) > 512 {
		return "", ErrTokenLookup
	}
	value := strings.TrimSpace(input)
	if fields := strings.Fields(value); len(fields) == 2 && strings.EqualFold(fields[0], "Bearer") {
		value = fields[1]
	}
	value = strings.TrimSuffix(strings.TrimSuffix(value, "…"), "...")
	if id, err := uuid.Parse(value); err == nil {
		return id.String(), nil
	}
	if len(value) != 8 && (len(value) < 16 || len(value) > 512) {
		return "", ErrTokenLookup
	}
	for _, ch := range value {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '-' || ch == '_') {
			return "", ErrTokenLookup
		}
	}
	return value, nil
}

// SearchTokens reads token metadata, including expired and revoked tokens.
// Do not authenticate the credential here: inspection must not touch usage,
// change expiry state, or apply the broker's client IP restrictions.
func (s *Store) SearchTokens(ctx context.Context, input string) ([]TokenWithBroker, bool, error) {
	value, err := NormalizeTokenLookup(input)
	if err != nil {
		return nil, false, err
	}
	var where string
	var argument any
	if id, err := uuid.Parse(value); err == nil {
		where, argument = ` WHERE t.id=$1`, id
	} else if len(value) == 8 {
		where, argument = ` WHERE t.token_prefix=$1`, value
	} else {
		where = ` WHERE t.token_hash=ANY($1::text[])`
		argument = []string{auth.HashAPIToken(value, s.pepper), auth.HashToken(value, s.pepper)}
	}
	rows, err := s.listTokensWithBroker(ctx, where+` ORDER BY t.created_at DESC, t.id LIMIT 21`, argument)
	if err != nil {
		return nil, false, err
	}
	more := len(rows) > TokenLookupLimit
	if more {
		rows = rows[:TokenLookupLimit]
	}
	if rows == nil {
		rows = []TokenWithBroker{}
	}
	return rows, more, nil
}
