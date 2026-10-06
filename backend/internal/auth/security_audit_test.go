package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func TestSecurityAuditJWTRejectsExpiredAndWrongSecret(t *testing.T) {
	expired, err := IssueAdminJWT("audit-secret", uuid.New(), "audit@example.test", RoleAdmin, -time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseAdminJWT("audit-secret", expired); err == nil {
		t.Fatal("expired JWT accepted")
	}
	valid, err := IssueAdminJWT("audit-secret", uuid.New(), "audit@example.test", RoleAdmin, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseAdminJWT("wrong-secret", valid); err == nil {
		t.Fatal("wrong key accepted")
	}
}

func TestSecurityAuditJWTWithoutExpiryRejected(t *testing.T) {
	id := uuid.NewString()
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, AdminClaims{UserID: id, RoleCode: RoleAdmin, RegisteredClaims: jwt.RegisteredClaims{Subject: id, IssuedAt: jwt.NewNumericDate(time.Now())}}).SignedString([]byte("audit-secret"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseAdminJWT("audit-secret", signed); err == nil {
		t.Fatal(err)
	}
	// Requires a valid signing key; not an unauthenticated signature bypass.
}
