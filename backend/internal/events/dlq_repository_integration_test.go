//go:build integration

// Audit §14: events' DLQ Repository (the data half of the admin DLQ
// endpoints, audit roadmap item #9) had zero automated tests. Gated behind
// the "integration" build tag, matching the house style set by
// internal/auth/refresh_integration_test.go.
package events_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"nimbus/internal/events"
)

func testPostgresDSN() string {
	if v := os.Getenv("NIMBUS_TEST_POSTGRES_DSN"); v != "" {
		return v
	}
	return "postgres://nimbus:nimbus@localhost:5432/nimbus?sslmode=disable"
}

func newTestPool(t *testing.T) *pgxpool.Pool {
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
	return pool
}

func TestInsertAndListDeadEvents_RoundTrips(t *testing.T) {
	pool := newTestPool(t)
	repo := events.NewRepository(pool)
	ctx := context.Background()

	payload := []byte(`{"event_id":"evt-1","file_id":"file-1"}`)
	if err := repo.InsertDeadEvent(ctx, "nimbus.uploads.completed", payload, "handler exploded", 5); err != nil {
		t.Fatalf("InsertDeadEvent: %v", err)
	}

	events_, err := repo.ListDeadEvents(ctx)
	if err != nil {
		t.Fatalf("ListDeadEvents: %v", err)
	}
	var found bool
	for _, e := range events_ {
		if e.Subject == "nimbus.uploads.completed" && e.Error == "handler exploded" && e.Deliveries == 5 && e.Status == "dead" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("ListDeadEvents did not include the just-inserted dead event; got %+v", events_)
	}
}

func TestGetDeadEvent_UnknownIDReturnsErrDeadEventNotFound(t *testing.T) {
	pool := newTestPool(t)
	repo := events.NewRepository(pool)
	_, err := repo.GetDeadEvent(context.Background(), "00000000-0000-0000-0000-000000000000")
	if !errors.Is(err, events.ErrDeadEventNotFound) {
		t.Fatalf("got err %v, want ErrDeadEventNotFound", err)
	}
}

func TestMarkRetried_OnlyTransitionsFromDeadOnce(t *testing.T) {
	pool := newTestPool(t)
	repo := events.NewRepository(pool)
	ctx := context.Background()

	if err := repo.InsertDeadEvent(ctx, "nimbus.uploads.completed", []byte(`{}`), "boom", 5); err != nil {
		t.Fatalf("InsertDeadEvent: %v", err)
	}
	all, err := repo.ListDeadEvents(ctx)
	if err != nil || len(all) == 0 {
		t.Fatalf("ListDeadEvents: %v", err)
	}
	id := all[0].ID

	ok, err := repo.MarkRetried(ctx, id)
	if err != nil {
		t.Fatalf("MarkRetried: %v", err)
	}
	if !ok {
		t.Fatal("first MarkRetried should succeed (transitioning dead -> retried)")
	}

	got, err := repo.GetDeadEvent(ctx, id)
	if err != nil {
		t.Fatalf("GetDeadEvent: %v", err)
	}
	if got.Status != "retried" || got.RetriedAt == nil {
		t.Fatalf("event = %+v, want status=retried with RetriedAt set", got)
	}

	// A double-click (or a retry of an already-retried event) must not
	// silently "succeed" again — the WHERE status='dead' guard exists
	// specifically to prevent double-republishing.
	ok, err = repo.MarkRetried(ctx, id)
	if err != nil {
		t.Fatalf("second MarkRetried: %v", err)
	}
	if ok {
		t.Fatal("second MarkRetried should report false (already retried, no row matched)")
	}
}

func TestRevertRetry_MakesTheEventRetryableAgain(t *testing.T) {
	pool := newTestPool(t)
	repo := events.NewRepository(pool)
	ctx := context.Background()

	if err := repo.InsertDeadEvent(ctx, "nimbus.uploads.completed", []byte(`{}`), "boom", 5); err != nil {
		t.Fatalf("InsertDeadEvent: %v", err)
	}
	all, err := repo.ListDeadEvents(ctx)
	if err != nil || len(all) == 0 {
		t.Fatalf("ListDeadEvents: %v", err)
	}
	id := all[0].ID

	if ok, err := repo.MarkRetried(ctx, id); err != nil || !ok {
		t.Fatalf("MarkRetried: ok=%v err=%v", ok, err)
	}
	if err := repo.RevertRetry(ctx, id); err != nil {
		t.Fatalf("RevertRetry: %v", err)
	}

	got, err := repo.GetDeadEvent(ctx, id)
	if err != nil {
		t.Fatalf("GetDeadEvent: %v", err)
	}
	if got.Status != "dead" || got.RetriedAt != nil {
		t.Fatalf("event after revert = %+v, want status=dead with RetriedAt cleared", got)
	}

	// And it should be markable-retried again now.
	if ok, err := repo.MarkRetried(ctx, id); err != nil || !ok {
		t.Fatalf("MarkRetried after revert: ok=%v err=%v, want ok=true", ok, err)
	}
}
