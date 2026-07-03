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
)

type StorageNode struct {
	ID             string
	Endpoint       string
	PublicEndpoint string
}

type Config struct {
	Env      string // "dev" | "prod"
	HTTPPort string

	PostgresDSN string
	RedisAddr   string
	NATSURL     string

	JWTSecret          string
	AccessTokenTTL     time.Duration
	RefreshTokenTTL    time.Duration
	TrashRetentionDays int

	ChunkSizeBytes    int64
	ReplicationFactor int // N
	WriteQuorum       int // W

	StorageNodes   []StorageNode
	MinIOAccessKey string
	MinIOSecretKey string
}

func Load() (Config, error) {
	cfg := Config{
		Env:                getEnv("NIMBUS_ENV", "dev"),
		HTTPPort:           getEnv("NIMBUS_HTTP_PORT", "8080"),
		PostgresDSN:        os.Getenv("NIMBUS_POSTGRES_DSN"),
		RedisAddr:          getEnv("NIMBUS_REDIS_ADDR", "localhost:6379"),
		NATSURL:            getEnv("NIMBUS_NATS_URL", "nats://localhost:4222"),
		JWTSecret:          os.Getenv("NIMBUS_JWT_SECRET"),
		MinIOAccessKey:     os.Getenv("NIMBUS_MINIO_ACCESS_KEY"),
		MinIOSecretKey:     os.Getenv("NIMBUS_MINIO_SECRET_KEY"),
		TrashRetentionDays: 30,
		ChunkSizeBytes:     8 * 1024 * 1024, // 8 MiB, docs/02-system-design.md §2.1
		ReplicationFactor:  2,
		WriteQuorum:        2,
	}

	var err error
	if cfg.AccessTokenTTL, err = getDuration("NIMBUS_ACCESS_TOKEN_TTL", 15*time.Minute); err != nil {
		return Config{}, err
	}
	if cfg.RefreshTokenTTL, err = getDuration("NIMBUS_REFRESH_TOKEN_TTL", 7*24*time.Hour); err != nil {
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
	if len(c.StorageNodes) > 0 {
		if c.MinIOAccessKey == "" {
			missing = append(missing, "NIMBUS_MINIO_ACCESS_KEY")
		}
		if c.MinIOSecretKey == "" {
			missing = append(missing, "NIMBUS_MINIO_SECRET_KEY")
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required env vars: %s", strings.Join(missing, ", "))
	}
	if c.WriteQuorum > c.ReplicationFactor {
		return fmt.Errorf("NIMBUS_WRITE_QUORUM (%d) cannot exceed NIMBUS_REPLICATION_FACTOR (%d)", c.WriteQuorum, c.ReplicationFactor)
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
