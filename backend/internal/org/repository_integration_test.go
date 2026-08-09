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
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
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

// TestQuotaOverride_DefaultsToNilThenRoundTripsSetAndClear covers the
// repository layer directly (audit §06: per-org quota override, previously
// a single global config value with no per-tenant differentiation at all).
func TestQuotaOverride_DefaultsToNilThenRoundTripsSetAndClear(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	owner := newTestUser(t, pool, "quota-owner")

	repo := org.NewRepository(pool)
	o, err := repo.CreateWithOwner(ctx, "Quota Repo Org", owner.ID)
	if err != nil {
		t.Fatalf("CreateWithOwner: %v", err)
	}

	if got, err := repo.QuotaOverride(ctx, o.ID); err != nil || got != nil {
		t.Fatalf("QuotaOverride on a fresh org = (%v, %v), want (nil, nil)", got, err)
	}

	override := int64(12345)
	if err := repo.SetQuotaOverride(ctx, o.ID, &override); err != nil {
		t.Fatalf("SetQuotaOverride: %v", err)
	}
	got, err := repo.QuotaOverride(ctx, o.ID)
	if err != nil || got == nil || *got != override {
		t.Fatalf("QuotaOverride after set = (%v, %v), want (%d, nil)", got, err, override)
	}

	if err := repo.SetQuotaOverride(ctx, o.ID, nil); err != nil {
		t.Fatalf("clear SetQuotaOverride: %v", err)
	}
	if got, err := repo.QuotaOverride(ctx, o.ID); err != nil || got != nil {
		t.Fatalf("QuotaOverride after clear = (%v, %v), want (nil, nil)", got, err)
	}
}

func TestSetQuotaOverride_UnknownOrgReturnsErrNotFound(t *testing.T) {
	pool := newTestPool(t)
	repo := org.NewRepository(pool)
	override := int64(100)
	err := repo.SetQuotaOverride(context.Background(), "00000000-0000-0000-0000-000000000000", &override)
	if !errors.Is(err, org.ErrNotFound) {
		t.Fatalf("got err %v, want org.ErrNotFound for a nonexistent org id", err)
	}
}

// TestEffectiveQuota_OverridePassesThroughToTheUsageView proves the same
// aggregation TestUsage_AggregatesStorageSharesAndMembers checked (with no
// override present) also reflects a real override once one is set — the
// service-layer counterpart to the repository round-trip above.
func TestEffectiveQuota_OverridePassesThroughToTheUsageView(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	owner := newTestUser(t, pool, "quota-usage-owner")

	repo := org.NewRepository(pool)
	o, err := repo.CreateWithOwner(ctx, "Quota Usage Org", owner.ID)
	if err != nil {
		t.Fatalf("CreateWithOwner: %v", err)
	}

	const configuredDefault = int64(5 * 1024 * 1024 * 1024)
	usage := org.UsageSources{
		Storage:  stubStorageStats{stats: org.StorageStats{UsedBytes: 1024, LiveFiles: 1, TrashedFiles: 0}},
		Shares:   stubShareLinkCounter{count: 0},
		Activity: stubActivityStats{},
	}
	svc := org.NewService(repo, nil, nil, usage, configuredDefault)

	if got, err := svc.EffectiveQuota(ctx, o.ID); err != nil || got != configuredDefault {
		t.Fatalf("EffectiveQuota with no override = (%d, %v), want (%d, nil)", got, err, configuredDefault)
	}

	override := int64(9999)
	if err := svc.SetQuota(ctx, o.ID, &override); err != nil {
		t.Fatalf("SetQuota: %v", err)
	}
	if got, err := svc.EffectiveQuota(ctx, o.ID); err != nil || got != override {
		t.Fatalf("EffectiveQuota after SetQuota = (%d, %v), want (%d, nil)", got, err, override)
	}

	u, err := svc.Usage(ctx, o.ID)
	if err != nil {
		t.Fatalf("Usage: %v", err)
	}
	if u.Storage.QuotaBytes != override {
		t.Fatalf("Usage().Storage.QuotaBytes = %d, want the override (%d), not the configured default", u.Storage.QuotaBytes, override)
	}
}

// The three tests below exercise org.Handler.SetQuota directly (real HTTP
// request/response, not just the service call above) — the platform-admin
// gate itself is applied by requirePlatformAdmin middleware in main.go, not
// by the handler, so what's left for the handler to get right is JSON
// parsing, the positive-or-null validation, and error-code mapping.

func newSetQuotaRequest(orgID, body string) *http.Request {
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(http.MethodPatch, "/v1/orgs/"+orgID+"/quota", nil)
	} else {
		r = httptest.NewRequest(http.MethodPatch, "/v1/orgs/"+orgID+"/quota", bytes.NewBufferString(body))
	}
	r.SetPathValue("orgId", orgID)
	return r
}

func TestHandlerSetQuota_NonPositiveValueReturns400(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	owner := newTestUser(t, pool, "quota-handler-owner-1")
	repo := org.NewRepository(pool)
	o, err := repo.CreateWithOwner(ctx, "Quota Handler Org 1", owner.ID)
	if err != nil {
		t.Fatalf("CreateWithOwner: %v", err)
	}
	h := org.NewHandler(org.NewService(repo, nil, nil, org.UsageSources{}, 1000))

	for _, body := range []string{`{"quota_bytes":0}`, `{"quota_bytes":-5}`} {
		w := httptest.NewRecorder()
		h.SetQuota(w, newSetQuotaRequest(o.ID, body))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("body %s: status = %d, want 400", body, w.Code)
		}
	}
}

func TestHandlerSetQuota_ValidSetThenNullClearBothSucceed(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	owner := newTestUser(t, pool, "quota-handler-owner-2")
	repo := org.NewRepository(pool)
	o, err := repo.CreateWithOwner(ctx, "Quota Handler Org 2", owner.ID)
	if err != nil {
		t.Fatalf("CreateWithOwner: %v", err)
	}
	h := org.NewHandler(org.NewService(repo, nil, nil, org.UsageSources{}, 1000))

	w := httptest.NewRecorder()
	h.SetQuota(w, newSetQuotaRequest(o.ID, `{"quota_bytes":500}`))
	if w.Code != http.StatusNoContent {
		t.Fatalf("set status = %d, want 204, body: %s", w.Code, w.Body.String())
	}
	if got, err := repo.QuotaOverride(ctx, o.ID); err != nil || got == nil || *got != 500 {
		t.Fatalf("QuotaOverride after handler set = (%v, %v), want (500, nil)", got, err)
	}

	w = httptest.NewRecorder()
	h.SetQuota(w, newSetQuotaRequest(o.ID, `{"quota_bytes":null}`))
	if w.Code != http.StatusNoContent {
		t.Fatalf("clear status = %d, want 204, body: %s", w.Code, w.Body.String())
	}
	if got, err := repo.QuotaOverride(ctx, o.ID); err != nil || got != nil {
		t.Fatalf("QuotaOverride after handler clear = (%v, %v), want (nil, nil)", got, err)
	}
}

func TestHandlerSetQuota_UnknownOrgReturns404(t *testing.T) {
	pool := newTestPool(t)
	repo := org.NewRepository(pool)
	h := org.NewHandler(org.NewService(repo, nil, nil, org.UsageSources{}, 1000))

	w := httptest.NewRecorder()
	h.SetQuota(w, newSetQuotaRequest("00000000-0000-0000-0000-000000000000", `{"quota_bytes":500}`))
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for a nonexistent org id", w.Code)
	}
}
