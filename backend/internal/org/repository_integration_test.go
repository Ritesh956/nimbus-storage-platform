//go:build integration

// Audit §14: org — organizations, membership, and the three-tier RBAC
// ladder (owner > admin > member) added in the governance session — had
// zero automated tests despite gating every write endpoint in the app.
// Gated behind the "integration" build tag, matching the house style set by
// internal/auth/refresh_integration_test.go and
// internal/upload/complete_race_integration_test.go: run explicitly with
// `go test -tags=integration ./...`.
package org_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"nimbus/internal/auth"
	"nimbus/internal/org"
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

// userLookupAdapter mirrors cmd/api/adapters.go's real userLookupAdapter —
// it can't be imported here (cmd/api imports org, so importing it back
// would cycle), but the wrapper is two lines and this keeps the test
// exercising the same interface shape production wiring does.
type userLookupAdapter struct{ repo *auth.Repository }

func (a userLookupAdapter) GetUserByEmail(ctx context.Context, email string) (string, error) {
	u, err := a.repo.GetUserByEmail(ctx, email)
	if err != nil {
		return "", err
	}
	return u.ID, nil
}

// noopFolderCreator satisfies org.FolderCreator without needing the folder
// module wired up — org.Service.Create treats a failing/absent root-folder
// creation as non-fatal by design (see service.go's comment on Create), so
// tests that don't care about the default folder can use this.
type noopFolderCreator struct{}

func (noopFolderCreator) CreateRoot(ctx context.Context, orgID, name string) error { return nil }

func newTestUser(t *testing.T, pool *pgxpool.Pool, label string) auth.User {
	t.Helper()
	authRepo := auth.NewRepository(pool)
	email := fmt.Sprintf("%s-%d@nimbus.test", label, time.Now().UnixNano())
	u, err := authRepo.CreateUser(context.Background(), email, "irrelevant-hash")
	if err != nil {
		t.Fatalf("create user %s: %v", label, err)
	}
	return u
}

func TestCreateWithOwner_OrgAndOwnerMembershipComeIntoExistenceTogether(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	owner := newTestUser(t, pool, "owner")

	repo := org.NewRepository(pool)
	o, err := repo.CreateWithOwner(ctx, "Test Org", owner.ID)
	if err != nil {
		t.Fatalf("CreateWithOwner: %v", err)
	}
	if o.OwnerUserID != owner.ID {
		t.Fatalf("org owner = %s, want %s", o.OwnerUserID, owner.ID)
	}

	m, err := repo.GetMembership(ctx, o.ID, owner.ID)
	if err != nil {
		t.Fatalf("GetMembership: %v", err)
	}
	if m.Role != org.RoleOwner {
		t.Fatalf("owner's own membership role = %s, want owner", m.Role)
	}

	orgs, err := repo.ListForUser(ctx, owner.ID)
	if err != nil {
		t.Fatalf("ListForUser: %v", err)
	}
	found := false
	for _, lo := range orgs {
		if lo.ID == o.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("ListForUser did not include the org the caller just created")
	}
}

func TestGetMembership_NonMemberReturnsErrNotMember(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	owner := newTestUser(t, pool, "owner")
	stranger := newTestUser(t, pool, "stranger")

	repo := org.NewRepository(pool)
	o, err := repo.CreateWithOwner(ctx, "Test Org", owner.ID)
	if err != nil {
		t.Fatalf("CreateWithOwner: %v", err)
	}

	_, err = repo.GetMembership(ctx, o.ID, stranger.ID)
	if !errors.Is(err, org.ErrNotMember) {
		t.Fatalf("got err %v, want ErrNotMember", err)
	}
}

func TestAddMemberByEmail_OwnerCanGrantAdminAndMember(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	owner := newTestUser(t, pool, "owner")
	admin := newTestUser(t, pool, "admin")
	member := newTestUser(t, pool, "member")

	repo := org.NewRepository(pool)
	o, err := repo.CreateWithOwner(ctx, "Test Org", owner.ID)
	if err != nil {
		t.Fatalf("CreateWithOwner: %v", err)
	}

	authRepo := auth.NewRepository(pool)
	svc := org.NewService(repo, userLookupAdapter{repo: authRepo}, noopFolderCreator{}, org.UsageSources{}, 0)

	if _, err := svc.AddMemberByEmail(ctx, o.ID, admin.Email, org.RoleAdmin, org.RoleOwner); err != nil {
		t.Fatalf("owner granting admin: %v", err)
	}
	if _, err := svc.AddMemberByEmail(ctx, o.ID, member.Email, org.RoleMember, org.RoleOwner); err != nil {
		t.Fatalf("owner granting member: %v", err)
	}

	adminM, err := repo.GetMembership(ctx, o.ID, admin.ID)
	if err != nil || adminM.Role != org.RoleAdmin {
		t.Fatalf("admin membership = %+v, err = %v, want role=admin", adminM, err)
	}
	memberM, err := repo.GetMembership(ctx, o.ID, member.ID)
	if err != nil || memberM.Role != org.RoleMember {
		t.Fatalf("member membership = %+v, err = %v, want role=member", memberM, err)
	}
}

