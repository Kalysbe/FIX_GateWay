package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/fix-gateway/backend/internal/admin"
	"github.com/fix-gateway/backend/internal/config"
	"github.com/fix-gateway/backend/internal/platform/db"
	"io"
	"os"
	"strings"
	"time"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "password rotation failed:", err)
		os.Exit(1)
	}
	fmt.Println("Administrator password rotated; previous sessions revoked.")
}
func run() error {
	path := flag.String("new-password-file", "", "private file with new password (never pass a password as an argument)")
	email := flag.String("email", "", "super administrator email; defaults to ADMIN_EMAIL")
	flag.Parse()
	if *path == "" {
		return fmt.Errorf("-new-password-file required")
	}
	f, err := os.Open(*path)
	if err != nil {
		return fmt.Errorf("password file unavailable")
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 74))
	if err != nil {
		return fmt.Errorf("password file unreadable")
	}
	password := strings.TrimRight(string(b), "\r\n")
	if len(password) < 12 || len(password) > 72 || strings.ContainsAny(password, "\r\n\x00") {
		return fmt.Errorf("password must contain 12 to 72 bytes on one line")
	}
	cfg := config.Load()
	cfg.AdminPassword = password
	if err := cfg.Validate(); err != nil {
		return err
	}
	if *email == "" {
		*email = cfg.AdminEmail
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("database connection unavailable")
	}
	defer pool.Close()
	if err := admin.NewUserStore(pool).RotatePassword(ctx, *email, password); err != nil {
		return err
	}
	return nil
}
