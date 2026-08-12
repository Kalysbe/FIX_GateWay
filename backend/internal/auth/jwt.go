package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type AdminClaims struct {
	UserID   string `json:"uid"`
	Email    string `json:"email"`
	RoleCode string `json:"role"`
	jwt.RegisteredClaims
}

func IssueAdminJWT(secret string, userID uuid.UUID, email, role string, ttl time.Duration) (string, error) {
	claims := AdminClaims{
		UserID:   userID.String(),
		Email:    email,
		RoleCode: role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Subject:   userID.String(),
		},
	}
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return t.SignedString([]byte(secret))
}

func ParseAdminJWT(secret, token string) (*AdminClaims, error) {
	parsed, err := jwt.ParseWithClaims(token, &AdminClaims{}, func(t *jwt.Token) (any, error) {
		if t.Method != jwt.SigningMethodHS256 {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(secret), nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := parsed.Claims.(*AdminClaims)
	if !ok || !parsed.Valid {
		return nil, errors.New("invalid token")
	}
	return claims, nil
}
