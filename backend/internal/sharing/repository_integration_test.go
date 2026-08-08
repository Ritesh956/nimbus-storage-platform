//go:build integration

// Audit §14 named sharing as the single highest-value smoke-script port
// target (scripts/smoke-sharing.js): create-share, expiry enforcement,
// scope checks, and revoke authorization, against real Postgres. Gated
// behind the "integration" build tag, matching the house style set by
// internal/auth/refresh_integration_test.go.
package sharing_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"nimbus/internal/auth"
	"nimbus/internal/file"
	"nimbus/internal/folder"
	"nimbus/internal/org"
	"nimbus/internal/sharing"
	"nimbus/internal/upload"
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

// The adapters below mirror cmd/api/adapters.go's real ones exactly — they
// can't be imported directly (cmd/api imports sharing, so the reverse would
// cycle) — plus a canned DownloadPlanner stub that sidesteps needing a real
// storage.Router/MinIO for tests that only care about share scoping and
// expiry, not actual chunk presigning.

type fileShareLookupAdapter struct{ repo *file.Repository }

func (a fileShareLookupAdapter) GetForShare(ctx context.Context, fileID string) (sharing.FileInfo, error) {
	f, err := a.repo.Get(ctx, fileID)
	if err != nil {
		return sharing.FileInfo{}, err
	}
	info := sharing.FileInfo{ID: f.ID, Name: f.Name, LatestVersionID: f.LatestVersionID}
	if f.LatestVersionID != nil {
		v, err := a.repo.GetVersion(ctx, f.ID, *f.LatestVersionID)
		if err != nil {
			return sharing.FileInfo{}, err
		}
		info.SizeBytes, info.MimeType, info.ChecksumSHA256 = v.SizeBytes, v.MimeType, v.ChecksumSHA256
	}
	return info, nil
}

type fileScopeAdapter struct{ repo *file.Repository }

func (a fileScopeAdapter) LiveFileOrg(ctx context.Context, fileID string) (string, error) {
	f, err := a.repo.Get(ctx, fileID)
	if err != nil {
		return "", err
	}
	return f.OrgID, nil
}

func (a fileScopeAdapter) LiveFileFolder(ctx context.Context, fileID string) (string, error) {
	f, err := a.repo.Get(ctx, fileID)
	if err != nil {
		return "", err
	}
	return f.FolderID, nil
}

type folderShareAdapter struct {
	folders *folder.Repository
	files   *file.Repository
}

func (a folderShareAdapter) GetFolderForShare(ctx context.Context, folderID string) (sharing.FolderInfo, error) {
	f, err := a.folders.Get(ctx, folderID)
	if err != nil {
		return sharing.FolderInfo{}, err
	}
	return sharing.FolderInfo{ID: f.ID, Name: f.Name}, nil
}

func (a folderShareAdapter) ListShareChildren(ctx context.Context, folderID string) ([]sharing.FolderInfo, []sharing.FileInfo, error) {
	f, err := a.folders.Get(ctx, folderID)
	if err != nil {
		return nil, nil, err
	}
	subfolders, err := a.folders.ListChildren(ctx, f.OrgID, &f.ID)
	if err != nil {
		return nil, nil, err
	}
	folderInfos := make([]sharing.FolderInfo, len(subfolders))
	for i, sf := range subfolders {
		folderInfos[i] = sharing.FolderInfo{ID: sf.ID, Name: sf.Name}
	}
	entries, err := a.files.ListInFolder(ctx, folderID)
	if err != nil {
		return nil, nil, err
	}
	fileInfos := make([]sharing.FileInfo, len(entries))
	for i, e := range entries {
		info := sharing.FileInfo{ID: e.ID, Name: e.Name, LatestVersionID: e.LatestVersionID}
		if e.SizeBytes != nil {
			info.SizeBytes = *e.SizeBytes
		}
		if e.MimeType != nil {
			info.MimeType = *e.MimeType
		}
		fileInfos[i] = info
	}
	return folderInfos, fileInfos, nil
}

func (a folderShareAdapter) IsSelfOrDescendant(ctx context.Context, folderID, candidateID string) (bool, error) {
	return a.folders.IsSelfOrDescendant(ctx, folderID, candidateID)
}

// stubDownloadPlanner sidesteps needing a real storage.Router/MinIO — this
// suite is testing share scoping/expiry/authorization, not chunk
// presigning (which file's own tests + storage.ring_test.go already cover).
type stubDownloadPlanner struct{}

func (stubDownloadPlanner) DownloadPlan(ctx context.Context, fileID, versionID string) ([]file.DownloadPlanChunk, error) {
	return []file.DownloadPlanChunk{{Sequence: 0, Hash: "stub-hash", Targets: []string{"http://stub/chunk"}}}, nil
}

type memberOnlyChecker struct{ isMember bool }

