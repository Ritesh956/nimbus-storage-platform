//go:build integration

// Day 12 deliverable (docs/09-roadmap.md): targeted integration coverage
// against a real Postgres (the docker-compose stack), not mocks. Gated
// behind the "integration" build tag — run explicitly with
// `go test -tags=integration ./...`.
package upload_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"nimbus/internal/auth"
	"nimbus/internal/file"
	"nimbus/internal/folder"
	"nimbus/internal/org"
	"nimbus/internal/upload"
)

// Overridable via env — see the comment on the equivalent helper in
// internal/auth/refresh_integration_test.go for why "localhost" alone isn't
// reliable on this project's dev machine.
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

// TestCompleteUpload_ConcurrentCallersOnlyOneWins locks in the
// compare-and-swap behavior documented on Repository.CompleteUpload: two
// concurrent /complete calls for the same upload must not both succeed —
// exactly one gets to flip status to 'completed', the other gets
// ErrAlreadyCompleting. No smoke script exercises this because it requires
// two genuinely simultaneous requests, not sequential ones.
func TestCompleteUpload_ConcurrentCallersOnlyOneWins(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	authRepo := auth.NewRepository(pool)
	user, err := authRepo.CreateUser(ctx, fmt.Sprintf("race-%s@nimbus.test", suffix), "irrelevant-hash")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	orgRepo := org.NewRepository(pool)
	o, err := orgRepo.CreateWithOwner(ctx, "Race Test Org "+suffix, user.ID)
	if err != nil {
		t.Fatalf("create org: %v", err)
	}

	folderRepo := folder.NewRepository(pool)
	f, err := folderRepo.Create(ctx, o.ID, nil, "Race Test Folder")
	if err != nil {
		t.Fatalf("create folder: %v", err)
	}

	// A real file+version row (empty chunk list — content is irrelevant
	// here) so uploads.file_id/version_id's FK constraints are satisfiable;
	// both racing completions point at the same one, since the point of
	// this test is the upload's own status CAS, not distinct files.
	fileRepo := file.NewRepository(pool)
	fileID, versionID, err := fileRepo.CreateWithVersion(ctx, o.ID, f.ID, "race.bin", user.ID, 0, "deadbeef", "application/octet-stream", []string{})
	if err != nil {
		t.Fatalf("create file+version: %v", err)
	}

	uploadRepo := upload.NewRepository(pool)
	u, err := uploadRepo.CreateUpload(ctx, o.ID, f.ID, "race-upload.bin", 0, "application/octet-stream", user.ID, nil)
	if err != nil {
		t.Fatalf("create upload: %v", err)
	}

	const racers = 8
	errs := make([]error, racers)
	var wg sync.WaitGroup
	wg.Add(racers)
	for i := 0; i < racers; i++ {
		go func(i int) {
			defer wg.Done()
			errs[i] = uploadRepo.CompleteUpload(ctx, u.ID, fileID, versionID, nil)
		}(i)
	}
	wg.Wait()

	var wins, losses int
	for _, err := range errs {
		switch {
		case err == nil:
			wins++
		case errors.Is(err, upload.ErrAlreadyCompleting):
			losses++
		default:
			t.Fatalf("unexpected error from a racer: %v", err)
		}
	}
	if wins != 1 {
		t.Fatalf("got %d winning CompleteUpload calls out of %d racers, want exactly 1 (losses=%d)", wins, racers, losses)
	}
	if losses != racers-1 {
		t.Fatalf("got %d ErrAlreadyCompleting losses, want %d", losses, racers-1)
	}
}
