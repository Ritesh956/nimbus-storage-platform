// Package config loads and validates process configuration from environment
// variables. Fail-fast: Load returns an error rather than letting a service
// start with an invalid/missing setting (see docs/03-hld.md §2).
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type StorageNode struct {
	ID             string
	Endpoint       string
	PublicEndpoint string
}

type Config struct {
	Env      string // "dev" | "prod"
	HTTPPort string
	// CORSOrigin is the browser origin allowed to call this API (see
	// httpserver.CORS). Defaults to "*" — safe here since auth is a Bearer
	// token, not cookies (docs/03-hld.md §2).
	CORSOrigin string

	PostgresDSN string
	RedisAddr   string
	NATSURL     string

	JWTSecret string
	// JWTSecretPrevious lets an access token signed under a just-rotated-out
	// secret keep verifying until it naturally expires (≤AccessTokenTTL,
	// 15min by default) — without it, rotating NIMBUS_JWT_SECRET (the audit's
	// §04 gap: one static secret, no rotation path) instantly invalidates
	// every live access token cluster-wide, forcing a synchronized re-login.
	// Empty disables the fallback entirely (the pre-rotation-support
	// behavior). Only auth.Service.VerifyAccessToken/PeekUserID ever try it;
	// new tokens are always issued under JWTSecret.
	JWTSecretPrevious  string
	AccessTokenTTL     time.Duration
	RefreshTokenTTL    time.Duration
	TrashRetentionDays int

	// SMTPAddr is the transactional-mail relay (backlog #14 — Mailpit in
	// the compose stack). Empty disables email entirely; password-reset
	// links are logged instead of sent. SMTPFrom is the bare sender
	// address; WebBaseURL is the frontend origin reset links point at.
	SMTPAddr   string
	SMTPFrom   string
	WebBaseURL string

	// AdminEmail/AdminPassword seed the single platform-admin account at
	// api boot (see auth.Repository.EnsureSeededAdmin). This is the only
	// way to gain is_platform_admin=true — there is deliberately no
	// email-list mechanism that could let an arbitrary signup become an
	// admin. Both are required; existing accounts are never demoted by
	// omitting them (see EnsureSeededAdmin's ON CONFLICT semantics).
	AdminEmail    string
	AdminPassword string

	ChunkSizeBytes    int64
	ReplicationFactor int // N
	WriteQuorum       int // W

	// StorageSlowThreshold is how high a node's rolling health-probe
	// latency EWMA has to climb before Router.Resolve treats it as a
	// second-tier placement choice — still eligible, but only used to fill
	// a shortfall the faster healthy nodes couldn't (audit §02: "no
	// capacity/latency signal folds into placement" was the named gap).
	// 0 disables the distinction entirely (every healthy node is first-tier,
	// the pre-fix behavior).
	StorageSlowThreshold time.Duration

	// MaxUploadBytes / OrgQuotaBytes enforce upload caps and per-org
	// storage quotas (post-v1 backlog #8). 0 disables the check.
	MaxUploadBytes int64
	OrgQuotaBytes  int64

	// RateLimitRPS / RateLimitBurst parameterize the per-caller token
	// bucket (docs/04-lld.md §4). RPS 0 disables rate limiting entirely.
	RateLimitRPS   int
	RateLimitBurst int

	// LoginRateLimitRPS / LoginRateLimitBurst gate a second, much tighter
	// bucket keyed by IP alone and applied only to POST /v1/auth/login and
	// /v1/auth/login/totp, independent of the general per-caller limiter
	// above. Those two routes run before authentication exists, so the
	// general limiter's per-user bucket never applies to them, and its
	// generous default (25rps/burst 50) is cheap enough to make online
	// password/TOTP guessing against a single account viable. RPS 0
	// disables this bucket entirely (falls back to the general limiter,
	// if any).
	LoginRateLimitRPS   int
	LoginRateLimitBurst int

	// GCInterval is how often nimbus-worker's chunk sweeper ticks; GCGrace
	// is both how long a chunk must be unreferenced-and-unseen before it's
	// doomed and how long it must stay doomed before its bytes are deleted
	// (docs/07-distributed-architecture.md §6). GCGrace should stay well
	// above the 15-minute presign expiry — a client told "chunk exists" must
	// get to finish its session before the chunk can be reaped. Interval 0
	// disables GC entirely.
	GCInterval time.Duration
	GCGrace    time.Duration

	StorageNodes   []StorageNode
	MinIOAccessKey string
	MinIOSecretKey string
}