func TestAddMemberByEmail_AdminCannotGrantAdminEvenViaTheServiceLayer(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	owner := newTestUser(t, pool, "owner")
	target := newTestUser(t, pool, "target")

	repo := org.NewRepository(pool)
	o, err := repo.CreateWithOwner(ctx, "Test Org", owner.ID)
	if err != nil {
		t.Fatalf("CreateWithOwner: %v", err)
	}

	authRepo := auth.NewRepository(pool)
	svc := org.NewService(repo, userLookupAdapter{repo: authRepo}, noopFolderCreator{}, org.UsageSources{}, 0)

	_, err = svc.AddMemberByEmail(ctx, o.ID, target.Email, org.RoleAdmin, org.RoleAdmin)
	if !errors.Is(err, org.ErrElevatedRoleNeedsOwner) {
		t.Fatalf("got err %v, want ErrElevatedRoleNeedsOwner", err)
	}
	// And no membership row should have been created.
	if _, err := repo.GetMembership(ctx, o.ID, target.ID); !errors.Is(err, org.ErrNotMember) {
		t.Fatalf("expected target to remain a non-member, got err %v", err)
	}
}

func TestAddMemberByEmail_DuplicateReturnsErrAlreadyMember(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	owner := newTestUser(t, pool, "owner")
	target := newTestUser(t, pool, "target")

	repo := org.NewRepository(pool)
	o, err := repo.CreateWithOwner(ctx, "Test Org", owner.ID)
	if err != nil {
		t.Fatalf("CreateWithOwner: %v", err)
	}
	authRepo := auth.NewRepository(pool)
	svc := org.NewService(repo, userLookupAdapter{repo: authRepo}, noopFolderCreator{}, org.UsageSources{}, 0)

	if _, err := svc.AddMemberByEmail(ctx, o.ID, target.Email, org.RoleMember, org.RoleOwner); err != nil {
		t.Fatalf("first add: %v", err)
	}
	if _, err := svc.AddMemberByEmail(ctx, o.ID, target.Email, org.RoleMember, org.RoleOwner); !errors.Is(err, org.ErrAlreadyMember) {
		t.Fatalf("second add: got err %v, want ErrAlreadyMember", err)
	}
}

func TestRemoveMember_OwnerCanNeverBeRemoved(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	owner := newTestUser(t, pool, "owner")

	repo := org.NewRepository(pool)
	o, err := repo.CreateWithOwner(ctx, "Test Org", owner.ID)
	if err != nil {
		t.Fatalf("CreateWithOwner: %v", err)
	}
	authRepo := auth.NewRepository(pool)
	svc := org.NewService(repo, userLookupAdapter{repo: authRepo}, noopFolderCreator{}, org.UsageSources{}, 0)

	if err := svc.RemoveMember(ctx, o.ID, owner.ID, org.RoleOwner); !errors.Is(err, org.ErrCannotRemoveOwner) {
		t.Fatalf("owner removing self: got err %v, want ErrCannotRemoveOwner", err)
	}
}

