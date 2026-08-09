//go:build integration

// Audit §14: folder had zero automated tests — cycle-prevention, cross-org
// isolation, and cascade trash/restore are exactly the kind of "no smoke
// script runs on every push" correctness claims the audit called out.
// Mirrors scripts/smoke-folders.sh's assertions against real Postgres.
// Gated behind the "integration" build tag, matching the house style set by
// internal/auth/refresh_integration_test.go.
package folder_test

import (
	"bytes"
	"context"
	"encoding/json"
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

// newTestOrg returns a fresh org ID — folder rows FK to organizations, so
// every test needs one, but no test here cares about the owning user beyond
// that FK.
func newTestOrg(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	authRepo := auth.NewRepository(pool)
	user, err := authRepo.CreateUser(ctx, fmt.Sprintf("folder-%s@nimbus.test", suffix), "irrelevant-hash")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	orgRepo := org.NewRepository(pool)
	o, err := orgRepo.CreateWithOwner(ctx, "Folder Test Org "+suffix, user.ID)
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	return o.ID
}

func TestCreate_ChildUnderCrossOrgParentRejected(t *testing.T) {
	pool := newTestPool(t)
	orgA := newTestOrg(t, pool)
	orgB := newTestOrg(t, pool)
	svc := folder.NewService(folder.NewRepository(pool))
	ctx := context.Background()

	parentInA, err := svc.Create(ctx, orgA, nil, "Parent In A")
	if err != nil {
		t.Fatalf("create parent: %v", err)
	}

	_, err = svc.Create(ctx, orgB, &parentInA.ID, "Child In B")
	if !errors.Is(err, folder.ErrInvalidParent) {
		t.Fatalf("got err %v, want ErrInvalidParent for a parent belonging to a different org", err)
	}
}

func TestCreate_NonexistentParentRejected(t *testing.T) {
	pool := newTestPool(t)
	orgID := newTestOrg(t, pool)
	svc := folder.NewService(folder.NewRepository(pool))

	ghost := "00000000-0000-0000-0000-000000000000"
	_, err := svc.Create(context.Background(), orgID, &ghost, "Orphan")
	if !errors.Is(err, folder.ErrInvalidParent) {
		t.Fatalf("got err %v, want ErrInvalidParent for a nonexistent parent", err)
	}
}

func TestUpdate_MoveIntoSelfRejected(t *testing.T) {
	pool := newTestPool(t)
	orgID := newTestOrg(t, pool)
	svc := folder.NewService(folder.NewRepository(pool))
	ctx := context.Background()

	f, err := svc.Create(ctx, orgID, nil, "Self Mover")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	selfID := f.ID
	newParent := &selfID
	_, err = svc.Update(ctx, f, nil, &newParent)
	if !errors.Is(err, folder.ErrCyclicMove) {
		t.Fatalf("got err %v, want ErrCyclicMove for a folder moved into itself", err)
	}
}

func TestUpdate_MoveIntoOwnDescendantRejected(t *testing.T) {
	pool := newTestPool(t)
	orgID := newTestOrg(t, pool)
	repo := folder.NewRepository(pool)
	svc := folder.NewService(repo)
	ctx := context.Background()

	root, err := svc.Create(ctx, orgID, nil, "Root")
	if err != nil {
		t.Fatalf("create root: %v", err)
	}
	child, err := svc.Create(ctx, orgID, &root.ID, "Child")
	if err != nil {
		t.Fatalf("create child: %v", err)
	}
	grandchild, err := svc.Create(ctx, orgID, &child.ID, "Grandchild")
	if err != nil {
		t.Fatalf("create grandchild: %v", err)
	}

	// Moving Root under its own grandchild must be rejected — a cycle two
	// levels deep, not just the direct-parent case.
	gcID := grandchild.ID
	newParent := &gcID
	_, err = svc.Update(ctx, root, nil, &newParent)
	if !errors.Is(err, folder.ErrCyclicMove) {
		t.Fatalf("got err %v, want ErrCyclicMove for a move into a grandchild", err)
	}
}

func TestUpdate_MoveAcrossOrgsRejected(t *testing.T) {
	pool := newTestPool(t)
	orgA := newTestOrg(t, pool)
	orgB := newTestOrg(t, pool)
	svc := folder.NewService(folder.NewRepository(pool))
	ctx := context.Background()

	fInA, err := svc.Create(ctx, orgA, nil, "In A")
	if err != nil {
		t.Fatalf("create in A: %v", err)
	}
	targetInB, err := svc.Create(ctx, orgB, nil, "In B")
	if err != nil {
		t.Fatalf("create in B: %v", err)
	}

	targetID := targetInB.ID
	newParent := &targetID
	_, err = svc.Update(ctx, fInA, nil, &newParent)
	if !errors.Is(err, folder.ErrInvalidParent) {
		t.Fatalf("got err %v, want ErrInvalidParent for a cross-org move", err)
	}
}

func TestUpdate_ValidRenameAndMoveSucceed(t *testing.T) {
	pool := newTestPool(t)
	orgID := newTestOrg(t, pool)
	svc := folder.NewService(folder.NewRepository(pool))
	ctx := context.Background()

	a, err := svc.Create(ctx, orgID, nil, "A")
	if err != nil {
		t.Fatalf("create A: %v", err)
	}
	b, err := svc.Create(ctx, orgID, nil, "B")
	if err != nil {
		t.Fatalf("create B: %v", err)
	}

	newName := "A Renamed"
	renamed, err := svc.Update(ctx, a, &newName, nil)
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if renamed.Name != "A Renamed" {
		t.Fatalf("name = %s, want %q", renamed.Name, "A Renamed")
	}

	bID := b.ID
	newParent := &bID
	moved, err := svc.Update(ctx, renamed, nil, &newParent)
	if err != nil {
		t.Fatalf("move: %v", err)
	}
	if moved.ParentID == nil || *moved.ParentID != b.ID {
		t.Fatalf("ParentID after move = %v, want %s", moved.ParentID, b.ID)
	}
}

func TestAncestors_ReturnsRootToLeafInOrder(t *testing.T) {
	pool := newTestPool(t)
	orgID := newTestOrg(t, pool)
	svc := folder.NewService(folder.NewRepository(pool))
	ctx := context.Background()

	root, err := svc.Create(ctx, orgID, nil, "Root")
	if err != nil {
		t.Fatalf("create root: %v", err)
	}
	mid, err := svc.Create(ctx, orgID, &root.ID, "Mid")
	if err != nil {
		t.Fatalf("create mid: %v", err)
	}
	leaf, err := svc.Create(ctx, orgID, &mid.ID, "Leaf")
	if err != nil {
		t.Fatalf("create leaf: %v", err)
	}

	chain, err := svc.Ancestors(ctx, leaf.ID)
	if err != nil {
		t.Fatalf("Ancestors: %v", err)
	}
	if len(chain) != 3 {
		t.Fatalf("chain length = %d, want 3 (root, mid, leaf)", len(chain))
	}
	if chain[0].ID != root.ID || chain[1].ID != mid.ID || chain[2].ID != leaf.ID {
		t.Fatalf("chain order = %+v, want root -> mid -> leaf", chain)
	}
}

func TestSoftDeleteCascadeAndRestoreCascade_CoverTheWholeSubtree(t *testing.T) {
	pool := newTestPool(t)
	orgID := newTestOrg(t, pool)
	repo := folder.NewRepository(pool)
	svc := folder.NewService(repo)
	ctx := context.Background()

	parent, err := svc.Create(ctx, orgID, nil, "Parent")
	if err != nil {
		t.Fatalf("create parent: %v", err)
	}
	child, err := svc.Create(ctx, orgID, &parent.ID, "Child")
	if err != nil {
		t.Fatalf("create child: %v", err)
	}

	if err := svc.Delete(ctx, parent.ID); err != nil {
		t.Fatalf("Delete (cascade trash): %v", err)
	}
	if _, err := repo.Get(ctx, parent.ID); !errors.Is(err, folder.ErrNotFound) {
		t.Fatalf("parent should read as not-found while trashed, got err %v", err)
	}
	if _, err := repo.Get(ctx, child.ID); !errors.Is(err, folder.ErrNotFound) {
		t.Fatalf("child should read as not-found while trashed (cascade), got err %v", err)
	}

	trashed, err := repo.ListTrashed(ctx, orgID)
	if err != nil {
		t.Fatalf("ListTrashed: %v", err)
	}
	if len(trashed) != 2 {
		t.Fatalf("trashed count = %d, want 2 (parent + child)", len(trashed))
	}

	if err := svc.Restore(ctx, parent.ID); err != nil {
		t.Fatalf("Restore (cascade): %v", err)
	}
	if _, err := repo.Get(ctx, parent.ID); err != nil {
		t.Fatalf("parent should be live again after restore, got err %v", err)
	}
	if _, err := repo.Get(ctx, child.ID); err != nil {
		t.Fatalf("child should be live again after cascade restore, got err %v", err)
	}
}

func TestPurgeExpiredTrash_OnlyPurgesPastRetentionAndGuardsLiveContent(t *testing.T) {
	pool := newTestPool(t)
	orgID := newTestOrg(t, pool)
	repo := folder.NewRepository(pool)
	svc := folder.NewService(repo)
	ctx := context.Background()

	// Case 1: trashed but within the retention window — must survive.
	recent, err := svc.Create(ctx, orgID, nil, "Recently Trashed")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := svc.Delete(ctx, recent.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Case 2: trashed and backdated past retention — must be purged.
	expired, err := svc.Create(ctx, orgID, nil, "Expired")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := svc.Delete(ctx, expired.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE folders SET deleted_at = now() - interval '31 days' WHERE id = $1`, expired.ID); err != nil {
		t.Fatalf("backdate: %v", err)
	}

	purged, err := repo.PurgeExpiredTrash(ctx, 30*24*time.Hour)
	if err != nil {
		t.Fatalf("PurgeExpiredTrash: %v", err)
	}
	if purged != 1 {
		t.Fatalf("purged = %d, want exactly 1 (only the backdated folder)", purged)
	}

	if _, err := repo.GetAny(ctx, expired.ID); !errors.Is(err, folder.ErrNotFound) {
		t.Fatalf("expired folder should be hard-deleted, got err %v", err)
	}
	if _, err := repo.GetAny(ctx, recent.ID); err != nil {
		t.Fatalf("recently trashed folder should survive (within retention), got err %v", err)
	}
}

// TestPurgeExpiredTrash_CascadeDecrementsOrgUsageBytes covers the write path
// audit §06's usage_bytes counter (backend/internal/file/repository.go's
// OrgUsageBytes) most needed direct proof for: deleting a folders row
// CASCADEs straight through files/file_versions without ever calling
// file.Repository.Purge or PurgeExpiredTrash, so this module has its own,
// independent obligation to credit the freed bytes back — nothing else
// would catch a regression here.
func TestPurgeExpiredTrash_CascadeDecrementsOrgUsageBytes(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	authRepo := auth.NewRepository(pool)
	user, err := authRepo.CreateUser(ctx, fmt.Sprintf("folder-usage-%s@nimbus.test", suffix), "irrelevant-hash")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	orgRepo := org.NewRepository(pool)
	o, err := orgRepo.CreateWithOwner(ctx, "Folder Usage Org "+suffix, user.ID)
	if err != nil {
		t.Fatalf("create org: %v", err)
	}

	folderRepo := folder.NewRepository(pool)
	folderSvc := folder.NewService(folderRepo)
	fileRepo := file.NewRepository(pool)

	f, err := folderSvc.Create(ctx, o.ID, nil, "Folder With A File")
	if err != nil {
		t.Fatalf("create folder: %v", err)
	}
	if _, _, err := fileRepo.CreateWithVersion(ctx, o.ID, f.ID, "inside.bin", user.ID, 750, "checksum", "text/plain", nil); err != nil {
		t.Fatalf("create file: %v", err)
	}
	if used, err := fileRepo.OrgUsageBytes(ctx, o.ID); err != nil || used != 750 {
		t.Fatalf("OrgUsageBytes before delete = (%d, %v), want (750, nil)", used, err)
	}

	if err := folderSvc.Delete(ctx, f.ID); err != nil {
		t.Fatalf("delete folder: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE folders SET deleted_at = now() - interval '31 days' WHERE id = $1`, f.ID); err != nil {
		t.Fatalf("backdate: %v", err)
	}

	purged, err := folderRepo.PurgeExpiredTrash(ctx, 30*24*time.Hour)
	if err != nil {
		t.Fatalf("PurgeExpiredTrash: %v", err)
	}
	if purged != 1 {
		t.Fatalf("purged = %d, want exactly 1", purged)
	}

	used, err := fileRepo.OrgUsageBytes(ctx, o.ID)
	if err != nil {
		t.Fatalf("OrgUsageBytes after cascade purge: %v", err)
	}
	if used != 0 {
		t.Fatalf("OrgUsageBytes after cascade purge = %d, want 0 — the file inside the purged folder is gone, and its bytes must come off the counter even though file.Repository.Purge was never called for it", used)
	}

	_ = f // silence unused if reordered later
}

// Audit §14 named folder as one of five backend modules still at 0%
// handler-layer coverage even after this file's service/repository tests
// landed. The tests below exercise folder.Handler directly (real HTTP
// request/response via httptest, WithFolder standing in for
// RequireAccess's context injection) — JSON parsing, the
// omitted-vs-explicit-null distinction Update's own comment calls out, and
// the error-code switch in writeFolderError, none of which the tests above
// touch.

type stubFileLister struct{ files []folder.FileSummary }

func (s stubFileLister) ListInFolder(ctx context.Context, folderID string) ([]folder.FileSummary, error) {
	return s.files, nil
}

type stubMembershipChecker struct{ isMember bool }

func (s stubMembershipChecker) IsMember(ctx context.Context, orgID, userID string) (bool, error) {
	return s.isMember, nil
}

func TestHandlerCreate_MissingNameReturns400(t *testing.T) {
	pool := newTestPool(t)
	orgID := newTestOrg(t, pool)
	h := folder.NewHandler(folder.NewService(folder.NewRepository(pool)), stubFileLister{}, stubMembershipChecker{isMember: true})

	req := httptest.NewRequest(http.MethodPost, "/v1/orgs/"+orgID+"/folders", bytes.NewBufferString(`{}`))
	req.SetPathValue("orgId", orgID)
	w := httptest.NewRecorder()
	h.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for a missing name", w.Code)
	}
}

func TestHandlerCreate_ValidReturns201WithRealID(t *testing.T) {
	pool := newTestPool(t)
	orgID := newTestOrg(t, pool)
	h := folder.NewHandler(folder.NewService(folder.NewRepository(pool)), stubFileLister{}, stubMembershipChecker{isMember: true})

	req := httptest.NewRequest(http.MethodPost, "/v1/orgs/"+orgID+"/folders", bytes.NewBufferString(`{"name":"New Folder"}`))
	req.SetPathValue("orgId", orgID)
	w := httptest.NewRecorder()
	h.Create(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201, body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp["id"] == "" || resp["id"] == nil {
		t.Fatalf("response missing id: %+v", resp)
	}
}

func TestHandlerListChildren_ReturnsFoldersAndFilesFromContext(t *testing.T) {
	pool := newTestPool(t)
	orgID := newTestOrg(t, pool)
	folderRepo := folder.NewRepository(pool)
	folderSvc := folder.NewService(folderRepo)
	ctx := context.Background()

	parent, err := folderSvc.Create(ctx, orgID, nil, "Parent")
	if err != nil {
		t.Fatalf("create parent: %v", err)
	}
	if _, err := folderSvc.Create(ctx, orgID, &parent.ID, "Child"); err != nil {
		t.Fatalf("create child: %v", err)
	}

	files := stubFileLister{files: []folder.FileSummary{{ID: "file-1", Name: "doc.pdf"}}}
	h := folder.NewHandler(folderSvc, files, stubMembershipChecker{isMember: true})

	req := httptest.NewRequest(http.MethodGet, "/v1/folders/"+parent.ID+"/children", nil)
	req = req.WithContext(folder.WithFolder(req.Context(), parent))
	w := httptest.NewRecorder()
	h.ListChildren(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Folders []map[string]any `json:"folders"`
		Files   []map[string]any `json:"files"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(resp.Folders) != 1 || resp.Folders[0]["name"] != "Child" {
		t.Fatalf("folders = %+v, want exactly the one child folder", resp.Folders)
	}
	if len(resp.Files) != 1 || resp.Files[0]["name"] != "doc.pdf" {
		t.Fatalf("files = %+v, want the one stubbed file", resp.Files)
	}
}

func TestHandlerPath_ReturnsRootToLeafAncestorChain(t *testing.T) {
	pool := newTestPool(t)
	orgID := newTestOrg(t, pool)
	folderSvc := folder.NewService(folder.NewRepository(pool))
	ctx := context.Background()

	root, err := folderSvc.Create(ctx, orgID, nil, "Root")
	if err != nil {
		t.Fatalf("create root: %v", err)
	}
	leaf, err := folderSvc.Create(ctx, orgID, &root.ID, "Leaf")
	if err != nil {
		t.Fatalf("create leaf: %v", err)
	}

	h := folder.NewHandler(folderSvc, stubFileLister{}, stubMembershipChecker{isMember: true})
	req := httptest.NewRequest(http.MethodGet, "/v1/folders/"+leaf.ID+"/path", nil)
	req = req.WithContext(folder.WithFolder(req.Context(), leaf))
	w := httptest.NewRecorder()
	h.Path(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", w.Code, w.Body.String())
	}
	var chain []map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &chain); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(chain) != 2 || chain[0]["name"] != "Root" || chain[1]["name"] != "Leaf" {
		t.Fatalf("path chain = %+v, want [Root, Leaf] in that order", chain)
	}
}

func TestHandlerUpdate_RenameSucceedsAndMoveIntoOwnDescendantReturns400(t *testing.T) {
	pool := newTestPool(t)
	orgID := newTestOrg(t, pool)
	folderSvc := folder.NewService(folder.NewRepository(pool))
	ctx := context.Background()

	parent, err := folderSvc.Create(ctx, orgID, nil, "Update Parent")
	if err != nil {
		t.Fatalf("create parent: %v", err)
	}
	child, err := folderSvc.Create(ctx, orgID, &parent.ID, "Update Child")
	if err != nil {
		t.Fatalf("create child: %v", err)
	}

	h := folder.NewHandler(folderSvc, stubFileLister{}, stubMembershipChecker{isMember: true})

	renameReq := httptest.NewRequest(http.MethodPatch, "/v1/folders/"+parent.ID, bytes.NewBufferString(`{"name":"Renamed Parent"}`))
	renameReq = renameReq.WithContext(folder.WithFolder(renameReq.Context(), parent))
	renameW := httptest.NewRecorder()
	h.Update(renameW, renameReq)
	if renameW.Code != http.StatusOK {
		t.Fatalf("rename status = %d, want 200, body: %s", renameW.Code, renameW.Body.String())
	}
	var renamed map[string]any
	if err := json.Unmarshal(renameW.Body.Bytes(), &renamed); err != nil {
		t.Fatalf("unmarshal rename response: %v", err)
	}
	if renamed["name"] != "Renamed Parent" {
		t.Fatalf("renamed name = %v, want %q", renamed["name"], "Renamed Parent")
	}

	moveReq := httptest.NewRequest(http.MethodPatch, "/v1/folders/"+parent.ID, bytes.NewBufferString(`{"parent_id":"`+child.ID+`"}`))
	moveReq = moveReq.WithContext(folder.WithFolder(moveReq.Context(), parent))
	moveW := httptest.NewRecorder()
	h.Update(moveW, moveReq)
	if moveW.Code != http.StatusBadRequest {
		t.Fatalf("move-into-own-child status = %d, want 400 (ErrCyclicMove)", moveW.Code)
	}
}

func TestHandlerDelete_TrashesFolder(t *testing.T) {
	pool := newTestPool(t)
	orgID := newTestOrg(t, pool)
	folderRepo := folder.NewRepository(pool)
	folderSvc := folder.NewService(folderRepo)
	ctx := context.Background()

	f, err := folderSvc.Create(ctx, orgID, nil, "To Delete")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	h := folder.NewHandler(folderSvc, stubFileLister{}, stubMembershipChecker{isMember: true})
	req := httptest.NewRequest(http.MethodDelete, "/v1/folders/"+f.ID, nil)
	req = req.WithContext(folder.WithFolder(req.Context(), f))
	w := httptest.NewRecorder()
	h.Delete(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204, body: %s", w.Code, w.Body.String())
	}
	got, err := folderRepo.GetAny(ctx, f.ID)
	if err != nil {
		t.Fatalf("GetAny after delete: %v", err)
	}
	if got.DeletedAt == nil {
		t.Fatal("expected DeletedAt to be set after Handler.Delete")
	}
}

func TestHandlerRestore_NonMemberReturns403AndMemberSucceeds(t *testing.T) {
	pool := newTestPool(t)
	orgID := newTestOrg(t, pool)
	folderRepo := folder.NewRepository(pool)
	folderSvc := folder.NewService(folderRepo)
	ctx := context.Background()

	f, err := folderSvc.Create(ctx, orgID, nil, "To Restore")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := folderSvc.Delete(ctx, f.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	deniedHandler := folder.NewHandler(folderSvc, stubFileLister{}, stubMembershipChecker{isMember: false})
	deniedReq := httptest.NewRequest(http.MethodPost, "/v1/folders/"+f.ID+"/restore", nil)
	deniedReq.SetPathValue("folderId", f.ID)
	deniedW := httptest.NewRecorder()
	deniedHandler.Restore(deniedW, deniedReq)
	if deniedW.Code != http.StatusForbidden {
		t.Fatalf("non-member restore status = %d, want 403", deniedW.Code)
	}

	allowedHandler := folder.NewHandler(folderSvc, stubFileLister{}, stubMembershipChecker{isMember: true})
	allowedReq := httptest.NewRequest(http.MethodPost, "/v1/folders/"+f.ID+"/restore", nil)
	allowedReq.SetPathValue("folderId", f.ID)
	allowedW := httptest.NewRecorder()
	allowedHandler.Restore(allowedW, allowedReq)
	if allowedW.Code != http.StatusOK {
		t.Fatalf("member restore status = %d, want 200, body: %s", allowedW.Code, allowedW.Body.String())
	}
	got, err := folderRepo.GetAny(ctx, f.ID)
	if err != nil {
		t.Fatalf("GetAny after restore: %v", err)
	}
	if got.DeletedAt != nil {
		t.Fatal("expected DeletedAt to be cleared after Handler.Restore")
	}
}
