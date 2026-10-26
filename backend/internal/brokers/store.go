package brokers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/fix-gateway/backend/internal/auth"
	"github.com/fix-gateway/backend/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound = errors.New("not found")
)

type Store struct {
	pool   *pgxpool.Pool
	pepper string

	lastUsedMu sync.Mutex
	lastUsed   map[uuid.UUID]time.Time
}

func NewStore(pool *pgxpool.Pool, pepper string) *Store {
	return &Store{
		pool:     pool,
		pepper:   pepper,
		lastUsed: make(map[uuid.UUID]time.Time),
	}
}

const brokerSelect = `
	SELECT id, code, name, description, status,
	       COALESCE(ip_whitelist_enabled, false),
	       COALESCE(rate_limit_rps, 100),
	       COALESCE(rate_limit_orders_per_sec, 20),
	       created_at, updated_at
	FROM brokers`

func scanBroker(row pgx.Row) (domain.Broker, error) {
	var b domain.Broker
	err := row.Scan(
		&b.ID, &b.Code, &b.Name, &b.Description, &b.Status,
		&b.IPWhitelistEnabled, &b.RateLimitRPS, &b.RateLimitOrdersPerSec,
		&b.CreatedAt, &b.UpdatedAt,
	)
	return b, err
}

func (s *Store) CreateBroker(ctx context.Context, code, name, description string) (domain.Broker, error) {
	return scanBroker(s.pool.QueryRow(ctx, `
		INSERT INTO brokers (code, name, description) VALUES ($1,$2,$3)
		RETURNING id, code, name, description, status,
		          COALESCE(ip_whitelist_enabled, false),
		          COALESCE(rate_limit_rps, 100),
		          COALESCE(rate_limit_orders_per_sec, 20),
		          created_at, updated_at`,
		code, name, description,
	))
}

func (s *Store) ListBrokers(ctx context.Context) ([]domain.Broker, error) {
	rows, err := s.pool.Query(ctx, brokerSelect+` ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Broker
	for rows.Next() {
		b, err := scanBroker(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *Store) GetBroker(ctx context.Context, id int64) (domain.Broker, error) {
	b, err := scanBroker(s.pool.QueryRow(ctx, brokerSelect+` WHERE id=$1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return b, ErrNotFound
	}
	return b, err
}

type BrokerUpdate struct {
	Name                  *string
	Description           *string
	Status                *domain.BrokerStatus
	IPWhitelistEnabled    *bool
	RateLimitRPS          *int
	RateLimitOrdersPerSec *int
}

func (s *Store) UpdateBroker(ctx context.Context, id int64, name, description *string, status *domain.BrokerStatus) (domain.Broker, error) {
	return s.UpdateBrokerExt(ctx, id, BrokerUpdate{Name: name, Description: description, Status: status})
}

func (s *Store) UpdateBrokerExt(ctx context.Context, id int64, upd BrokerUpdate) (domain.Broker, error) {
	b, err := s.GetBroker(ctx, id)
	if err != nil {
		return b, err
	}
	if upd.Name != nil {
		b.Name = *upd.Name
	}
	if upd.Description != nil {
		b.Description = *upd.Description
	}
	if upd.Status != nil {
		b.Status = *upd.Status
	}
	if upd.IPWhitelistEnabled != nil {
		b.IPWhitelistEnabled = *upd.IPWhitelistEnabled
	}
	if upd.RateLimitRPS != nil {
		b.RateLimitRPS = *upd.RateLimitRPS
	}
	if upd.RateLimitOrdersPerSec != nil {
		b.RateLimitOrdersPerSec = *upd.RateLimitOrdersPerSec
	}
	return scanBroker(s.pool.QueryRow(ctx, `
		UPDATE brokers SET name=$2, description=$3, status=$4,
		                   ip_whitelist_enabled=$5, rate_limit_rps=$6, rate_limit_orders_per_sec=$7,
		                   updated_at=now()
		WHERE id=$1
		RETURNING id, code, name, description, status,
		          COALESCE(ip_whitelist_enabled, false),
		          COALESCE(rate_limit_rps, 100),
		          COALESCE(rate_limit_orders_per_sec, 20),
		          created_at, updated_at`,
		id, b.Name, b.Description, b.Status, b.IPWhitelistEnabled, b.RateLimitRPS, b.RateLimitOrdersPerSec,
	))
}

type CreatedToken struct {
	Token domain.APIToken
	Raw   string
}

