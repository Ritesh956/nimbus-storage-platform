//go:build integration

// Audit §14: file — versioning, restore, purge, cross-org move validation —
// had zero automated tests. Mirrors scripts/smoke-versions.js's assertions
// (upload v1 -> re-upload v2 -> restore v1) against real Postgres. Gated
// behind the "integration" build tag, matching the house style set by
// internal/auth/refresh_integration_test.go. DownloadPlan/ThumbnailTargets
// need a real storage.Router/MinIO and are intentionally out of scope here —
// covered indirectly by internal/sharing's stub-planner tests and
// internal/gc's real-MinIO tests.
package file_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"nimbus/internal/auth"
	"nimbus/internal/file"
	"nimbus/internal/folder"
	"nimbus/internal/org"
	"nimbus/internal/upload"
)

// fakeChecksum returns a real-shaped 64-hex-char SHA-256 digest —
// file_versions.checksum_sha256 is a fixed-length char(64) column, so a
// short placeholder like fakeChecksum("v1") comes back from Postgres
// space-padded to 64 chars and fails any equality check against the
// original short string (caught by an actual failing test run).
func fakeChecksum(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:])
}

// seedChunk inserts a row into the global chunks table so a chunk_hash
// referencing it in file_version_chunks satisfies that table's FK — a real
// upload commits chunks via upload.Service.CommitChunk before a file's
// version can reference them; CreateWithVersion has no MinIO dependency
// itself, so this is the minimal real setup rather than going through the
// full HTTP upload flow (matching the pattern established in
// internal/gc's integration tests).
func seedChunk(t *testing.T, pool *pgxpool.Pool, hash string) {
	t.Helper()
	if err := upload.NewRepository(pool).UpsertGlobalChunk(context.Background(), hash, 1024); err != nil {
		t.Fatalf("seed chunk %s: %v", hash, err)
	}
}

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

// folderOrgLookupAdapter mirrors cmd/api/adapters.go's real one.
type folderOrgLookupAdapter struct{ repo *folder.Repository }

func (a folderOrgLookupAdapter) OrgIDOf(ctx context.Context, folderID string) (string, error) {
	f, err := a.repo.Get(ctx, folderID)
	if err != nil {
		return "", err
	}
	return f.OrgID, nil
}

type fixture struct {
	ownerID    string
	orgID      string
	folderID   string
	folderRepo *folder.Repository
	fileRepo   *file.Repository
	svc        *file.Service
}

func newFixture(t *testing.T, pool *pgxpool.Pool) fixture {
	t.Helper()
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	authRepo := auth.NewRepository(pool)
	user, err := authRepo.CreateUser(ctx, fmt.Sprintf("file-%s@nimbus.test", suffix), "irrelevant-hash")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	orgRepo := org.NewRepository(pool)
	o, err := orgRepo.CreateWithOwner(ctx, "File Test Org "+suffix, user.ID)
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	folderRepo := folder.NewRepository(pool)
	f, err := folderRepo.Create(ctx, o.ID, nil, "File Test Folder")
	if err != nil {
		t.Fatalf("create folder: %v", err)
	}
	fileRepo := file.NewRepository(pool)
	svc := file.NewService(fileRepo, folderOrgLookupAdapter{repo: folderRepo}, nil, nil)
	return fixture{ownerID: user.ID, orgID: o.ID, folderID: f.ID, folderRepo: folderRepo, fileRepo: fileRepo, svc: svc}
}