func Load() (Config, error) {
	cfg := Config{
		Env:                 getEnv("NIMBUS_ENV", "dev"),
		HTTPPort:            getEnv("NIMBUS_HTTP_PORT", "8080"),
		CORSOrigin:          getEnv("NIMBUS_CORS_ORIGIN", "*"),
		PostgresDSN:         os.Getenv("NIMBUS_POSTGRES_DSN"),
		RedisAddr:           getEnv("NIMBUS_REDIS_ADDR", "localhost:6379"),
		NATSURL:             getEnv("NIMBUS_NATS_URL", "nats://localhost:4222"),
		JWTSecret:           os.Getenv("NIMBUS_JWT_SECRET"),
		JWTSecretPrevious:   os.Getenv("NIMBUS_JWT_SECRET_PREVIOUS"),
		SMTPAddr:            os.Getenv("NIMBUS_SMTP_ADDR"),
		SMTPFrom:            getEnv("NIMBUS_SMTP_FROM", "no-reply@nimbus.dev"),
		WebBaseURL:          getEnv("NIMBUS_WEB_BASE_URL", "http://localhost:3000"),
		MinIOAccessKey:      os.Getenv("NIMBUS_MINIO_ACCESS_KEY"),
		MinIOSecretKey:      os.Getenv("NIMBUS_MINIO_SECRET_KEY"),
		AdminEmail:          strings.ToLower(strings.TrimSpace(os.Getenv("NIMBUS_ADMIN_EMAIL"))),
		AdminPassword:       os.Getenv("NIMBUS_ADMIN_PASSWORD"),
		TrashRetentionDays:  30,
		ChunkSizeBytes:      8 * 1024 * 1024, // 8 MiB, docs/02-system-design.md §2.1
		ReplicationFactor:   2,
		WriteQuorum:         2,
		MaxUploadBytes:      100 * 1024 * 1024,       // 100 MiB per file
		OrgQuotaBytes:       10 * 1024 * 1024 * 1024, // 10 GiB per org
		RateLimitRPS:        25,
		RateLimitBurst:      50,
		LoginRateLimitRPS:   1,
		LoginRateLimitBurst: 5,
	}

	var err error
	if cfg.AccessTokenTTL, err = getDuration("NIMBUS_ACCESS_TOKEN_TTL", 15*time.Minute); err != nil {
		return Config{}, err
	}
	if cfg.RefreshTokenTTL, err = getDuration("NIMBUS_REFRESH_TOKEN_TTL", 7*24*time.Hour); err != nil {
		return Config{}, err
	}
	if cfg.GCInterval, err = getDuration("NIMBUS_GC_INTERVAL", 10*time.Minute); err != nil {
		return Config{}, err
	}
	if cfg.GCGrace, err = getDuration("NIMBUS_GC_GRACE", time.Hour); err != nil {
		return Config{}, err
	}
	if cfg.StorageSlowThreshold, err = getDuration("NIMBUS_STORAGE_SLOW_THRESHOLD", 200*time.Millisecond); err != nil {
		return Config{}, err
	}
	if v := os.Getenv("NIMBUS_TRASH_RETENTION_DAYS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return Config{}, fmt.Errorf("NIMBUS_TRASH_RETENTION_DAYS: %w", err)
		}
		cfg.TrashRetentionDays = n
	}
	if v := os.Getenv("NIMBUS_CHUNK_SIZE_BYTES"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return Config{}, fmt.Errorf("NIMBUS_CHUNK_SIZE_BYTES: %w", err)
		}
		cfg.ChunkSizeBytes = n
	}
	if v := os.Getenv("NIMBUS_MAX_UPLOAD_BYTES"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return Config{}, fmt.Errorf("NIMBUS_MAX_UPLOAD_BYTES: %w", err)
		}
		cfg.MaxUploadBytes = n
	}
	if v := os.Getenv("NIMBUS_ORG_QUOTA_BYTES"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return Config{}, fmt.Errorf("NIMBUS_ORG_QUOTA_BYTES: %w", err)
		}
		cfg.OrgQuotaBytes = n
	}
	if v := os.Getenv("NIMBUS_RATE_LIMIT_RPS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return Config{}, fmt.Errorf("NIMBUS_RATE_LIMIT_RPS: %w", err)
		}
		cfg.RateLimitRPS = n
	}
	if v := os.Getenv("NIMBUS_RATE_LIMIT_BURST"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return Config{}, fmt.Errorf("NIMBUS_RATE_LIMIT_BURST: %w", err)
		}
		cfg.RateLimitBurst = n
	}
	if v := os.Getenv("NIMBUS_LOGIN_RATE_LIMIT_RPS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return Config{}, fmt.Errorf("NIMBUS_LOGIN_RATE_LIMIT_RPS: %w", err)
		}
		cfg.LoginRateLimitRPS = n
	}
	if v := os.Getenv("NIMBUS_LOGIN_RATE_LIMIT_BURST"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return Config{}, fmt.Errorf("NIMBUS_LOGIN_RATE_LIMIT_BURST: %w", err)
		}
		cfg.LoginRateLimitBurst = n
	}
	if v := os.Getenv("NIMBUS_REPLICATION_FACTOR"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return Config{}, fmt.Errorf("NIMBUS_REPLICATION_FACTOR: %w", err)
		}
		cfg.ReplicationFactor = n
	}
	if v := os.Getenv("NIMBUS_WRITE_QUORUM"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return Config{}, fmt.Errorf("NIMBUS_WRITE_QUORUM: %w", err)
		}
		cfg.WriteQuorum = n
	}

	if cfg.StorageNodes, err = parseStorageNodes(os.Getenv("NIMBUS_STORAGE_NODES")); err != nil {
		return Config{}, err
	}

	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// parseStorageNodes parses "id1=internal[|public],id2=internal[|public],...".
