//go:build integration

// Audit §14 named gc's 13-assertion smoke script (scripts/smoke-gc.js) as
// one of the two highest-value ports into CI (alongside sharing). mark/
// sweep/reapChunk are unexported, so this lives in package gc (white-box)
// rather than gc_test, matching the constraint noted when this suite was
// planned. Needs real Postgres (chunk/file state) and real MinIO (physical
// object deletion) — both now provided by the integration-test CI job.
// Timing is made deterministic by directly backdating last_seen_at/
// doomed_at via SQL rather than sleeping through real grace windows, unlike
// the smoke script's real-wall-clock waits — same correctness properties,
// no multi-minute test run. The down-node resilience scenario
// (scripts/smoke-gc.js's step 8, which stops a real MinIO container) isn't
// ported: it needs container orchestration this suite doesn't have and
// stays a manual/smoke-script concern.
package gc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/minio/minio-go/v7"

	"nimbus/internal/auth"
	"nimbus/internal/file"
	"nimbus/internal/folder"
	"nimbus/internal/org"
	"nimbus/internal/storage"
	"nimbus/internal/upload"
)

func testPostgresDSN() string {
	if v := os.Getenv("NIMBUS_TEST_POSTGRES_DSN"); v != "" {
		return v
	}
	return "postgres://nimbus:nimbus@localhost:5432/nimbus?sslmode=disable"
}

func testMinioEndpoint() string {
	if v := os.Getenv("NIMBUS_TEST_MINIO_ENDPOINT"); v != "" {
		return v
	}
	return "localhost:9000"
}

func testMinioAccessKey() string {
	if v := os.Getenv("NIMBUS_TEST_MINIO_ACCESS_KEY"); v != "" {
		return v
	}
	return "nimbus"
}

