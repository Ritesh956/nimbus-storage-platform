//go:build integration

// Audit §14: search had zero automated tests. Mirrors
// scripts/smoke-search-activity.js's search assertions (name/type/date
// filtering, org scoping) against real Postgres, plus pagination — not
// covered by any smoke script. Gated behind the "integration" build tag,
// matching the house style set by internal/auth/refresh_integration_test.go.
package search_test

import (
	"context"
	"encoding/json"
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
	"nimbus/internal/search"
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

type fixture struct {
	ownerID  string
	orgID    string
	folderID string
	fileRepo *file.Repository
	svc      *search.Service
}

func newFixture(t *testing.T, pool *pgxpool.Pool) fixture {
	t.Helper()
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	authRepo := auth.NewRepository(pool)
	user, err := authRepo.CreateUser(ctx, fmt.Sprintf("search-%s@nimbus.test", suffix), "irrelevant-hash")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	orgRepo := org.NewRepository(pool)
	o, err := orgRepo.CreateWithOwner(ctx, "Search Test Org "+suffix, user.ID)
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	folderRepo := folder.NewRepository(pool)
	f, err := folderRepo.Create(ctx, o.ID, nil, "Search Test Folder")
	if err != nil {
		t.Fatalf("create folder: %v", err)
	}
	fileRepo := file.NewRepository(pool)
	svc := search.NewService(search.NewRepository(pool))
	return fixture{ownerID: user.ID, orgID: o.ID, folderID: f.ID, fileRepo: fileRepo, svc: svc}
}

func (fx fixture) upload(t *testing.T, name, mimeType string, sizeBytes int64) string {
	t.Helper()
	id, _, err := fx.fileRepo.CreateWithVersion(context.Background(), fx.orgID, fx.folderID, name, fx.ownerID, sizeBytes, "checksum-"+name, mimeType, nil)
	if err != nil {
		t.Fatalf("upload %s: %v", name, err)
	}
	return id
}

func TestSearch_NameMatchIsScopedToOrg(t *testing.T) {
	pool := newTestPool(t)
	fxA := newFixture(t, pool)
	fxB := newFixture(t, pool)

	fxA.upload(t, "quarterly-report.pdf", "application/pdf", 1000)
	fxB.upload(t, "quarterly-report.pdf", "application/pdf", 1000) // same name, different org

	results, _, err := fxA.svc.Search(context.Background(), fxA.orgID, search.Filters{Query: "quarterly", Limit: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %d, want exactly 1 (org B's identically-named file must not leak into org A's search)", len(results))
	}
}

func TestSearch_TypeFilterIsPrefixMatchOnMimeType(t *testing.T) {
	pool := newTestPool(t)
	fx := newFixture(t, pool)
	fx.upload(t, "photo.png", "image/png", 500)
	fx.upload(t, "notes.txt", "text/plain", 200)

	results, _, err := fx.svc.Search(context.Background(), fx.orgID, search.Filters{Type: "image", Limit: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 || results[0].Name != "photo.png" {
		t.Fatalf("results = %+v, want exactly photo.png", results)
	}
}

func TestSearch_SizeRangeFilter(t *testing.T) {
	pool := newTestPool(t)
	fx := newFixture(t, pool)
	fx.upload(t, "small.bin", "application/octet-stream", 100)
	fx.upload(t, "medium.bin", "application/octet-stream", 5000)
	fx.upload(t, "large.bin", "application/octet-stream", 50000)

	min, max := int64(1000), int64(10000)
	results, _, err := fx.svc.Search(context.Background(), fx.orgID, search.Filters{SizeMin: &min, SizeMax: &max, Limit: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 || results[0].Name != "medium.bin" {
		t.Fatalf("results = %+v, want exactly medium.bin", results)
	}
}

func TestSearch_OwnerFilter(t *testing.T) {
	pool := newTestPool(t)
	fx := newFixture(t, pool)
	fx.upload(t, "mine.bin", "application/octet-stream", 10)

	results, _, err := fx.svc.Search(context.Background(), fx.orgID, search.Filters{OwnerID: fx.ownerID, Limit: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1 for the real owner", len(results))
	}

	otherID := "00000000-0000-0000-0000-000000000000"
	results, _, err = fx.svc.Search(context.Background(), fx.orgID, search.Filters{OwnerID: otherID, Limit: 10})
	if err != nil {
		t.Fatalf("Search with unrelated owner: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("results = %d, want 0 for an owner with nothing uploaded", len(results))
	}
}

func TestSearch_PaginationCursorCoversAllResultsExactlyOnce(t *testing.T) {
	pool := newTestPool(t)
	fx := newFixture(t, pool)
	const total = 5
	uploaded := make(map[string]bool, total)
	for i := 0; i < total; i++ {
		id := fx.upload(t, fmt.Sprintf("page-item-%d.bin", i), "application/octet-stream", 10)
		uploaded[id] = true
	}

	seen := make(map[string]bool, total)
	cursor := ""
	for pages := 0; pages < total+1; pages++ { // +1 headroom so a pagination bug fails loudly instead of looping forever
		results, next, err := fx.svc.Search(context.Background(), fx.orgID, search.Filters{Cursor: cursor, Limit: 2})
		if err != nil {
			t.Fatalf("Search page: %v", err)
		}
		for _, r := range results {
			if seen[r.FileID] {
				t.Fatalf("file %s returned on more than one page", r.FileID)
			}
			seen[r.FileID] = true
		}
		if next == "" {
			break
		}
		cursor = next
	}

	if len(seen) != total {
		t.Fatalf("saw %d distinct results across all pages, want %d", len(seen), total)
	}
	for id := range uploaded {
		if !seen[id] {
			t.Fatalf("file %s never appeared in any page", id)
		}
	}
}

func TestSearch_DateRangeFilter(t *testing.T) {
	pool := newTestPool(t)
	fx := newFixture(t, pool)
	fx.upload(t, "todays-file.bin", "application/octet-stream", 10)

	future := time.Now().Add(24 * time.Hour)
	results, _, err := fx.svc.Search(context.Background(), fx.orgID, search.Filters{DateFrom: &future, Limit: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("results = %d, want 0 for a date_from in the future", len(results))
	}

	past := time.Now().Add(-24 * time.Hour)
	results, _, err = fx.svc.Search(context.Background(), fx.orgID, search.Filters{DateFrom: &past, Limit: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1 for a date_from in the past", len(results))
	}
}

// Audit §14 named search as one of five backend modules still at 0%
// handler-layer coverage — search.Handler.Search has no HTTP-level test at
// all, only the service-layer Filters tests above. The tests below exercise
// it directly (real HTTP request/response via httptest): query-string
// parsing into Filters, the RFC3339/int64 validation switch, and the actual
// response body shape a real client receives.

func TestHandlerSearch_InvalidDateFromReturns400(t *testing.T) {
	pool := newTestPool(t)
	fx := newFixture(t, pool)
	h := search.NewHandler(fx.svc)

	req := httptest.NewRequest(http.MethodGet, "/v1/orgs/"+fx.orgID+"/search?date_from=not-a-date", nil)
	req.SetPathValue("orgId", fx.orgID)
	w := httptest.NewRecorder()
	h.Search(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for a non-RFC3339 date_from", w.Code)
	}
}

func TestHandlerSearch_InvalidSizeMinReturns400(t *testing.T) {
	pool := newTestPool(t)
	fx := newFixture(t, pool)
	h := search.NewHandler(fx.svc)

	req := httptest.NewRequest(http.MethodGet, "/v1/orgs/"+fx.orgID+"/search?size_min=not-a-number", nil)
	req.SetPathValue("orgId", fx.orgID)
	w := httptest.NewRecorder()
	h.Search(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for a non-integer size_min", w.Code)
	}
}

func TestHandlerSearch_ValidQueryReturnsRealJSONResponseBody(t *testing.T) {
	pool := newTestPool(t)
	fx := newFixture(t, pool)
	fx.upload(t, "handler-search-report.pdf", "application/pdf", 2048)
	h := search.NewHandler(fx.svc)

	req := httptest.NewRequest(http.MethodGet, "/v1/orgs/"+fx.orgID+"/search?q=handler-search&limit=10", nil)
	req.SetPathValue("orgId", fx.orgID)
	w := httptest.NewRecorder()
	h.Search(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Results []map[string]any `json:"results"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(resp.Results) != 1 || resp.Results[0]["name"] != "handler-search-report.pdf" {
		t.Fatalf("results = %+v, want exactly the one uploaded file", resp.Results)
	}
}
