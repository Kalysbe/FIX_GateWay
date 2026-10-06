package config

import (
	"errors"
	"github.com/jackc/pgx/v5"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	FIXSessionDir            string
	PrometheusURL            string
	HTTPAddr                 string
	APIReadConcurrency       int
	APITradeConcurrency      int
	APIRequestTimeout        time.Duration
	RateLimitMaxStates       int
	GRPCAddr                 string
	GRPCCertFile             string
	GRPCKeyFile              string
	GRPCCAFile               string
	GRPCRequireClientCert    bool
	GRPCClientCertFile       string
	GRPCClientKeyFile        string
	GRPCTarget               string
	GRPCServerName           string
	DatabaseURL              string
	QuikExportDatabaseURL    string
	QuikAPIBaseURL           string
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
	MarketDataBoards         []string      // QUIK class codes for 35=V (tag 100)
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
	AllowInsecureDB          bool
	AllowInsecureHTTP        bool
	AllowInsecureHMAC        bool
	DocsPublic               bool
}

func Load() Config {
	appEnv := getEnv("APP_ENV", "development")
	prod := isProdEnv(appEnv)
	return Config{
		FIXSessionDir:            getEnv("FIX_SESSION_DIR", filepath.Join(os.TempDir(), "fix-gateway-sessions")),
		PrometheusURL:            getEnv("PROMETHEUS_URL", ""),
		HTTPAddr:                 getEnv("HTTP_ADDR", ":8080"),
		APIReadConcurrency:       getInt("API_READ_CONCURRENCY", 32),
		APITradeConcurrency:      getInt("API_TRADE_CONCURRENCY", 16),
		APIRequestTimeout:        getDuration("API_REQUEST_TIMEOUT", 15*time.Second),
		GRPCAddr:                 getEnv("GRPC_ADDR", ":8443"),
		GRPCCertFile:             getEnv("GRPC_TLS_CERT", "certs/server.crt"),
		GRPCKeyFile:              getEnv("GRPC_TLS_KEY", "certs/server.key"),
		GRPCCAFile:               getEnv("GRPC_TLS_CA", "certs/ca.crt"),
		GRPCRequireClientCert:    getBool("GRPC_MTLS_REQUIRED", true),
		GRPCClientCertFile:       getEnv("GRPC_CLIENT_CERT", "certs/bridge.crt"),
		GRPCClientKeyFile:        getEnv("GRPC_CLIENT_KEY", "certs/bridge.key"),
		GRPCTarget:               getEnv("GRPC_TARGET", "localhost:8443"),
		GRPCServerName:           getEnv("GRPC_SERVER_NAME", "localhost"),
		DatabaseURL:              getEnv("DATABASE_URL", "postgres://postgres:123@localhost:5432/fix_gateway?sslmode=disable"),
		QuikExportDatabaseURL:    getEnv("QUIK_EXPORT_DATABASE_URL", ""),
		QuikAPIBaseURL:           getEnv("QUIK_API_BASE_URL", "http://192.168.0.89:5000"),
		JWTSecret:                getEnv("JWT_SECRET", "dev-jwt-secret-change-me"),
		APITokenPepper:           getEnv("API_TOKEN_PEPPER", "dev-token-pepper-change-me"),
		AdminEmail:               getEnv("ADMIN_EMAIL", "admin@localhost"),
		AdminPassword:            getEnv("ADMIN_PASSWORD", "admin123"),
		LBStrategy:               getEnv("LB_STRATEGY", "least_connections"),
		OutboxPollInterval:       getDuration("OUTBOX_POLL_INTERVAL", 500*time.Millisecond),
		OutboxBatchSize:          getInt("OUTBOX_BATCH_SIZE", 20),
		HealthInterval:           getDuration("HEALTH_INTERVAL", 5*time.Second),
		WorkerID:                 getEnv("WORKER_ID", "gateway-1"),
		CORSOrigins:              strings.Split(getEnv("CORS_ORIGINS", "http://localhost:5173,http://localhost:3000,http://localhost:5174"), ","),
		MigrationsPath:           getEnv("MIGRATIONS_PATH", "migrations"),
		AdapterHostOverride:      getEnv("ADAPTER_HOST_OVERRIDE", ""),
		RequireHTTPS:             getBool("REQUIRE_HTTPS", false),
		HMACRequired:             getBool("HMAC_REQUIRED", false),
		DefaultRateLimitRPS:      getInt("DEFAULT_RATE_LIMIT_RPS", 100),
		RateLimitMaxStates:       getInt("RATE_LIMIT_MAX_STATES", 100000),
		DefaultOrderRateLimit:    getInt("DEFAULT_ORDER_RATE_LIMIT", 20),
		AuthFailMaxPerMinute:     getInt("AUTH_FAIL_MAX_PER_MINUTE", 30),
		AppEnv:                   appEnv,
		PositionSyncInterval:     getDuration("POSITION_SYNC_INTERVAL", 30*time.Second),
		MarketDataSyncInterval:   getDuration("MARKET_DATA_SYNC_INTERVAL", 15*time.Second),
		MarketDataBoards:         getCSV("MARKET_DATA_BOARDS", "LISTEDSTOCKK,LIST_GBA"),
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
		AllowInsecureDB:          getBool("ALLOW_INSECURE_DB", false),
		AllowInsecureHTTP:        getBool("ALLOW_INSECURE_HTTP", false),
		AllowInsecureHMAC:        getBool("ALLOW_INSECURE_HMAC", false),
		DocsPublic:               getBool("DOCS_PUBLIC", !prod),
	}
}

