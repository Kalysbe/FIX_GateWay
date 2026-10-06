package config

import "testing"

func TestProductionRejectsDatabasePlaintextFallback(t *testing.T) {
	for _, dsn := range []string{"postgres://x:y@db/db", "postgres://x:y@db/db?sslmode=prefer", "postgres://x:y@db/db?sslmode=allow", "postgres://x:y@db/db?sslmode=%64isable"} {
		c := Config{AppEnv: "production", JWTSecret: "abcdefghijklmnopqrstuvwxyz012345", APITokenPepper: "abcdefghijklmnopqrstuvwxyz012345", AdminPassword: "not-the-default-pw", DatabaseURL: dsn, RequireHTTPS: true, HMACRequired: true}
		if err := c.Validate(); err == nil {
			t.Fatalf("accepted plaintext-capable DSN %q", dsn)
		}
	}
}

func TestValidateSkipsDevelopment(t *testing.T) {
	c := Config{AppEnv: "development", JWTSecret: "change-me", AdminPassword: "admin123"}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestFIXSessionDirectoryConfigured(t *testing.T) {
	t.Setenv("FIX_SESSION_DIR", "/persistent/fix")
	if got := Load().FIXSessionDir; got != "/persistent/fix" {
		t.Fatalf("session directory ignored: %q", got)
	}
}

func TestValidateProductionRejectsDefaults(t *testing.T) {
	c := Config{
		AppEnv:         "production",
		JWTSecret:      "change-me",
		APITokenPepper: "dev-token-pepper-change-me",
		AdminPassword:  "admin123",
		DatabaseURL:    "postgres://x:y@db/db?sslmode=disable",
	}
	if err := c.Validate(); err == nil {
		t.Fatal("expected production validation error")
	}
}

func TestValidateProductionAcceptsStrongConfig(t *testing.T) {
	c := Config{
		AppEnv:         "production",
		JWTSecret:      "abcdefghijklmnopqrstuvwxyz012345",
		APITokenPepper: "abcdefghijklmnopqrstuvwxyz012345",
		AdminPassword:  "not-the-default-pw",
		DatabaseURL:    "postgres://x:y@db/db?sslmode=require",
		RequireHTTPS:   true,
		HMACRequired:   true,
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestValidateProductionRequiresHMAC(t *testing.T) {
	c := Config{
		AppEnv:         "production",
		JWTSecret:      "abcdefghijklmnopqrstuvwxyz012345",
		APITokenPepper: "abcdefghijklmnopqrstuvwxyz012345",
		AdminPassword:  "not-the-default-pw",
		DatabaseURL:    "postgres://x:y@db/db?sslmode=require",
		RequireHTTPS:   true,
	}
	if err := c.Validate(); err == nil {
		t.Fatal("expected HMAC_REQUIRED error")
	}
	c.AllowInsecureHMAC = true
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestGetCSVDefaultBoards(t *testing.T) {
	got := getCSV("MARKET_DATA_BOARDS_MISSING_"+t.Name(), "LISTEDSTOCKK,LIST_GBA")
	if len(got) != 2 || got[0] != "LISTEDSTOCKK" || got[1] != "LIST_GBA" {
		t.Fatalf("got %#v", got)
	}
}

func TestQuikAPIBaseURLDefault(t *testing.T) {
	t.Setenv("QUIK_API_BASE_URL", "")
	c := Load()
	if c.QuikAPIBaseURL != "http://192.168.0.89:5000" {
		t.Fatalf("got %q", c.QuikAPIBaseURL)
	}
}

func TestQuikAPIBaseURLFromEnv(t *testing.T) {
	t.Setenv("QUIK_API_BASE_URL", "http://10.0.0.5:5000")
	c := Load()
	if c.QuikAPIBaseURL != "http://10.0.0.5:5000" {
		t.Fatalf("got %q", c.QuikAPIBaseURL)
	}
}

func TestIsProduction(t *testing.T) {
	if !(Config{AppEnv: "prod"}).IsProduction() {
		t.Fatal("prod")
	}
	if (Config{AppEnv: "development"}).IsProduction() {
		t.Fatal("dev")
	}
}
