package admin

import (
	"context"
	"errors"
	"time"

	"github.com/fix-gateway/backend/internal/auth"
	"github.com/fix-gateway/backend/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type UserStore struct {
	pool *pgxpool.Pool
}

func NewUserStore(pool *pgxpool.Pool) *UserStore {
	return &UserStore{pool: pool}
}

func (s *UserStore) EnsureAdmin(ctx context.Context, email, password string) error {
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `SELECT id FROM users WHERE email=$1`, email).Scan(&id)
	if err == nil {
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO users (email, password_hash, full_name, role_code)
		VALUES ($1,$2,'Administrator','SUPER_ADMIN')`, email, hash)
	return err
}

func (s *UserStore) Authenticate(ctx context.Context, email, password string) (domain.User, error) {
	var u domain.User
	err := s.pool.QueryRow(ctx, `
		SELECT id, email, password_hash, full_name, role_code, status, created_at, updated_at
		FROM users WHERE email=$1`, email,
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.FullName, &u.RoleCode, &u.Status, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return u, errors.New("invalid credentials")
	}
	if err != nil {
		return u, err
	}
	if u.Status != "ACTIVE" || !auth.CheckPassword(u.PasswordHash, password) {
		return u, errors.New("invalid credentials")
	}
	return u, nil
}

func (s *UserStore) List(ctx context.Context) ([]domain.User, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, email, password_hash, full_name, role_code, status, created_at, updated_at
		FROM users ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.User
	for rows.Next() {
		var u domain.User
		if err := rows.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.FullName, &u.RoleCode, &u.Status, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, err
		}
		u.PasswordHash = ""
		out = append(out, u)
	}
	return out, rows.Err()
}

func IssueToken(secret string, u domain.User) (string, error) {
	return auth.IssueAdminJWT(secret, u.ID, u.Email, u.RoleCode, 12*time.Hour, u.UpdatedAt.UnixMicro())
}

func (s *UserStore) AuthenticateSession(ctx context.Context, secret, token string) (*auth.AdminClaims, error) {
	claims, err := auth.ParseAdminJWT(secret, token)
	if err != nil {
		return nil, err
	}
	if err := s.ValidateSession(ctx, claims.UserID, claims.RoleCode, claims.UserVersion); err != nil {
		return nil, err
	}
	return claims, nil
}

// ValidateSession revokes sessions on status, role, password or version changes.
func (s *UserStore) ValidateSession(ctx context.Context, id, role string, version int64) error {
	if s == nil || s.pool == nil || version == 0 {
		return errors.New("invalid session")
	}
	var currentRole, status string
	var updated time.Time
	err := s.pool.QueryRow(ctx, `SELECT role_code, status, updated_at FROM users WHERE id=$1`, id).Scan(&currentRole, &status, &updated)
	if err != nil {
		return err
	}
	if status != "ACTIVE" || currentRole != role || updated.UnixMicro() != version {
		return errors.New("session revoked")
	}
	return nil
}
