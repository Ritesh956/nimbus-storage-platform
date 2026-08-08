//go:build integration

// Audit §14: DLQHandler (the HTTP surface of the admin DLQ endpoints,
// roadmap #9) had zero automated tests — including the retry/revert
// interlock (Retry.go's comment: "if it then fails again... dead-letters it
// as a fresh row"), which no smoke script exercises. Gated behind the
// "integration" build tag, matching the house style set by
// internal/auth/refresh_integration_test.go.
package events_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"nimbus/internal/events"
)

func TestDLQHandlerRetry_UnknownIDReturns404(t *testing.T) {
	pool := newTestPool(t)
	js := newTestJetStream(t)
	repo := events.NewRepository(pool)
	h := events.NewDLQHandler(repo, events.NewPublisher(js))

	req := httptest.NewRequest(http.MethodPost, "/v1/admin/dlq/ghost/retry", nil)
	req.SetPathValue("id", "00000000-0000-0000-0000-000000000000")
	w := httptest.NewRecorder()
	h.Retry(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestDLQHandlerRetry_RepublishesAndMarksRetried(t *testing.T) {
	pool := newTestPool(t)
	js := newTestJetStream(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := events.EnsureStream(ctx, js); err != nil {
		t.Fatalf("EnsureStream: %v", err)
	}

	repo := events.NewRepository(pool)
	pub := events.NewPublisher(js)
	h := events.NewDLQHandler(repo, pub)

	// A unique error marker per run — this Postgres container persists data
	// across repeated `go test` invocations (no per-test cleanup truncates
	// dead_events), so a fixed literal risks matching a stale already-
	// retried row from an earlier run instead of the one this test just
	// inserted (caught by an actual failing test run across repeated
	// invocations, not by a single run or vet/build).
	errMarker := fmt.Sprintf("original failure %d", time.Now().UnixNano())
	payload := []byte(`{"event_id":"dlq-retry-test","file_id":"f","version_id":"v","org_id":"o","request_id":"r"}`)
	if err := repo.InsertDeadEvent(ctx, events.UploadCompletedSubject, payload, errMarker, 5); err != nil {
		t.Fatalf("InsertDeadEvent: %v", err)
	}
	all, err := repo.ListDeadEvents(ctx)
	if err != nil || len(all) == 0 {
		t.Fatalf("ListDeadEvents: %v", err)
	}
	var id string
	for _, e := range all {
		if e.Error == errMarker {
			id = e.ID
			break // ListDeadEvents orders newest-first; the first match is this run's own row
		}
	}
	if id == "" {
		t.Fatal("could not find the seeded dead event")
	}

	// A subscriber that proves the republish actually lands on the wire —
	// Retry's own success response only proves PublishRaw returned nil, not
	// that JetStream actually has the message.
	received := make(chan struct{}, 1)
	dead := events.NewRepository(nil)
	if _, err := events.Subscribe(ctx, js, dead, func(ctx context.Context, evt events.UploadCompleted) error {
		if evt.EventID == "dlq-retry-test" {
			received <- struct{}{}
		}
		return nil
	}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/admin/dlq/"+id+"/retry", nil)
	req.SetPathValue("id", id)
	w := httptest.NewRecorder()
	h.Retry(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", w.Code, w.Body.String())
	}

	got, err := repo.GetDeadEvent(ctx, id)
	if err != nil {
		t.Fatalf("GetDeadEvent: %v", err)
	}
	if got.Status != "retried" {
		t.Fatalf("status = %s, want retried", got.Status)
	}

	select {
	case <-received:
	case <-ctx.Done():
		t.Fatal("timed out waiting for the republished payload to actually arrive on the subject")
	}

	// A second retry attempt on the same (now-retried) row must be
	// rejected — this is the double-republish guard.
	req2 := httptest.NewRequest(http.MethodPost, "/v1/admin/dlq/"+id+"/retry", nil)
	req2.SetPathValue("id", id)
	w2 := httptest.NewRecorder()
	h.Retry(w2, req2)
	if w2.Code != http.StatusConflict {
		t.Fatalf("second retry status = %d, want 409", w2.Code)
	}
}

func TestDLQHandlerList_ReturnsInsertedEvents(t *testing.T) {
	pool := newTestPool(t)
	js := newTestJetStream(t)
	repo := events.NewRepository(pool)
	h := events.NewDLQHandler(repo, events.NewPublisher(js))
	ctx := context.Background()

	if err := repo.InsertDeadEvent(ctx, events.UploadCompletedSubject, []byte(`{"event_id":"list-test"}`), "list test error", 3); err != nil {
		t.Fatalf("InsertDeadEvent: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/dlq", nil)
	w := httptest.NewRecorder()
	h.List(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), "list test error") {
		t.Fatalf("response body did not contain the seeded event's error message: %s", w.Body.String())
	}
}