func TestCreateWithVersion_PointsLatestVersionAtTheNewVersion(t *testing.T) {
	pool := newTestPool(t)
	fx := newFixture(t, pool)
	ctx := context.Background()
	seedChunk(t, pool, fakeChecksum("chunk-1"))

	fileID, versionID, err := fx.fileRepo.CreateWithVersion(ctx, fx.orgID, fx.folderID, "v1.bin", fx.ownerID, 100, fakeChecksum("v1"), "application/octet-stream", []string{fakeChecksum("chunk-1")})
	if err != nil {
		t.Fatalf("CreateWithVersion: %v", err)
	}

	f, err := fx.fileRepo.Get(ctx, fileID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if f.LatestVersionID == nil || *f.LatestVersionID != versionID {
		t.Fatalf("LatestVersionID = %v, want %s", f.LatestVersionID, versionID)
	}

	v, err := fx.fileRepo.GetVersion(ctx, fileID, versionID)
	if err != nil {
		t.Fatalf("GetVersion: %v", err)
	}
	if v.ChecksumSHA256 != fakeChecksum("v1") {
		t.Fatalf("checksum = %s, want checksum-v1", v.ChecksumSHA256)
	}

	chunks, err := fx.fileRepo.GetVersionChunks(ctx, versionID)
	if err != nil {
		t.Fatalf("GetVersionChunks: %v", err)
	}
	if len(chunks) != 1 || chunks[0].Hash != fakeChecksum("chunk-1") {
		t.Fatalf("chunks = %+v, want one chunk 'chunk-1'", chunks)
	}
}

func TestAddVersionThenRestoreVersion_MirrorsSmokeVersionsFlow(t *testing.T) {
	pool := newTestPool(t)
	fx := newFixture(t, pool)
	ctx := context.Background()
	seedChunk(t, pool, fakeChecksum("chunk-1"))
	seedChunk(t, pool, fakeChecksum("chunk-2a"))
	seedChunk(t, pool, fakeChecksum("chunk-2b"))

	fileID, v1, err := fx.fileRepo.CreateWithVersion(ctx, fx.orgID, fx.folderID, "doc.bin", fx.ownerID, 100, fakeChecksum("v1"), "text/plain", []string{fakeChecksum("chunk-1")})
	if err != nil {
		t.Fatalf("CreateWithVersion: %v", err)
	}

	v2, err := fx.fileRepo.AddVersion(ctx, fx.orgID, fileID, fx.ownerID, 200, fakeChecksum("v2"), "text/plain", []string{fakeChecksum("chunk-2a"), fakeChecksum("chunk-2b")})
	if err != nil {
		t.Fatalf("AddVersion: %v", err)
	}

	f, err := fx.fileRepo.Get(ctx, fileID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if f.LatestVersionID == nil || *f.LatestVersionID != v2 {
		t.Fatalf("after AddVersion, LatestVersionID = %v, want %s (the new version)", f.LatestVersionID, v2)
	}

	versions, err := fx.fileRepo.ListVersions(ctx, fileID)
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("version count = %d, want 2", len(versions))
	}

	// Restore v1 — the checksum a downstream client would see must revert.
	restored, err := fx.fileRepo.RestoreVersion(ctx, fileID, v1)
	if err != nil {
		t.Fatalf("RestoreVersion: %v", err)
	}
	if restored.LatestVersionID == nil || *restored.LatestVersionID != v1 {
		t.Fatalf("after RestoreVersion, LatestVersionID = %v, want %s (v1)", restored.LatestVersionID, v1)
	}
	current, err := fx.fileRepo.GetVersion(ctx, fileID, *restored.LatestVersionID)
	if err != nil {
		t.Fatalf("GetVersion after restore: %v", err)
	}
	if current.ChecksumSHA256 != fakeChecksum("v1") {
		t.Fatalf("checksum after restore = %s, want checksum-v1", current.ChecksumSHA256)
	}
	// v2 must still exist in the version list — restore repoints, it doesn't delete.
	versions, err = fx.fileRepo.ListVersions(ctx, fileID)
	if err != nil {
		t.Fatalf("ListVersions after restore: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("version count after restore = %d, want 2 (restore repoints, does not delete)", len(versions))
	}
}

func TestUpdate_MoveToCrossOrgFolderRejected(t *testing.T) {
	pool := newTestPool(t)
	fxA := newFixture(t, pool)
	fxB := newFixture(t, pool)
	ctx := context.Background()

	fileID, _, err := fxA.fileRepo.CreateWithVersion(ctx, fxA.orgID, fxA.folderID, "cross.bin", fxA.ownerID, 10, "checksum", "text/plain", nil)
	if err != nil {
		t.Fatalf("CreateWithVersion: %v", err)
	}
	f, err := fxA.fileRepo.Get(ctx, fileID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	_, err = fxA.svc.Update(ctx, f, nil, &fxB.folderID)
	if !errors.Is(err, file.ErrInvalidFolder) {
		t.Fatalf("got err %v, want ErrInvalidFolder for a move into another org's folder", err)
	}
}

func TestDeleteRestorePurge_Lifecycle(t *testing.T) {
	pool := newTestPool(t)
	fx := newFixture(t, pool)
	ctx := context.Background()

	fileID, _, err := fx.fileRepo.CreateWithVersion(ctx, fx.orgID, fx.folderID, "lifecycle.bin", fx.ownerID, 10, "checksum", "text/plain", nil)
	if err != nil {
		t.Fatalf("CreateWithVersion: %v", err)
	}
	if used, err := fx.fileRepo.OrgUsageBytes(ctx, fx.orgID); err != nil || used != 10 {
		t.Fatalf("OrgUsageBytes after create = (%d, %v), want (10, nil)", used, err)
	}

	if err := fx.svc.Delete(ctx, fileID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := fx.fileRepo.Get(ctx, fileID); !errors.Is(err, file.ErrNotFound) {
		t.Fatalf("trashed file should 404 via Get, got err %v", err)
	}

	// Purge before restore is refused — trash-then-purge is the only path.
	live, err := fx.fileRepo.GetAny(ctx, fileID)
	if err != nil {
		t.Fatalf("GetAny: %v", err)
	}
	live.DeletedAt = nil // simulate calling Purge on a file that was never trashed
	if err := fx.svc.Purge(ctx, live); !errors.Is(err, file.ErrNotTrashed) {
		t.Fatalf("got err %v, want ErrNotTrashed for purging a live-looking file", err)
	}

	if err := fx.svc.Restore(ctx, fileID); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if _, err := fx.fileRepo.Get(ctx, fileID); err != nil {
		t.Fatalf("file should be live again after restore, got err %v", err)
	}

	if err := fx.svc.Delete(ctx, fileID); err != nil {
		t.Fatalf("re-delete: %v", err)
	}
	trashedFile, err := fx.fileRepo.GetAny(ctx, fileID)
	if err != nil {
		t.Fatalf("GetAny: %v", err)
	}
	if err := fx.svc.Purge(ctx, trashedFile); err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if _, err := fx.fileRepo.GetAny(ctx, fileID); !errors.Is(err, file.ErrNotFound) {
		t.Fatalf("purged file should be gone even via GetAny, got err %v", err)
	}
	// Audit §06: usage_bytes is now a maintained counter (was a live SUM
	// join) — Purge is one of the write paths that has to credit it back,
	// or a trashed-then-purged file's bytes would sit stuck against quota
	// forever despite the file being provably gone.
	if used, err := fx.fileRepo.OrgUsageBytes(ctx, fx.orgID); err != nil || used != 0 {
		t.Fatalf("OrgUsageBytes after Purge = (%d, %v), want (0, nil) — the only file in this org is now gone", used, err)
	}
}

func TestOrgUsageBytesAndFileCounts_ReflectLiveAndTrashedFiles(t *testing.T) {
	pool := newTestPool(t)
	fx := newFixture(t, pool)
	ctx := context.Background()

	liveID, _, err := fx.fileRepo.CreateWithVersion(ctx, fx.orgID, fx.folderID, "live.bin", fx.ownerID, 1000, fakeChecksum("live"), "text/plain", nil)
	if err != nil {
		t.Fatalf("create live: %v", err)
	}
	trashedID, _, err := fx.fileRepo.CreateWithVersion(ctx, fx.orgID, fx.folderID, "trashed.bin", fx.ownerID, 500, fakeChecksum("trashed"), "text/plain", nil)
	if err != nil {
		t.Fatalf("create trashed: %v", err)
	}
	if err := fx.fileRepo.SoftDelete(ctx, trashedID); err != nil {
		t.Fatalf("SoftDelete: %v", err)
	}
	_ = liveID

	used, err := fx.fileRepo.OrgUsageBytes(ctx, fx.orgID)
	if err != nil {
		t.Fatalf("OrgUsageBytes: %v", err)
	}
	if used != 1500 {
		t.Fatalf("OrgUsageBytes = %d, want 1500 (trashed bytes still count until purge)", used)
	}

	live, trashed, err := fx.fileRepo.OrgFileCounts(ctx, fx.orgID)
	if err != nil {
		t.Fatalf("OrgFileCounts: %v", err)
	}
	if live != 1 || trashed != 1 {
		t.Fatalf("OrgFileCounts = (live=%d, trashed=%d), want (1, 1)", live, trashed)
	}
}

func TestPurgeExpiredTrash_OnlyPurgesPastRetention(t *testing.T) {
	pool := newTestPool(t)
	fx := newFixture(t, pool)
	ctx := context.Background()

	recentID, _, err := fx.fileRepo.CreateWithVersion(ctx, fx.orgID, fx.folderID, "recent.bin", fx.ownerID, 10, fakeChecksum("recent"), "text/plain", nil)
	if err != nil {
		t.Fatalf("create recent: %v", err)
	}
	if err := fx.fileRepo.SoftDelete(ctx, recentID); err != nil {
		t.Fatalf("SoftDelete recent: %v", err)
	}

	expiredID, _, err := fx.fileRepo.CreateWithVersion(ctx, fx.orgID, fx.folderID, "expired.bin", fx.ownerID, 10, fakeChecksum("expired"), "text/plain", nil)
	if err != nil {
		t.Fatalf("create expired: %v", err)
	}
	if err := fx.fileRepo.SoftDelete(ctx, expiredID); err != nil {
		t.Fatalf("SoftDelete expired: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE files SET deleted_at = now() - interval '31 days' WHERE id = $1`, expiredID); err != nil {
		t.Fatalf("backdate: %v", err)
	}

	purged, err := fx.fileRepo.PurgeExpiredTrash(ctx, 30*24*time.Hour)
	if err != nil {
		t.Fatalf("PurgeExpiredTrash: %v", err)
	}
	if purged != 1 {
		t.Fatalf("purged = %d, want exactly 1", purged)
	}
	if _, err := fx.fileRepo.GetAny(ctx, expiredID); !errors.Is(err, file.ErrNotFound) {
		t.Fatalf("expired file should be gone, got err %v", err)
	}
	if _, err := fx.fileRepo.GetAny(ctx, recentID); err != nil {
		t.Fatalf("recently trashed file should survive, got err %v", err)
	}
	// Only the expired file's 10 bytes come off usage_bytes — the recent
	// one is still trashed-but-not-purged, so its bytes still count
	// (matching OrgUsageBytes's own trashed-still-counts semantics).
	if used, err := fx.fileRepo.OrgUsageBytes(ctx, fx.orgID); err != nil || used != 10 {
		t.Fatalf("OrgUsageBytes after PurgeExpiredTrash = (%d, %v), want (10, nil) — only recent.bin's bytes remain", used, err)
	}
}
