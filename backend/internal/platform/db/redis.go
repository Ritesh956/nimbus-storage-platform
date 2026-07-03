package db

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

// NewRedis builds a client and verifies connectivity with a PING.
func NewRedis(ctx context.Context, addr string) (*redis.Client, error) {
	client := redis.NewClient(&redis.Options{Addr: addr})
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("redis: %w", err)
	}
	return client, nil
}