type CreateTokenInput struct {
	BrokerID      int64
	Name          string
	Description   string
	QuikFirm      string // FIX tag 5018
	ExpiresInDays *int   // nil or 0 = no expiry; otherwise 30/90/180/365
	ExpiresAt     *time.Time
	Permissions   []string
	CreatedBy     *uuid.UUID
	RotatedFrom   *uuid.UUID
}

func resolveExpiry(in CreateTokenInput) (*time.Time, error) {
	if in.ExpiresAt != nil {
		return in.ExpiresAt, nil
	}
	if in.ExpiresInDays == nil {
		// default 180 days
		t := time.Now().UTC().Add(180 * 24 * time.Hour)
		return &t, nil
	}
	days := *in.ExpiresInDays
	if days == 0 {
		return nil, nil
	}
	switch days {
	case 30, 90, 180, 365:
		t := time.Now().UTC().Add(time.Duration(days) * 24 * time.Hour)
		return &t, nil
	default:
		return nil, fmt.Errorf("expires_in_days must be 30, 90, 180, 365, or 0 (no expiry)")
	}
}

const tokenSelect = `
	SELECT id, broker_id, name, COALESCE(description, ''), token_hash, token_prefix, status,
	       permissions, COALESCE(quik_firm, ''), expires_at, created_at, revoked_at, last_used_at, last_used_ip,
	       created_by, revoked_by, rotated_from
	FROM api_tokens`

func scanToken(row pgx.Row) (domain.APIToken, error) {
	var t domain.APIToken
	var perms []byte
	err := row.Scan(
		&t.ID, &t.BrokerID, &t.Name, &t.Description, &t.TokenHash, &t.TokenPrefix, &t.Status,
		&perms, &t.QuikFirm, &t.ExpiresAt, &t.CreatedAt, &t.RevokedAt, &t.LastUsedAt, &t.LastUsedIP,
		&t.CreatedBy, &t.RevokedBy, &t.RotatedFrom,
	)
	if err != nil {
		return t, err
	}
	if len(perms) > 0 {
		_ = json.Unmarshal(perms, &t.Permissions)
	}
	if t.Permissions == nil {
		t.Permissions = []string{}
	}
	return t, nil
}

func (s *Store) CreateToken(ctx context.Context, brokerID int64, name string, expiresAt *time.Time) (CreatedToken, error) {
	return s.CreateTokenExt(ctx, CreateTokenInput{
		BrokerID:  brokerID,
		Name:      name,
		ExpiresAt: expiresAt,
	})
}

func (s *Store) CreateTokenExt(ctx context.Context, in CreateTokenInput) (CreatedToken, error) {
	if _, err := s.GetBroker(ctx, in.BrokerID); err != nil {
		return CreatedToken{}, err
	}
	if in.Name == "" {
		return CreatedToken{}, fmt.Errorf("name is required")
	}
	if strings.TrimSpace(in.QuikFirm) == "" {
		return CreatedToken{}, fmt.Errorf("quik_firm is required (код фирмы → FIX tag 5018)")
	}
	perms, ok := auth.NormalizePermissions(in.Permissions)
	if !ok {
		return CreatedToken{}, fmt.Errorf("invalid permissions")
	}
	expiresAt, err := resolveExpiry(in)
	if err != nil {
		return CreatedToken{}, err
	}
	raw, prefix, err := auth.GenerateAPIToken()
	if err != nil {
		return CreatedToken{}, err
	}
	hash := auth.HashAPIToken(raw, s.pepper)
	permsJSON, _ := json.Marshal(perms)
	t, err := scanToken(s.pool.QueryRow(ctx, `
		INSERT INTO api_tokens (
			broker_id, name, description, token_hash, token_prefix, expires_at,
			permissions, created_by, rotated_from, quik_firm
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		RETURNING id, broker_id, name, COALESCE(description, ''), token_hash, token_prefix, status,
		          permissions, COALESCE(quik_firm, ''), expires_at, created_at, revoked_at, last_used_at, last_used_ip,
		          created_by, revoked_by, rotated_from`,
		in.BrokerID, in.Name, in.Description, hash, prefix, expiresAt, permsJSON, in.CreatedBy, in.RotatedFrom, strings.TrimSpace(in.QuikFirm),
	))
	if err != nil {
		return CreatedToken{}, err
	}
	return CreatedToken{Token: t, Raw: raw}, nil
}

