package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	HTTPAddr                 string
	DatabaseURL              string
	QuikExportDatabaseURL    string
	JWTSecret                string
	APITokenPepper           string
	AdminEmail               string
	AdminPassword            string
	LBStrategy               string
	OutboxPollInterval       time.Duration
	OutboxBatchSize          int
	HealthInterval           time.Duration
	WorkerID                 string
	CORSOrigins              []string
	MigrationsPath           string
	AdapterHostOverride      string
	RequireHTTPS             bool
	HMACRequired             bool
	DefaultRateLimitRPS      int
	DefaultOrderRateLimit    int
	AuthFailMaxPerMinute     int
	AppEnv                   string
	PositionSyncInterval     time.Duration // 0 disables background FIX position sync
	MarketDataSyncInterval   time.Duration // 0 disables background FIX market data sync
	TrustedProxies           string
	ObsFastMS                int
	ObsNormalMS              int
	ObsSlowMS                int
	ObsChannelSize           int
	ObsFixRawLogs            bool
	ObsAPIRetentionDays      int
	ObsBusinessRetentionDays int
	ObsAuthRetentionDays     int
	ObsAuditRetentionDays    int
	ObsDebugRetentionDays    int
	ObsErrorRetentionDays    int
	ObsSystemRetentionDays   int
	ObsAlertWindow           time.Duration
	ObsErrorWarnPct          float64
	ObsErrorErrorPct         float64
	ObsErrorCritPct          float64
	ObsLatencyWarnMS         int
	ObsLatencyErrorMS        int
	ObsSkipOrigins           []string
}

func Load() Config {
	return Config{
		HTTPAddr:                 getEnv("HTTP_ADDR", ":8080"),
		DatabaseURL:              getEnv("DATABASE_URL", "postgres://postgres:123@localhost:5432/fix_gateway?sslmode=disable"),
		QuikExportDatabaseURL:    getEnv("QUIK_EXPORT_DATABASE_URL", "postgres://postgres:123@192.168.0.89:5432/quik_export2?sslmode=disable"),
		JWTSecret:                getEnv("JWT_SECRET", "dev-jwt-secret-change-me"),
		APITokenPepper:           getEnv("API_TOKEN_PEPPER", "dev-token-pepper-change-me"),
		AdminEmail:               getEnv("ADMIN_EMAIL", "admin@localhost"),
		AdminPassword:            getEnv("ADMIN_PASSWORD", "admin123"),
		LBStrategy:               getEnv("LB_STRATEGY", "least_connections"),
		OutboxPollInterval:       getDuration("OUTBOX_POLL_INTERVAL", 500*time.Millisecond),
		OutboxBatchSize:          getInt("OUTBOX_BATCH_SIZE", 20),
		HealthInterval:           getDuration("HEALTH_INTERVAL", 5*time.Second),
		WorkerID:                 getEnv("WORKER_ID", "gateway-1"),
		CORSOrigins:              strings.Split(getEnv("CORS_ORIGINS", "http://localhost:5173,http://localhost:3000"), ","),
		MigrationsPath:           getEnv("MIGRATIONS_PATH", "migrations"),
		AdapterHostOverride:      getEnv("ADAPTER_HOST_OVERRIDE", ""),
		RequireHTTPS:             getBool("REQUIRE_HTTPS", false),
		HMACRequired:             getBool("HMAC_REQUIRED", false),
		DefaultRateLimitRPS:      getInt("DEFAULT_RATE_LIMIT_RPS", 100),
		DefaultOrderRateLimit:    getInt("DEFAULT_ORDER_RATE_LIMIT", 20),
		AuthFailMaxPerMinute:     getInt("AUTH_FAIL_MAX_PER_MINUTE", 30),
		AppEnv:                   getEnv("APP_ENV", "development"),
		PositionSyncInterval:     getDuration("POSITION_SYNC_INTERVAL", 30*time.Second),
		MarketDataSyncInterval:   getDuration("MARKET_DATA_SYNC_INTERVAL", 15*time.Second),
		TrustedProxies:           getEnv("TRUSTED_PROXIES", ""),
		ObsFastMS:                getInt("OBS_FAST_MS", 100),
		ObsNormalMS:              getInt("OBS_NORMAL_MS", 500),
		ObsSlowMS:                getInt("OBS_SLOW_MS", 2000),
		ObsChannelSize:           getInt("OBS_CHANNEL_SIZE", 4096),
		ObsFixRawLogs:            getBool("OBS_FIX_RAW_LOGS", false),
		ObsAPIRetentionDays:      getInt("OBS_API_RETENTION_DAYS", 15),
		ObsBusinessRetentionDays: getInt("OBS_BUSINESS_RETENTION_DAYS", 15),
		ObsAuthRetentionDays:     getInt("OBS_AUTH_RETENTION_DAYS", 15),
		ObsAuditRetentionDays:    getInt("OBS_AUDIT_RETENTION_DAYS", 15),
		ObsDebugRetentionDays:    getInt("OBS_DEBUG_RETENTION_DAYS", 15),
		ObsErrorRetentionDays:    getInt("OBS_ERROR_RETENTION_DAYS", 15),
		ObsSystemRetentionDays:   getInt("OBS_SYSTEM_RETENTION_DAYS", 15),
		ObsAlertWindow:           getDuration("OBS_ALERT_WINDOW", time.Minute),
		ObsErrorWarnPct:          getFloat("OBS_ERROR_WARN_PCT", 5),
		ObsErrorErrorPct:         getFloat("OBS_ERROR_ERROR_PCT", 10),
		ObsErrorCritPct:          getFloat("OBS_ERROR_CRIT_PCT", 20),
		ObsLatencyWarnMS:         getInt("OBS_LATENCY_WARN_MS", 1000),
		ObsLatencyErrorMS:        getInt("OBS_LATENCY_ERROR_MS", 3000),
		ObsSkipOrigins:           strings.Split(getEnv("OBS_SKIP_ORIGINS", "http://localhost:5173,http://127.0.0.1:5173"), ","),
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

func getFloat(key string, def float64) float64 {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return n
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