func isProdEnv(appEnv string) bool {
	switch strings.ToLower(strings.TrimSpace(appEnv)) {
	case "production", "prod":
		return true
	default:
		return false
	}
}

func (c Config) IsProduction() bool {
	return isProdEnv(c.AppEnv)
}

func isWeakSecret(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "change-me", "change-me-in-production", "dev-jwt-secret-change-me", "dev-token-pepper-change-me":
		return true
	default:
		return false
	}
}

// Validate refuses to boot a production process on documented defaults.
func (c Config) Validate() error {
	if !c.IsProduction() {
		return nil
	}
	var errs []string
	if isWeakSecret(c.JWTSecret) || len(c.JWTSecret) < 32 {
		errs = append(errs, "JWT_SECRET must be a unique value of at least 32 characters")
	}
	if isWeakSecret(c.APITokenPepper) || len(c.APITokenPepper) < 32 {
		errs = append(errs, "API_TOKEN_PEPPER must be a unique value of at least 32 characters")
	}
	if c.AdminPassword == "admin123" || len(c.AdminPassword) < 12 {
		errs = append(errs, "ADMIN_PASSWORD must be at least 12 characters and not the documented default")
	}
	if !c.AllowInsecureDB {
		parsed, err := pgx.ParseConfig(c.DatabaseURL)
		secure := err == nil && parsed.TLSConfig != nil
		if secure {
			for _, fallback := range parsed.Fallbacks {
				if fallback.TLSConfig == nil {
					secure = false
					break
				}
			}
		}
		if !secure {
			errs = append(errs, "DATABASE_URL must enforce TLS without plaintext fallback (set ALLOW_INSECURE_DB=true to override)")
		}
	}
	if !c.RequireHTTPS && !c.AllowInsecureHTTP {
		errs = append(errs, "REQUIRE_HTTPS must be true in production (set ALLOW_INSECURE_HTTP=true to override)")
	}
	if !c.HMACRequired && !c.AllowInsecureHMAC {
		errs = append(errs, "HMAC_REQUIRED must be true in production (set ALLOW_INSECURE_HMAC=true to override)")
	}
	if len(errs) == 0 {
		return nil
	}
	return errors.New(strings.Join(errs, "; "))
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getCSV(key, def string) []string {
	var out []string
	seen := map[string]bool{}
	for _, p := range strings.Split(getEnv(key, def), ",") {
		p = strings.TrimSpace(p)
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
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