func TestRemoveMember_AdminCanOnlyRemovePlainMembers(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	owner := newTestUser(t, pool, "owner")
	adminA := newTestUser(t, pool, "admin-a")
	adminB := newTestUser(t, pool, "admin-b")
	member := newTestUser(t, pool, "member")

	repo := org.NewRepository(pool)
	o, err := repo.CreateWithOwner(ctx, "Test Org", owner.ID)
	if err != nil {
		t.Fatalf("CreateWithOwner: %v", err)
	}
	authRepo := auth.NewRepository(pool)
	svc := org.NewService(repo, userLookupAdapter{repo: authRepo}, noopFolderCreator{}, org.UsageSources{}, 0)

	for _, u := range []struct {
		id   string
		role org.Role
	}{{adminA.ID, org.RoleAdmin}, {adminB.ID, org.RoleAdmin}, {member.ID, org.RoleMember}} {
		if err := repo.AddMember(ctx, o.ID, u.id, u.role); err != nil {
			t.Fatalf("seed member %s: %v", u.id, err)
		}
	}

	// adminA (an admin caller) tries to remove adminB (a peer) — must fail.
	if err := svc.RemoveMember(ctx, o.ID, adminB.ID, org.RoleAdmin); !errors.Is(err, org.ErrAdminRemovesMembersOnly) {
		t.Fatalf("admin removing another admin: got err %v, want ErrAdminRemovesMembersOnly", err)
	}
	if _, err := repo.GetMembership(ctx, o.ID, adminB.ID); err != nil {
		t.Fatalf("adminB should still be a member after the rejected removal, got err %v", err)
	}

	// adminA removing a plain member — must succeed.
	if err := svc.RemoveMember(ctx, o.ID, member.ID, org.RoleAdmin); err != nil {
		t.Fatalf("admin removing a plain member: %v", err)
	}
	if _, err := repo.GetMembership(ctx, o.ID, member.ID); !errors.Is(err, org.ErrNotMember) {
		t.Fatalf("expected member to be removed, got err %v", err)
	}

	// The owner, in contrast, can remove another admin outright.
	if err := svc.RemoveMember(ctx, o.ID, adminB.ID, org.RoleOwner); err != nil {
		t.Fatalf("owner removing an admin: %v", err)
	}
}

// Minimal stubs for org.UsageSources — Usage() is otherwise a thin
// aggregation over three ports plus repo.ListMembers, so this proves the
// aggregation math (quota passthrough, per-member shape) without needing
// the file/sharing/activity modules wired up.
type stubStorageStats struct{ stats org.StorageStats }

func (s stubStorageStats) OrgStorageStats(ctx context.Context, orgID string) (org.StorageStats, error) {
	return s.stats, nil
}

type stubShareLinkCounter struct{ count int }

func (s stubShareLinkCounter) ActiveLinkCount(ctx context.Context, orgID string) (int, error) {
	return s.count, nil
}

type stubActivityStats struct{}

func (stubActivityStats) ActorStats(ctx context.Context, orgID string, since time.Time) (map[string]org.MemberActivityStat, error) {
	return map[string]org.MemberActivityStat{}, nil
}

func (stubActivityStats) VerbCounts(ctx context.Context, orgID string, since time.Time) (map[string]int, error) {
	return map[string]int{"upload": 3}, nil
}

func TestUsage_AggregatesStorageSharesAndMembers(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	owner := newTestUser(t, pool, "owner")

	repo := org.NewRepository(pool)
	o, err := repo.CreateWithOwner(ctx, "Test Org", owner.ID)
	if err != nil {
		t.Fatalf("CreateWithOwner: %v", err)
	}

	const quotaBytes = int64(5 * 1024 * 1024 * 1024)
	usage := org.UsageSources{
		Storage:  stubStorageStats{stats: org.StorageStats{UsedBytes: 1024, LiveFiles: 2, TrashedFiles: 1}},
		Shares:   stubShareLinkCounter{count: 4},
		Activity: stubActivityStats{},
	}
	svc := org.NewService(repo, nil, nil, usage, quotaBytes)

	u, err := svc.Usage(ctx, o.ID)
	if err != nil {
		t.Fatalf("Usage: %v", err)
	}
	if u.Storage.QuotaBytes != quotaBytes {
		t.Fatalf("QuotaBytes = %d, want %d (passthrough of the configured value)", u.Storage.QuotaBytes, quotaBytes)
	}
	if u.Storage.UsedBytes != 1024 || u.Storage.LiveFiles != 2 || u.Storage.TrashedFiles != 1 {
		t.Fatalf("storage stats = %+v, want the stub's values", u.Storage)
	}
	if u.ActiveShareLinks != 4 {
		t.Fatalf("ActiveShareLinks = %d, want 4", u.ActiveShareLinks)
	}
	if len(u.Members) != 1 || u.Members[0].UserID != owner.ID || u.Members[0].Role != org.RoleOwner {
		t.Fatalf("Members = %+v, want exactly the owner", u.Members)
	}
	if u.Activity30d["upload"] != 3 {
		t.Fatalf("Activity30d = %+v, want the stub's verb counts", u.Activity30d)
	}
}
