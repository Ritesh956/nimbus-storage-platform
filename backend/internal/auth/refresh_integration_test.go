//go:build integration

// Day 12 deliverable (docs/09-roadmap.md): targeted integration coverage
// against real Postgres/Redis (the docker-compose stack), not mocks — per
// this project's "verify against the real thing" convention. Gated behind
// the "integration" build tag so a plain `go test ./...` (e.g. in an
// environment without the stack running) doesn't fail; run explicitly with
// `go test -tags=integration ./...`.
package auth_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"nimbus/internal/auth"
)

// Overridable via env because "localhost" is ambiguous on this project's
// dev machine: a native Postgres install competes with Docker Desktop's
// port-forward for host port 5432 (see docs/00-project-state.md's Docker
// footgun notes) — the default here matches `docker compose up` run
// directly on a host with no such conflict; CI/local setups that do
// conflict should point these at the compose network instead (e.g. run via
// `docker run --network nimbus_default ... -e NIMBUS_TEST_POSTGRES_DSN=postgres://nimbus:nimbus@postgres:5432/nimbus?sslmode=disable`).
func testPostgresDSN() string {
	if v := os.Getenv("NIMBUS_TEST_POSTGRES_DSN"); v != "" {
		return v
	}
	return "postgres://nimbus:nimbus@localhost:5432/nimbus?sslmode=disable"
}

func testRedisAddr() string {
	if v := os.Getenv("NIMBUS_TEST_REDIS_ADDR"); v != "" {
		return v
	}
	return "localhost:6379"
}

func newTestAuthService(t *testing.T) *auth.Service {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	dsn := testPostgresDSN()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skipf("postgres not reachable at %s (is `docker compose up` running?): %v", dsn, err)
	}
	if err := pool.Ping(ctx); err != nil {
		t.Skipf("postgres not reachable at %s (is `docker compose up` running?): %v", dsn, err)
	}
	t.Cleanup(pool.Close)

	addr := testRedisAddr()
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skipf("redis not reachable at %s (is `docker compose up` running?): %v", addr, err)
	}
	t.Cleanup(func() { _ = rdb.Close() })

	repo := auth.NewRepository(pool)
	return auth.NewService(repo, rdb, "integration-test-secret-at-least-32-characters", 15*time.Minute, 7*24*time.Hour)
}

// TestRefresh_ReuseOfRotatedTokenRevokesWholeFamily locks in the
// compromise-response behavior documented in
// Repository.RotateRefreshToken and docs/04-lld.md §3: replaying an
// already-rotated refresh token doesn't just fail that one call, it revokes
// every token in the family — including ones that were never themselves
// reused. No smoke script exercises this because it requires holding onto
// an already-rotated token, which the real frontend never does.
func TestRefresh_ReuseOfRotatedTokenRevokesWholeFamily(t *testing.T) {
	svc := newTestAuthService(t)
	ctx := context.Background()

	email := fmt.Sprintf("refresh-reuse-%d@nimbus.test", time.Now().UnixNano())
	password := "correct-horse-battery-staple"
	if _, err := svc.Register(ctx, email, password); err != nil {
		t.Fatalf("register: %v", err)
	}
	_, pair1, err := svc.Login(ctx, email, password)
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	pair2, err := svc.Refresh(ctx, pair1.RefreshToken)
	if err != nil {
		t.Fatalf("first (legitimate) refresh: got %v, want success", err)
	}

	if _, err := svc.Refresh(ctx, pair1.RefreshToken); !errors.Is(err, auth.ErrInvalidRefresh) {
		t.Fatalf("reuse of already-rotated token: got %v, want ErrInvalidRefresh", err)
	}

	if _, err := svc.Refresh(ctx, pair2.RefreshToken); !errors.Is(err, auth.ErrInvalidRefresh) {
		t.Fatalf("legitimately-rotated token after sibling reuse: got %v, want ErrInvalidRefresh (whole family should be revoked)", err)
	}
}