// The "|public" half is optional and defaults to the internal endpoint; it
// exists because presigned URLs must be signed against whatever host the
// external caller will actually use (see StorageNode.PublicEndpoint).
func parseStorageNodes(raw string) ([]StorageNode, error) {
	if raw == "" {
		return nil, nil
	}
	var nodes []StorageNode
	for _, pair := range strings.Split(raw, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return nil, fmt.Errorf("NIMBUS_STORAGE_NODES: invalid entry %q, want id=endpoint", pair)
		}
		endpoints := strings.SplitN(parts[1], "|", 2)
		node := StorageNode{ID: parts[0], Endpoint: endpoints[0], PublicEndpoint: endpoints[0]}
		if len(endpoints) == 2 && endpoints[1] != "" {
			node.PublicEndpoint = endpoints[1]
		}
		nodes = append(nodes, node)
	}
	return nodes, nil
}

func (c Config) validate() error {
	var missing []string
	if c.PostgresDSN == "" {
		missing = append(missing, "NIMBUS_POSTGRES_DSN")
	}
	if c.JWTSecret == "" {
		missing = append(missing, "NIMBUS_JWT_SECRET")
	} else if len(c.JWTSecret) < 32 {
		return fmt.Errorf("NIMBUS_JWT_SECRET: must be at least 32 characters")
	}
	if c.JWTSecretPrevious != "" && len(c.JWTSecretPrevious) < 32 {
		return fmt.Errorf("NIMBUS_JWT_SECRET_PREVIOUS: must be at least 32 characters")
	}
	if len(c.StorageNodes) > 0 {
		if c.MinIOAccessKey == "" {
			missing = append(missing, "NIMBUS_MINIO_ACCESS_KEY")
		}
		if c.MinIOSecretKey == "" {
			missing = append(missing, "NIMBUS_MINIO_SECRET_KEY")
		}
	}
	if c.AdminEmail == "" {
		missing = append(missing, "NIMBUS_ADMIN_EMAIL")
	}
	if c.AdminPassword == "" {
		missing = append(missing, "NIMBUS_ADMIN_PASSWORD")
	} else if err := validateAdminPassword(c.AdminPassword); err != nil {
		return err
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required env vars: %s", strings.Join(missing, ", "))
	}
	if c.WriteQuorum > c.ReplicationFactor {
		return fmt.Errorf("NIMBUS_WRITE_QUORUM (%d) cannot exceed NIMBUS_REPLICATION_FACTOR (%d)", c.WriteQuorum, c.ReplicationFactor)
	}
	if c.RateLimitRPS > 0 && c.RateLimitBurst < 1 {
		return fmt.Errorf("NIMBUS_RATE_LIMIT_BURST must be at least 1 when rate limiting is enabled")
	}
	if c.LoginRateLimitRPS > 0 && c.LoginRateLimitBurst < 1 {
		return fmt.Errorf("NIMBUS_LOGIN_RATE_LIMIT_BURST must be at least 1 when login rate limiting is enabled")
	}
	return nil
}

// validateAdminPassword holds the seeded platform-admin credential
// (auth.Repository.EnsureSeededAdmin) to a higher bar than the ≥8-character
// check applied to regular user signup passwords (auth.Handler.Register) —
// this one account has cluster-wide /v1/admin/* access, so a short or
// low-entropy value shouldn't be able to stand in as its real credential
// (audit §04).
func validateAdminPassword(pw string) error {
	if len(pw) < 12 {
		return fmt.Errorf("NIMBUS_ADMIN_PASSWORD: must be at least 12 characters")
	}
	var hasUpper, hasLower, hasDigit, hasSymbol bool
	for _, r := range pw {
		switch {
		case unicode.IsUpper(r):
			hasUpper = true
		case unicode.IsLower(r):
			hasLower = true
		case unicode.IsDigit(r):
			hasDigit = true
		default:
			hasSymbol = true
		}
	}
	classes := 0
	for _, ok := range []bool{hasUpper, hasLower, hasDigit, hasSymbol} {
		if ok {
			classes++
		}
	}
	if classes < 3 {
		return fmt.Errorf("NIMBUS_ADMIN_PASSWORD: must contain at least 3 of: uppercase, lowercase, digit, symbol")
	}
	return nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getDuration(key string, fallback time.Duration) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return d, nil
}