func (m memberOnlyChecker) IsMember(ctx context.Context, orgID, userID string) (bool, error) {
	return m.isMember, nil
}

type testFixture struct {
	ownerID  string
	orgID    string
	folderID string
	pool     *pgxpool.Pool
	fileRepo *file.Repository
	svc      *sharing.Service
	repo     *sharing.Repository
}

func newFixture(t *testing.T, pool *pgxpool.Pool) testFixture {
	t.Helper()
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	authRepo := auth.NewRepository(pool)
	user, err := authRepo.CreateUser(ctx, fmt.Sprintf("share-%s@nimbus.test", suffix), "irrelevant-hash")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	orgRepo := org.NewRepository(pool)
	o, err := orgRepo.CreateWithOwner(ctx, "Share Test Org "+suffix, user.ID)
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	folderRepo := folder.NewRepository(pool)
	f, err := folderRepo.Create(ctx, o.ID, nil, "Share Test Folder")
	if err != nil {
		t.Fatalf("create folder: %v", err)
	}

	fileRepo := file.NewRepository(pool)
	shareRepo := sharing.NewRepository(pool)
	svc := sharing.NewService(shareRepo,
		fileShareLookupAdapter{repo: fileRepo},
		fileScopeAdapter{repo: fileRepo},
		folderShareAdapter{folders: folderRepo, files: fileRepo},
		stubDownloadPlanner{},
	)
	return testFixture{ownerID: user.ID, orgID: o.ID, folderID: f.ID, pool: pool, fileRepo: fileRepo, svc: svc, repo: shareRepo}
}

// fakeChecksum returns a real-shaped 64-hex-char SHA-256 digest —
// file_versions.checksum_sha256 is a fixed-length char(64) column, so a
// short placeholder string comes back from Postgres space-padded to 64
// chars and fails any equality check against the original short string
// (caught by an actual failing test run, not by vet/build).
func fakeChecksum(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:])
}

func (fx testFixture) createFile(t *testing.T, name, checksumSeed string) (fileID, versionID string) {
	t.Helper()
	checksum := fakeChecksum(checksumSeed)
	ctx := context.Background()
	// file_version_chunks.chunk_hash FKs to chunks(hash) — a real upload
	// commits the chunk (upload.Service.CommitChunk) before a version can
	// reference it; UpsertGlobalChunk's ON CONFLICT arm makes this
	// idempotent, so reusing "chunk-a" as content shared across multiple
	// files in the same test (real dedup) is safe to upsert every call.
	if err := upload.NewRepository(fx.pool).UpsertGlobalChunk(ctx, "chunk-a", 1024); err != nil {
		t.Fatalf("seed chunk-a: %v", err)
	}
	fileID, versionID, err := fx.fileRepo.CreateWithVersion(ctx, fx.orgID, fx.folderID, name, fx.ownerID, 1024, checksum, "application/octet-stream", []string{"chunk-a"})
	if err != nil {
		t.Fatalf("create file %s: %v", name, err)
	}
	return fileID, versionID
}

func TestCreateShareAndResolve_RoundTripsFileInfoAndChecksum(t *testing.T) {
	pool := newTestPool(t)
	fx := newFixture(t, pool)
	fileID, _ := fx.createFile(t, "share-a.bin", "checksum-abc123")

	future := time.Now().Add(time.Hour)
	link, err := fx.svc.CreateShare(context.Background(), fx.orgID, fileID, fx.ownerID, &future)
	if err != nil {
		t.Fatalf("CreateShare: %v", err)
	}
	if link.Token == "" {
		t.Fatal("expected a non-empty token")
	}

	resolved, err := fx.svc.Resolve(context.Background(), link.Token)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolved.Kind != sharing.KindFile {
		t.Fatalf("Kind = %s, want file", resolved.Kind)
	}
	if resolved.File.ID != fileID {
		t.Fatalf("resolved file ID = %s, want %s", resolved.File.ID, fileID)
	}
	if resolved.File.ChecksumSHA256 != fakeChecksum("checksum-abc123") {
		t.Fatalf("resolved checksum = %s, want the seeded checksum (public share info must round-trip the real version checksum)", resolved.File.ChecksumSHA256)
	}
	if len(resolved.DownloadPlan) != 1 {
		t.Fatalf("download plan chunks = %d, want 1", len(resolved.DownloadPlan))
	}
}

func TestResolve_UnknownTokenReturnsErrNotFound(t *testing.T) {
	pool := newTestPool(t)
	fx := newFixture(t, pool)
	_, err := fx.svc.Resolve(context.Background(), "nonexistent-token-xyz")
	if !errors.Is(err, sharing.ErrNotFound) {
		t.Fatalf("got err %v, want ErrNotFound", err)
	}
}

