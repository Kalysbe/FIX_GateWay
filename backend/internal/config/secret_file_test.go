package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSecretFilesFailClosedAndDoNotLeakValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jwt")
	secret := "file-secret-abcdefghijklmnopqrstuvwxyz-012345"
	if err := os.WriteFile(path, []byte(secret+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("JWT_SECRET", "")
	t.Setenv("JWT_SECRET_FILE", path)
	cfg := Load()
	if cfg.JWTSecret != secret || cfg.Validate() != nil {
		t.Fatal("file secret not loaded")
	}
	t.Setenv("JWT_SECRET", "conflicting-value")
	if err := Load().Validate(); err == nil || strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "conflicting-value") {
		t.Fatal("conflicting secret accepted or leaked")
	}
	t.Setenv("JWT_SECRET", "")
	t.Setenv("JWT_SECRET_FILE", path+"-missing")
	if err := Load().Validate(); err == nil {
		t.Fatal("missing secret silently fell back to default")
	}
}
