package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	HTTPAddr              string
	DatabaseURL           string
	JWTSecret             string
	APITokenPepper        string
	AdminEmail            string
	AdminPassword         string
	LBStrategy            string
	OutboxPollInterval    time.Duration
	OutboxBatchSize       int
	HealthInterval        time.Duration
	WorkerID              string
	CORSOrigins           []string
	MigrationsPath        string
	AdapterHostOverride   string
	RequireHTTPS          bool
	HMACRequired          bool
	DefaultRateLimitRPS   int
	DefaultOrderRateLimit int
	AuthFailMaxPerMinute  int
	AppEnv                string
}

func Load() Config {
	return Config{
		HTTPAddr:              getEnv("HTTP_ADDR", ":8080"),
		DatabaseURL:           getEnv("DATABASE_URL", "postgres://postgres:123@localhost:5432/fix_gateway?sslmode=disable"),
		JWTSecret:             getEnv("JWT_SECRET", "dev-jwt-secret-change-me"),
		APITokenPepper:        getEnv("API_TOKEN_PEPPER", "dev-token-pepper-change-me"),
		AdminEmail:            getEnv("ADMIN_EMAIL", "admin@localhost"),
		AdminPassword:         getEnv("ADMIN_PASSWORD", "admin123"),
		LBStrategy:            getEnv("LB_STRATEGY", "least_connections"),
		OutboxPollInterval:    getDuration("OUTBOX_POLL_INTERVAL", 500*time.Millisecond),
		OutboxBatchSize:       getInt("OUTBOX_BATCH_SIZE", 20),
		HealthInterval:        getDuration("HEALTH_INTERVAL", 5*time.Second),
		WorkerID:              getEnv("WORKER_ID", "gateway-1"),
		CORSOrigins:           strings.Split(getEnv("CORS_ORIGINS", "http://localhost:5173,http://localhost:3000"), ","),
		MigrationsPath:        getEnv("MIGRATIONS_PATH", "migrations"),
		AdapterHostOverride:   getEnv("ADAPTER_HOST_OVERRIDE", ""),
		RequireHTTPS:          getBool("REQUIRE_HTTPS", false),
		HMACRequired:          getBool("HMAC_REQUIRED", false),
		DefaultRateLimitRPS:   getInt("DEFAULT_RATE_LIMIT_RPS", 100),
		DefaultOrderRateLimit: getInt("DEFAULT_ORDER_RATE_LIMIT", 20),
		AuthFailMaxPerMinute:  getInt("AUTH_FAIL_MAX_PER_MINUTE", 30),
		AppEnv:                getEnv("APP_ENV", "development"),
	}
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func getBool(key string, def bool) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	if v == "" {
		return def
	}
	switch v {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return def
	}
}

func getDuration(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}