func TestResolve_NaturallyExpiredLinkRejected(t *testing.T) {
	pool := newTestPool(t)
	fx := newFixture(t, pool)
	fileID, _ := fx.createFile(t, "share-expired.bin", "checksum-expired")

	// The handler layer refuses to accept a past expires_at at creation time
	// (parseCreateShare, handler_test.go) — this reaches the same state a
	// link that was valid when created but has since elapsed would reach,
	// exercising Resolve's own expiry check independently of the handler's.
	past := time.Now().Add(-time.Minute)
	link, err := fx.svc.CreateShare(context.Background(), fx.orgID, fileID, fx.ownerID, &past)
	if err != nil {
		t.Fatalf("CreateShare: %v", err)
	}

	_, err = fx.svc.Resolve(context.Background(), link.Token)
	if !errors.Is(err, sharing.ErrExpired) {
		t.Fatalf("got err %v, want ErrExpired", err)
	}
}

func TestDelete_NonMemberCannotRevoke(t *testing.T) {
	pool := newTestPool(t)
	fx := newFixture(t, pool)
	fileID, _ := fx.createFile(t, "share-revoke-a.bin", "checksum-a")
	future := time.Now().Add(time.Hour)
	link, err := fx.svc.CreateShare(context.Background(), fx.orgID, fileID, fx.ownerID, &future)
	if err != nil {
		t.Fatalf("CreateShare: %v", err)
	}

	h := sharing.NewHandler(fx.svc, memberOnlyChecker{isMember: false})
	req := httptest.NewRequest(http.MethodDelete, "/v1/shares/"+link.Token, nil)
	req.SetPathValue("token", link.Token)
	w := httptest.NewRecorder()
	h.Delete(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 for a non-member revoke attempt", w.Code)
	}
	// The link must still resolve — a rejected revoke must not have side effects.
	if _, err := fx.svc.Resolve(context.Background(), link.Token); err != nil {
		t.Fatalf("link should still be live after a rejected revoke, got: %v", err)
	}
}

func TestDelete_MemberCanRevokeAndLinkIsGoneAfter(t *testing.T) {
	pool := newTestPool(t)
	fx := newFixture(t, pool)
	fileID, _ := fx.createFile(t, "share-revoke-b.bin", "checksum-b")
	future := time.Now().Add(time.Hour)
	link, err := fx.svc.CreateShare(context.Background(), fx.orgID, fileID, fx.ownerID, &future)
	if err != nil {
		t.Fatalf("CreateShare: %v", err)
	}

	h := sharing.NewHandler(fx.svc, memberOnlyChecker{isMember: true})
	req := httptest.NewRequest(http.MethodDelete, "/v1/shares/"+link.Token, nil)
	req.SetPathValue("token", link.Token)
	w := httptest.NewRecorder()
	h.Delete(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 for an authorized revoke", w.Code)
	}
	if _, err := fx.svc.Resolve(context.Background(), link.Token); !errors.Is(err, sharing.ErrNotFound) {
		t.Fatalf("expected the link to 404 after revoke, got: %v", err)
	}
}

func TestCreateBundleShare_MixedOrgFilesRejectedEndToEnd(t *testing.T) {
	pool := newTestPool(t)
	fx := newFixture(t, pool)
	otherFx := newFixture(t, pool) // a second, unrelated org
	fileA, _ := fx.createFile(t, "bundle-a.bin", "checksum-bundle-a")
	fileB, _ := otherFx.createFile(t, "bundle-b.bin", "checksum-bundle-b")

	future := time.Now().Add(time.Hour)
	_, err := fx.svc.CreateBundleShare(context.Background(), fx.orgID, []string{fileA, fileB}, fx.ownerID, &future)
	if !errors.Is(err, sharing.ErrFileNotShareable) {
		t.Fatalf("got err %v, want ErrFileNotShareable for a bundle spanning two orgs", err)
	}
}

func TestCreateBundleShare_ValidMultiFileBundleResolvesAllMembers(t *testing.T) {
	pool := newTestPool(t)
	fx := newFixture(t, pool)
	fileA, _ := fx.createFile(t, "bundle-x.bin", "checksum-x")
	fileB, _ := fx.createFile(t, "bundle-y.bin", "checksum-y")

	future := time.Now().Add(time.Hour)
	link, err := fx.svc.CreateBundleShare(context.Background(), fx.orgID, []string{fileA, fileB}, fx.ownerID, &future)
	if err != nil {
		t.Fatalf("CreateBundleShare: %v", err)
	}

	resolved, err := fx.svc.Resolve(context.Background(), link.Token)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolved.Kind != sharing.KindBundle {
		t.Fatalf("Kind = %s, want bundle", resolved.Kind)
	}
	if len(resolved.Files) != 2 {
		t.Fatalf("bundle resolved %d files, want 2", len(resolved.Files))
	}
}