func testMinioSecretKey() string {
	if v := os.Getenv("NIMBUS_TEST_MINIO_SECRET_KEY"); v != "" {
		return v
	}
	return "nimbus-secret"
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

// newTestRouter builds a single-node Router against the test MinIO instance
// and ensures its bucket exists — same Endpoint/PublicEndpoint since the Go
// test process, unlike nimbus-api's own container, isn't itself running
// inside the docker network the CI job's MinIO container lives on.
func newTestRouter(t *testing.T, pool *pgxpool.Pool) (*storage.Router, storage.NodeID) {
	t.Helper()
	ctx := context.Background()
	endpoint := "http://" + testMinioEndpoint()
	const nodeID = storage.NodeID("gc-test-node")

	storageRepo := storage.NewRepository(pool)
	router, err := storage.NewRouter(storageRepo, nil,
		[]storage.StorageNode{{ID: nodeID, Endpoint: endpoint, PublicEndpoint: endpoint}},
		testMinioAccessKey(), testMinioSecretKey(), slog.Default(), 0)
	if err != nil {
		t.Skipf("could not build a test Router (is MinIO reachable at %s?): %v", endpoint, err)
	}
	if err := router.Bootstrap(ctx); err != nil {
		t.Fatalf("Router.Bootstrap: %v", err)
	}
	if err := router.EnsureBuckets(ctx); err != nil {
		t.Skipf("minio not reachable at %s: %v", endpoint, err)
	}
	return router, nodeID
}

func objectExists(t *testing.T, router *storage.Router, nodeID storage.NodeID, hash string) bool {
	t.Helper()
	rc, err := router.GetObject(context.Background(), nodeID, hash)
	if err != nil {
		return false
	}
	defer rc.Close()
	obj, ok := rc.(*minio.Object)
	if !ok {
		t.Fatalf("Router.GetObject returned a %T, want *minio.Object", rc)
	}
	// minio-go's GetObject doesn't itself hit the network — the object's
	// existence is only proven by Stat() (a real HEAD request).
	_, err = obj.Stat()
	return err == nil
}

type gcFixture struct {
	ownerID  string
	orgID    string
	folderID string
	pool     *pgxpool.Pool
	fileRepo *file.Repository
	upRepo   *upload.Repository
	router   *storage.Router
	nodeID   storage.NodeID
	sweeper  *Sweeper
}

func newGCFixture(t *testing.T) gcFixture {
	t.Helper()
	pool := newTestPool(t)
	router, nodeID := newTestRouter(t, pool)
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	authRepo := auth.NewRepository(pool)
	user, err := authRepo.CreateUser(ctx, fmt.Sprintf("gc-%s@nimbus.test", suffix), "irrelevant-hash")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	orgRepo := org.NewRepository(pool)
	o, err := orgRepo.CreateWithOwner(ctx, "GC Test Org "+suffix, user.ID)
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	folderRepo := folder.NewRepository(pool)
	f, err := folderRepo.Create(ctx, o.ID, nil, "GC Test Folder")
	if err != nil {
		t.Fatalf("create folder: %v", err)
	}
	fileRepo := file.NewRepository(pool)
	upRepo := upload.NewRepository(pool)

	// grace=1s is irrelevant to correctness here — every test backdates
	// last_seen_at/doomed_at directly rather than waiting it out, but a
	// real (non-zero) value keeps the SQL interval math unremarkable.
	sweeper := NewSweeper(pool, router, time.Hour /* interval: Run() is never called */, time.Second, 0, fileRepo, folderRepo, slog.Default())

	return gcFixture{ownerID: user.ID, orgID: o.ID, folderID: f.ID, pool: pool, fileRepo: fileRepo, upRepo: upRepo, router: router, nodeID: nodeID, sweeper: sweeper}
}

// commitChunk writes real bytes to MinIO and records the chunk/location
// rows the same way upload.Service.CommitChunk does at the repository
// level — this is the minimal real setup gc's mark/sweep actually reason
// about, without needing the full HTTP upload flow.
func (fx gcFixture) commitChunk(t *testing.T, content []byte) (hash string) {
	t.Helper()
	sum := sha256.Sum256(content)
	hash = hex.EncodeToString(sum[:])
	ctx := context.Background()

	if err := fx.router.PutObject(ctx, fx.nodeID, hash, content, "application/octet-stream"); err != nil {
		t.Fatalf("PutObject: %v", err)
	}
	if err := fx.upRepo.UpsertGlobalChunk(ctx, hash, int64(len(content))); err != nil {
		t.Fatalf("UpsertGlobalChunk: %v", err)
	}
	if err := fx.upRepo.UpsertChunkLocation(ctx, hash, string(fx.nodeID)); err != nil {
		t.Fatalf("UpsertChunkLocation: %v", err)
	}
	return hash
}

func (fx gcFixture) gcState(t *testing.T, hash string) (state string, doomedAt *time.Time) {
	t.Helper()
	err := fx.pool.QueryRow(context.Background(), `SELECT gc_state, doomed_at FROM chunks WHERE hash = $1`, hash).Scan(&state, &doomedAt)
	if err != nil {
		t.Fatalf("query gc_state for %s: %v", hash, err)
	}
	return state, doomedAt
}

// Both backdate helpers pass seconds through a `* interval '1 second'`
// multiply rather than binding a Go time.Duration directly — matching
// sweeper.go's own mark()/sweep() queries — because pgx has no built-in
// mapping from time.Duration to Postgres' interval type; binding one
// directly resolves to a type Postgres can't subtract from a timestamptz
// column (caught by an actual failing test run, not by vet/build).
func (fx gcFixture) backdateLastSeen(t *testing.T, hash string, ago time.Duration) {
	t.Helper()
	if _, err := fx.pool.Exec(context.Background(),
		`UPDATE chunks SET last_seen_at = now() - ($1 * interval '1 second') WHERE hash = $2`, ago.Seconds(), hash); err != nil {
		t.Fatalf("backdate last_seen_at: %v", err)
	}
}

func (fx gcFixture) backdateDoomedAt(t *testing.T, hash string, ago time.Duration) {
	t.Helper()
	if _, err := fx.pool.Exec(context.Background(),
		`UPDATE chunks SET doomed_at = now() - ($1 * interval '1 second') WHERE hash = $2`, ago.Seconds(), hash); err != nil {
		t.Fatalf("backdate doomed_at: %v", err)
	}
}

// ---- 1. dedup protection: chunks referenced by a surviving file stay live

func TestMark_ChunkStaysLiveWhileStillReferenced(t *testing.T) {
	fx := newGCFixture(t)
	ctx := context.Background()
	content := []byte("shared content between two files")
	hash := fx.commitChunk(t, content)

	fileA, _, err := fx.fileRepo.CreateWithVersion(ctx, fx.orgID, fx.folderID, "gc-a.bin", fx.ownerID, int64(len(content)), "checksum-a", "application/octet-stream", []string{hash})
	if err != nil {
		t.Fatalf("create file A: %v", err)
	}
	_, _, err = fx.fileRepo.CreateWithVersion(ctx, fx.orgID, fx.folderID, "gc-b.bin", fx.ownerID, int64(len(content)), "checksum-b", "application/octet-stream", []string{hash})
	if err != nil {
		t.Fatalf("create file B (shares the chunk): %v", err)
	}

	// Purge file A (its only reference) — file B still references the chunk.
	if err := fx.fileRepo.SoftDelete(ctx, fileA); err != nil {
		t.Fatalf("SoftDelete A: %v", err)
	}
	if err := fx.fileRepo.Purge(ctx, fileA); err != nil {
		t.Fatalf("Purge A: %v", err)
	}
	fx.backdateLastSeen(t, hash, time.Hour) // would be wrongly doomed if refcounting were broken

	if _, err := fx.sweeper.mark(ctx); err != nil {
		t.Fatalf("mark: %v", err)
	}
	state, _ := fx.gcState(t, hash)
	if state != "live" {
		t.Fatalf("gc_state = %s, want live (file B still references this chunk)", state)
	}
}

// ---- 2. mark: purging the last reference dooms the chunk

func TestMark_UnreferencedChunkIsDoomedAfterGrace(t *testing.T) {
	fx := newGCFixture(t)
	ctx := context.Background()
	content := []byte("solo content")
	hash := fx.commitChunk(t, content)

	fileID, _, err := fx.fileRepo.CreateWithVersion(ctx, fx.orgID, fx.folderID, "gc-solo.bin", fx.ownerID, int64(len(content)), "checksum-solo", "application/octet-stream", []string{hash})
	if err != nil {
		t.Fatalf("create file: %v", err)
	}
	if err := fx.fileRepo.SoftDelete(ctx, fileID); err != nil {
		t.Fatalf("SoftDelete: %v", err)
	}
	if err := fx.fileRepo.Purge(ctx, fileID); err != nil {
		t.Fatalf("Purge: %v", err)
	}
	fx.backdateLastSeen(t, hash, time.Hour)

	doomed, err := fx.sweeper.mark(ctx)
	if err != nil {
		t.Fatalf("mark: %v", err)
	}
	if doomed != 1 {
		t.Fatalf("mark doomed %d chunks, want 1", doomed)
	}
	state, doomedAt := fx.gcState(t, hash)
	if state != "doomed" {
		t.Fatalf("gc_state = %s, want doomed", state)
	}
	if doomedAt == nil {
		t.Fatal("doomed_at should be set once a chunk is doomed")
	}
}

// ---- 3. resurrection: re-committing doomed content flips it back to live

func TestResurrection_RecommittingADoomedChunkFlipsItBackToLive(t *testing.T) {
	fx := newGCFixture(t)
	ctx := context.Background()
	content := []byte("resurrectable content")
	hash := fx.commitChunk(t, content)
	// Resurrection (below) intentionally flips this chunk back to 'live'
	// with a fresh last_seen_at without ever giving it a new
	// file_version_chunks reference — that's the whole point, proving
	// UpsertGlobalChunk alone does it. Left alone past this test, it's a
	// real live-but-unreferenced row sitting in the shared test database:
	// once its last_seen_at ages past fx.sweeper's 1-second grace, any
	// later test's mark() call (a global, unscoped UPDATE) can sweep it in
	// and inflate that test's own doomed-chunk count. Clean it up
	// explicitly rather than leaving cross-test timing to chance.
	t.Cleanup(func() {
		if _, err := fx.pool.Exec(context.Background(), `DELETE FROM chunks WHERE hash = $1`, hash); err != nil {
			t.Logf("cleanup: delete resurrected chunk %s: %v", hash, err)
		}
	})

	fileID, _, err := fx.fileRepo.CreateWithVersion(ctx, fx.orgID, fx.folderID, "gc-r1.bin", fx.ownerID, int64(len(content)), "checksum-r1", "application/octet-stream", []string{hash})
	if err != nil {
		t.Fatalf("create file: %v", err)
	}
	if err := fx.fileRepo.SoftDelete(ctx, fileID); err != nil {
		t.Fatalf("SoftDelete: %v", err)
	}
	if err := fx.fileRepo.Purge(ctx, fileID); err != nil {
		t.Fatalf("Purge: %v", err)
	}
	fx.backdateLastSeen(t, hash, time.Hour)
	if _, err := fx.sweeper.mark(ctx); err != nil {
		t.Fatalf("mark: %v", err)
	}
	if state, _ := fx.gcState(t, hash); state != "doomed" {
		t.Fatalf("precondition failed: gc_state = %s, want doomed before resurrection", state)
	}

	// Re-upload the same content — mirrors what a second CommitChunk does.
	if err := fx.upRepo.UpsertGlobalChunk(ctx, hash, int64(len(content))); err != nil {
		t.Fatalf("re-commit (resurrect): %v", err)
	}

	state, doomedAt := fx.gcState(t, hash)
	if state != "live" {
		t.Fatalf("gc_state after resurrection = %s, want live", state)
	}
	if doomedAt != nil {
		t.Fatal("doomed_at should be cleared after resurrection")
	}
}

// ---- 4. sweep: a doomed chunk past its second grace window is physically deleted

func TestSweep_PhysicallyDeletesChunkRowAndMinIOObject(t *testing.T) {
	fx := newGCFixture(t)
	ctx := context.Background()
	content := []byte("content destined for the sweep")
	hash := fx.commitChunk(t, content)

	if !objectExists(t, fx.router, fx.nodeID, hash) {
		t.Fatal("precondition failed: object should exist in MinIO right after PutObject")
	}

	fileID, _, err := fx.fileRepo.CreateWithVersion(ctx, fx.orgID, fx.folderID, "gc-sweep.bin", fx.ownerID, int64(len(content)), "checksum-sweep", "application/octet-stream", []string{hash})
	if err != nil {
		t.Fatalf("create file: %v", err)
	}
	if err := fx.fileRepo.SoftDelete(ctx, fileID); err != nil {
		t.Fatalf("SoftDelete: %v", err)
	}
	if err := fx.fileRepo.Purge(ctx, fileID); err != nil {
		t.Fatalf("Purge: %v", err)
	}
	fx.backdateLastSeen(t, hash, time.Hour)
	if _, err := fx.sweeper.mark(ctx); err != nil {
		t.Fatalf("mark: %v", err)
	}
	fx.backdateDoomedAt(t, hash, time.Hour) // simulate the second grace window elapsing

	if err := fx.sweeper.sweep(ctx); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	var count int
	if err := fx.pool.QueryRow(ctx, `SELECT count(*) FROM chunks WHERE hash = $1`, hash).Scan(&count); err != nil {
		t.Fatalf("count chunks row: %v", err)
	}
	if count != 0 {
		t.Fatal("chunks row should be gone after sweep")
	}
	if objectExists(t, fx.router, fx.nodeID, hash) {
		t.Fatal("MinIO object should be gone after sweep")
	}
}

// ---- 5. trash auto-purge feeds the chunk GC (folder.PurgeExpiredTrash / file.PurgeExpiredTrash via TrashPurger)

func TestPurgeTrash_ExpiredFileIsAutoPurgedAndFeedsMark(t *testing.T) {
	fx := newGCFixture(t)
	ctx := context.Background()
	content := []byte("auto-purge candidate")
	hash := fx.commitChunk(t, content)

	fileID, _, err := fx.fileRepo.CreateWithVersion(ctx, fx.orgID, fx.folderID, "gc-autopurge.bin", fx.ownerID, int64(len(content)), "checksum-autopurge", "application/octet-stream", []string{hash})
	if err != nil {
		t.Fatalf("create file: %v", err)
	}
	if err := fx.fileRepo.SoftDelete(ctx, fileID); err != nil {
		t.Fatalf("SoftDelete: %v", err)
	}
	if _, err := fx.pool.Exec(ctx, `UPDATE files SET deleted_at = now() - interval '31 days' WHERE id = $1`, fileID); err != nil {
		t.Fatalf("backdate deleted_at: %v", err)
	}

	// trashRetention=0 on this fixture's sweeper (disabled) — build a second
	// Sweeper with retention enabled, matching how nimbus-worker is actually
	// configured (NIMBUS_TRASH_RETENTION), to exercise purgeTrash.
	// purgeTrash calls both files.PurgeExpiredTrash and
	// folders.PurgeExpiredTrash unconditionally (the trashRetention>0 guard
	// lives in tick(), one level up) so folders must be a real repository,
	// not nil, even though this test only cares about the file side.
	retainingSweeper := NewSweeper(fx.pool, fx.router, time.Hour, time.Second, 30*24*time.Hour, fx.fileRepo, folder.NewRepository(fx.pool), slog.Default())
	retainingSweeper.purgeTrash(ctx)

	if _, err := fx.fileRepo.GetAny(ctx, fileID); err == nil {
		t.Fatal("expired trashed file should have been auto-purged")
	}

	fx.backdateLastSeen(t, hash, time.Hour)
	doomed, err := fx.sweeper.mark(ctx)
	if err != nil {
		t.Fatalf("mark: %v", err)
	}
	if doomed != 1 {
		t.Fatalf("auto-purge should have freed the chunk's last reference, feeding mark — doomed=%d, want 1", doomed)
	}
}
