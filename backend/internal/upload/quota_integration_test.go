//go:build integration

// Audit §14: quota enforcement had no dedicated test coverage anywhere, not
// even a smoke script — this proves the real end-to-end wiring (InitUpload
// -> checkQuota -> file.Repository.OrgUsageBytes) against real Postgres,
// on top of quota_test.go's unit coverage of checkQuota's own boundary
// logic. Gated behind the "integration" build tag, matching the house style
// set by internal/auth/refresh_integration_test.go.
package upload_test

import (
	"context"
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

func testQuotaPostgresDSN() string {
	if v := os.Getenv("NIMBUS_TEST_POSTGRES_DSN"); v != "" {
		return v
	}
	return "postgres://nimbus:nimbus@localhost:5432/nimbus?sslmode=disable"
}

func newQuotaTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	dsn := testQuotaPostgresDSN()
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

type quotaFolderOrgLookupAdapter struct{ repo *folder.Repository }

func (a quotaFolderOrgLookupAdapter) OrgIDOf(ctx context.Context, folderID string) (string, error) {
	f, err := a.repo.Get(ctx, folderID)
	if err != nil {
		return "", err
	}
	return f.OrgID, nil
}

type quotaMembershipAdapter struct{ repo *org.Repository }

func (a quotaMembershipAdapter) IsMember(ctx context.Context, orgID, userID string) (bool, error) {
	_, err := a.repo.GetMembership(ctx, orgID, userID)
	if errors.Is(err, org.ErrNotMember) {
		return false, nil
	}
	return err == nil, err
}

// TestInitUpload_RejectsWhenNewUploadWouldExceedOrgQuota wires the real
// InitUpload -> checkQuota -> file.Repository.OrgUsageBytes path against a
// fresh org that already has real stored bytes, then asserts a new upload
// declaration that would push it over quota is rejected with 413-equivalent
// ErrQuotaExceeded — and that a declaration comfortably under quota is
// accepted, proving the check isn't just always-reject.
func TestInitUpload_RejectsWhenNewUploadWouldExceedOrgQuota(t *testing.T) {
	pool := newQuotaTestPool(t)
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	authRepo := auth.NewRepository(pool)
	user, err := authRepo.CreateUser(ctx, fmt.Sprintf("quota-%s@nimbus.test", suffix), "irrelevant-hash")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	orgRepo := org.NewRepository(pool)
	o, err := orgRepo.CreateWithOwner(ctx, "Quota Test Org "+suffix, user.ID)
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	folderRepo := folder.NewRepository(pool)
	f, err := folderRepo.Create(ctx, o.ID, nil, "Quota Test Folder")
	if err != nil {
		t.Fatalf("create folder: %v", err)
	}
	fileRepo := file.NewRepository(pool)

	// Seed 800 bytes of already-stored content — checkQuota reads this back
	// via file.Repository.OrgUsageBytes, so this is what makes the test
	// exercise the real Postgres aggregation, not a mock's canned number.
	if _, _, err := fileRepo.CreateWithVersion(ctx, o.ID, f.ID, "already-stored.bin", user.ID, 800, "checksum-seed", "application/octet-stream", nil); err != nil {
		t.Fatalf("seed existing file: %v", err)
	}

	const quotaBytes = int64(1000)
	uploadRepo := upload.NewRepository(pool)
	svc := upload.NewService(uploadRepo, nil, fileRepo, quotaFolderOrgLookupAdapter{repo: folderRepo}, quotaMembershipAdapter{repo: orgRepo},
		nil, nil, fileRepo, orgRepo, 1, 0, 0, 0, quotaBytes)

	// 800 already used + 300 new > 1000 quota -> rejected.
	_, err = svc.InitUpload(ctx, user.ID, f.ID, "", "over-quota.bin", 300, "application/octet-stream")
	if !errors.Is(err, upload.ErrQuotaExceeded) {
		t.Fatalf("got err %v, want ErrQuotaExceeded (800 used + 300 new > 1000 quota)", err)
	}

	// 800 already used + 150 new <= 1000 quota -> accepted, and a real
	// upload session row is created.
	u, err := svc.InitUpload(ctx, user.ID, f.ID, "", "under-quota.bin", 150, "application/octet-stream")
	if err != nil {
		t.Fatalf("got err %v, want nil (800 used + 150 new is within a 1000 quota)", err)
	}
	if u.ID == "" {
		t.Fatal("expected a real upload session to be created for the accepted request")
	}
}

func TestInitUpload_NonMemberRejectedBeforeQuotaEvenApplies(t *testing.T) {
	pool := newQuotaTestPool(t)
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	authRepo := auth.NewRepository(pool)
	owner, err := authRepo.CreateUser(ctx, fmt.Sprintf("quota-owner-%s@nimbus.test", suffix), "irrelevant-hash")
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	stranger, err := authRepo.CreateUser(ctx, fmt.Sprintf("quota-stranger-%s@nimbus.test", suffix), "irrelevant-hash")
	if err != nil {
		t.Fatalf("create stranger: %v", err)
	}
	orgRepo := org.NewRepository(pool)
	o, err := orgRepo.CreateWithOwner(ctx, "Quota Membership Org "+suffix, owner.ID)
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	folderRepo := folder.NewRepository(pool)
	f, err := folderRepo.Create(ctx, o.ID, nil, "Folder")
	if err != nil {
		t.Fatalf("create folder: %v", err)
	}
	fileRepo := file.NewRepository(pool)
	uploadRepo := upload.NewRepository(pool)
	svc := upload.NewService(uploadRepo, nil, fileRepo, quotaFolderOrgLookupAdapter{repo: folderRepo}, quotaMembershipAdapter{repo: orgRepo},
		nil, nil, fileRepo, orgRepo, 1, 0, 0, 0, 0)

	_, err = svc.InitUpload(ctx, stranger.ID, f.ID, "", "intrusion.bin", 10, "application/octet-stream")
	if !errors.Is(err, upload.ErrForbidden) {
		t.Fatalf("got err %v, want ErrForbidden for a non-member's upload attempt", err)
	}
}

// TestInitUpload_PerOrgQuotaOverrideBeatsTheConfiguredDefault proves
// upload.QuotaReader end to end: a per-org override tighter than the
// configured default (audit §06's per-tenant quota gap) is the number
// checkQuota actually enforces, not the default it's satisfied by
// (org.Repository, no adapter needed) — org.Repository.SetQuotaOverride is
// the same path a platform admin's PATCH /v1/orgs/{orgId}/quota drives.
func TestInitUpload_PerOrgQuotaOverrideBeatsTheConfiguredDefault(t *testing.T) {
	pool := newQuotaTestPool(t)
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	authRepo := auth.NewRepository(pool)
	user, err := authRepo.CreateUser(ctx, fmt.Sprintf("quota-override-%s@nimbus.test", suffix), "irrelevant-hash")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	orgRepo := org.NewRepository(pool)
	o, err := orgRepo.CreateWithOwner(ctx, "Quota Override Org "+suffix, user.ID)
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	folderRepo := folder.NewRepository(pool)
	f, err := folderRepo.Create(ctx, o.ID, nil, "Quota Override Folder")
	if err != nil {
		t.Fatalf("create folder: %v", err)
	}
	fileRepo := file.NewRepository(pool)

	// The configured default (100000) would happily allow this 300-byte
	// upload; a much tighter per-org override (200) must not.
	const configuredDefault = int64(100_000)
	override := int64(200)
	if err := orgRepo.SetQuotaOverride(ctx, o.ID, &override); err != nil {
		t.Fatalf("SetQuotaOverride: %v", err)
	}

	uploadRepo := upload.NewRepository(pool)
	svc := upload.NewService(uploadRepo, nil, fileRepo, quotaFolderOrgLookupAdapter{repo: folderRepo}, quotaMembershipAdapter{repo: orgRepo},
		nil, nil, fileRepo, orgRepo, 1, 0, 0, 0, configuredDefault)

	_, err = svc.InitUpload(ctx, user.ID, f.ID, "", "over-override.bin", 300, "application/octet-stream")
	if !errors.Is(err, upload.ErrQuotaExceeded) {
		t.Fatalf("got err %v, want ErrQuotaExceeded — the 200-byte override should have applied, not the 100000-byte default", err)
	}

	// Clearing the override falls back to the configured default, which
	// comfortably allows the same 300-byte upload.
	if err := orgRepo.SetQuotaOverride(ctx, o.ID, nil); err != nil {
		t.Fatalf("clear SetQuotaOverride: %v", err)
	}
	u, err := svc.InitUpload(ctx, user.ID, f.ID, "", "under-default.bin", 300, "application/octet-stream")
	if err != nil {
		t.Fatalf("got err %v, want nil once the override is cleared (falls back to the 100000-byte default)", err)
	}
	if u.ID == "" {
		t.Fatal("expected a real upload session to be created for the accepted request")
	}
}