func (s *Store) ListTokens(ctx context.Context, brokerID int64) ([]domain.APIToken, error) {
	rows, err := s.pool.Query(ctx, tokenSelect+` WHERE broker_id=$1 ORDER BY created_at DESC`, brokerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.APIToken
	for rows.Next() {
		t, err := scanToken(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

type TokenWithBroker struct {
	Token      domain.APIToken
	BrokerCode string
	BrokerName string
}

func (s *Store) ListAllTokens(ctx context.Context) ([]TokenWithBroker, error) {
	return s.listTokensWithBroker(ctx, ` ORDER BY t.created_at DESC`)
}

func (s *Store) listTokensWithBroker(ctx context.Context, suffix string, args ...any) ([]TokenWithBroker, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT t.id, t.broker_id, t.name, COALESCE(t.description, ''), t.token_hash, t.token_prefix, t.status,
		       t.permissions, COALESCE(t.quik_firm, ''), t.expires_at, t.created_at, t.revoked_at, t.last_used_at, t.last_used_ip,
		       t.created_by, t.revoked_by, t.rotated_from, b.code, b.name
		FROM api_tokens t
		JOIN brokers b ON b.id = t.broker_id`+suffix, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TokenWithBroker
	for rows.Next() {
		var t domain.APIToken
		var perms []byte
		var code, name string
		err := rows.Scan(
			&t.ID, &t.BrokerID, &t.Name, &t.Description, &t.TokenHash, &t.TokenPrefix, &t.Status,
			&perms, &t.QuikFirm, &t.ExpiresAt, &t.CreatedAt, &t.RevokedAt, &t.LastUsedAt, &t.LastUsedIP,
			&t.CreatedBy, &t.RevokedBy, &t.RotatedFrom, &code, &name,
		)
		if err != nil {
			return nil, err
		}
		if len(perms) > 0 {
			_ = json.Unmarshal(perms, &t.Permissions)
		}
		if t.Permissions == nil {
			t.Permissions = []string{}
		}
		out = append(out, TokenWithBroker{Token: t, BrokerCode: code, BrokerName: name})
	}
	return out, rows.Err()
}

func (s *Store) GetToken(ctx context.Context, tokenID uuid.UUID) (domain.APIToken, error) {
	t, err := scanToken(s.pool.QueryRow(ctx, tokenSelect+` WHERE id=$1`, tokenID))
	if errors.Is(err, pgx.ErrNoRows) {
		return t, ErrNotFound
	}
	return t, err
}

// RevokeToken marks token REVOKED. Idempotent: already-revoked returns nil.
func (s *Store) RevokeToken(ctx context.Context, tokenID uuid.UUID) error {
	return s.RevokeTokenBy(ctx, tokenID, nil)
}

func (s *Store) RevokeTokenBy(ctx context.Context, tokenID uuid.UUID, revokedBy *uuid.UUID) error {
	t, err := s.GetToken(ctx, tokenID)
	if err != nil {
		return err
	}
	if t.Status == domain.TokenRevoked {
		return nil
	}
	_, err = s.pool.Exec(ctx, `
		UPDATE api_tokens SET status='REVOKED', revoked_at=now(), revoked_by=$2
		WHERE id=$1`, tokenID, revokedBy)
	return err
}

// RotateToken creates a new ACTIVE token copying settings from the old one.
// The previous token stays ACTIVE for 24h (or its existing sooner expiry), then expires.
func (s *Store) RotateToken(ctx context.Context, tokenID uuid.UUID, createdBy *uuid.UUID) (CreatedToken, error) {
	old, err := s.GetToken(ctx, tokenID)
	if err != nil {
		return CreatedToken{}, err
	}
	if old.Status != domain.TokenActive {
		return CreatedToken{}, fmt.Errorf("only ACTIVE tokens can be rotated")
	}
	name := old.Name + "-rotated"
	in := CreateTokenInput{
		BrokerID:    old.BrokerID,
		Name:        name,
		Description: old.Description,
		QuikFirm:    old.QuikFirm,
		Permissions: old.Permissions,
		CreatedBy:   createdBy,
		RotatedFrom: &old.ID,
	}
	if old.ExpiresAt != nil {
		remaining := time.Until(*old.ExpiresAt)
		if remaining < 24*time.Hour {
			remaining = 24 * time.Hour
		}
		t := time.Now().UTC().Add(remaining)
		in.ExpiresAt = &t
	} else {
		zero := 0
		in.ExpiresInDays = &zero
	}
	const rotateGrace = 24 * time.Hour
	created, err := s.CreateTokenExt(ctx, in)
	if err != nil {
		return CreatedToken{}, err
	}
	grace := time.Now().UTC().Add(rotateGrace)
	_, err = s.pool.Exec(ctx, `
		UPDATE api_tokens
		SET expires_at = CASE
			WHEN expires_at IS NULL OR expires_at > $2 THEN $2
			ELSE expires_at
		END
		WHERE id=$1 AND status='ACTIVE'`, old.ID, grace)
	if err != nil {
		return created, err
	}
	return created, nil
}

// UpdateTokenQuikFirm sets FIX firm code (tag 5018) on an existing token.
func (s *Store) UpdateTokenQuikFirm(ctx context.Context, tokenID uuid.UUID, quikFirm string) (domain.APIToken, error) {
	firm := strings.TrimSpace(quikFirm)
	if firm == "" {
		return domain.APIToken{}, fmt.Errorf("quik_firm is required")
	}
	t, err := scanToken(s.pool.QueryRow(ctx, `
		UPDATE api_tokens SET quik_firm=$2
		WHERE id=$1
		RETURNING id, broker_id, name, COALESCE(description, ''), token_hash, token_prefix, status,
		          permissions, COALESCE(quik_firm, ''), expires_at, created_at, revoked_at, last_used_at, last_used_ip,
		          created_by, revoked_by, rotated_from`,
		tokenID, firm,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return t, ErrNotFound
	}
	return t, err
}

// Infrastructure failures must not be counted as bad credentials or block an IP.
var ErrAuthenticationUnavailable = errors.New("authentication backend unavailable")

func (s *Store) AuthenticateToken(ctx context.Context, raw, ip string) (domain.Broker, domain.APIToken, error) {
	hash := auth.HashAPIToken(raw, s.pepper)
	t, err := scanToken(s.pool.QueryRow(ctx, tokenSelect+` WHERE token_hash=$1`, hash))
	if errors.Is(err, pgx.ErrNoRows) {
		// Preserve existing credentials until their normal expiry or rotation.
		legacyHash := auth.HashToken(raw, s.pepper)
		t, err = scanToken(s.pool.QueryRow(ctx, tokenSelect+` WHERE token_hash=$1`, legacyHash))
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Broker{}, t, fmt.Errorf("invalid token")
	}
	if err != nil {
		return domain.Broker{}, t, fmt.Errorf("%w: %w", ErrAuthenticationUnavailable, err)
	}
	if t.Status != domain.TokenActive {
		return domain.Broker{}, t, fmt.Errorf("token not active")
	}
	if t.ExpiresAt != nil && t.ExpiresAt.Before(time.Now()) {
		_, _ = s.pool.Exec(ctx, `UPDATE api_tokens SET status='EXPIRED' WHERE id=$1 AND status='ACTIVE'`, t.ID)
		t.Status = domain.TokenExpired
		return domain.Broker{}, t, fmt.Errorf("token expired")
	}
	b, err := s.GetBroker(ctx, t.BrokerID)
	if err != nil {
		return b, t, fmt.Errorf("%w: %w", ErrAuthenticationUnavailable, err)
	}
	if b.Status != domain.BrokerActive {
		return b, t, fmt.Errorf("broker not active")
	}
	if b.IPWhitelistEnabled {
		ok, err := s.IPAllowed(ctx, b.ID, ip)
		if err != nil {
			return b, t, fmt.Errorf("%w: %w", ErrAuthenticationUnavailable, err)
		}
		if !ok {
			return b, t, fmt.Errorf("ip not allowed")
		}
	}
	s.touchLastUsed(ctx, t.ID, ip)
	return b, t, nil
}

func (s *Store) touchLastUsed(ctx context.Context, tokenID uuid.UUID, ip string) {
	s.lastUsedMu.Lock()
	last, ok := s.lastUsed[tokenID]
	now := time.Now()
	if ok && now.Sub(last) < 45*time.Second {
		s.lastUsedMu.Unlock()
		return
	}
	s.lastUsed[tokenID] = now
	s.lastUsedMu.Unlock()
	_, _ = s.pool.Exec(ctx, `UPDATE api_tokens SET last_used_at=now(), last_used_ip=$2 WHERE id=$1`, tokenID, ip)
}

func (s *Store) IPAllowed(ctx context.Context, brokerID int64, ipStr string) (bool, error) {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT ip_cidr FROM broker_ip_whitelist WHERE broker_id=$1`, brokerID)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	hasAny := false
	for rows.Next() {
		var cidr string
		if err := rows.Scan(&cidr); err != nil {
			return false, err
		}
		hasAny = true
		if parsed := net.ParseIP(cidr); parsed != nil {
			if parsed.Equal(ip) {
				return true, nil
			}
			continue
		}
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		if network.Contains(ip) {
			return true, nil
		}
	}
	if !hasAny {
		return false, nil
	}
	return false, rows.Err()
}

func (s *Store) ListIPWhitelist(ctx context.Context, brokerID int64) ([]domain.BrokerIPAllow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, broker_id, ip_cidr, note, created_at
		FROM broker_ip_whitelist WHERE broker_id=$1 ORDER BY id`, brokerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.BrokerIPAllow
	for rows.Next() {
		var a domain.BrokerIPAllow
		if err := rows.Scan(&a.ID, &a.BrokerID, &a.IPCIDR, &a.Note, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) AddIPWhitelist(ctx context.Context, brokerID int64, ipCIDR, note string) (domain.BrokerIPAllow, error) {
	if net.ParseIP(ipCIDR) == nil {
		if _, _, err := net.ParseCIDR(ipCIDR); err != nil {
			return domain.BrokerIPAllow{}, fmt.Errorf("invalid IP or CIDR")
		}
	}
	var a domain.BrokerIPAllow
	err := s.pool.QueryRow(ctx, `
		INSERT INTO broker_ip_whitelist (broker_id, ip_cidr, note) VALUES ($1,$2,$3)
		RETURNING id, broker_id, ip_cidr, note, created_at`,
		brokerID, ipCIDR, note,
	).Scan(&a.ID, &a.BrokerID, &a.IPCIDR, &a.Note, &a.CreatedAt)
	return a, err
}

func (s *Store) RemoveIPWhitelist(ctx context.Context, brokerID, entryID int64) error {
	ct, err := s.pool.Exec(ctx, `DELETE FROM broker_ip_whitelist WHERE id=$1 AND broker_id=$2`, entryID, brokerID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) RecordAuthFailure(ctx context.Context, ip, endpoint, reason string, tokenID *uuid.UUID, brokerID *int64) {
	_, _ = s.pool.Exec(ctx, `
		INSERT INTO auth_failures (ip, endpoint, reason, token_id, broker_id)
		VALUES ($1,$2,$3,$4,$5)`, ip, endpoint, reason, tokenID, brokerID)
}

type SecurityStats struct {
	ActiveTokens          int64      `json:"active_tokens"`
	ExpiredTokens         int64      `json:"expired_tokens"`
	RevokedTokens         int64      `json:"revoked_tokens"`
	DisabledTokens        int64      `json:"disabled_tokens"`
	LastAPIActivity       *time.Time `json:"last_api_activity,omitempty"`
	LastUsedIP            *string    `json:"last_used_ip,omitempty"`
	FailedAuthAttempts24h int64      `json:"failed_auth_attempts_24h"`
	IPWhitelistEnabled    bool       `json:"ip_whitelist_enabled"`
	IPWhitelistCount      int64      `json:"ip_whitelist_count"`
	RateLimitRPS          int        `json:"rate_limit_rps"`
	RateLimitOrdersPerSec int        `json:"rate_limit_orders_per_sec"`
}

func (s *Store) SecurityStats(ctx context.Context, brokerID int64) (SecurityStats, error) {
	b, err := s.GetBroker(ctx, brokerID)
	if err != nil {
		return SecurityStats{}, err
	}
	var st SecurityStats
	st.IPWhitelistEnabled = b.IPWhitelistEnabled
	st.RateLimitRPS = b.RateLimitRPS
	st.RateLimitOrdersPerSec = b.RateLimitOrdersPerSec
	_ = s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM api_tokens WHERE broker_id=$1 AND status='ACTIVE'`, brokerID).Scan(&st.ActiveTokens)
	_ = s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM api_tokens WHERE broker_id=$1 AND status='EXPIRED'`, brokerID).Scan(&st.ExpiredTokens)
	_ = s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM api_tokens WHERE broker_id=$1 AND status='REVOKED'`, brokerID).Scan(&st.RevokedTokens)
	_ = s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM api_tokens WHERE broker_id=$1 AND status='DISABLED'`, brokerID).Scan(&st.DisabledTokens)
	_ = s.pool.QueryRow(ctx, `
		SELECT last_used_at, last_used_ip FROM api_tokens
		WHERE broker_id=$1 AND last_used_at IS NOT NULL
		ORDER BY last_used_at DESC LIMIT 1`, brokerID).Scan(&st.LastAPIActivity, &st.LastUsedIP)
	_ = s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM auth_failures
		WHERE broker_id=$1 AND created_at > now() - interval '24 hours'`, brokerID).Scan(&st.FailedAuthAttempts24h)
	_ = s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM broker_ip_whitelist WHERE broker_id=$1`, brokerID).Scan(&st.IPWhitelistCount)
	return st, nil
}
