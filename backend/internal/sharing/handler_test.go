package sharing

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Audit §14 / scripts/smoke-sharing.js's own named assertions: missing,
// malformed, past, and >7-day expires_at must all be rejected with 400.
// parseCreateShare is the one place that logic lives, and it's a plain
// function of (http.ResponseWriter, *http.Request) with no DB dependency —
// genuinely unit-testable, unlike the rest of the handler.

func newCreateShareRequest(t *testing.T, body string) *http.Request {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(http.MethodPost, "/v1/files/f1/share", nil)
	} else {
		r = httptest.NewRequest(http.MethodPost, "/v1/files/f1/share", bytes.NewBufferString(body))
	}
	return r
}

func TestParseCreateShare_MissingBodyRejected(t *testing.T) {
	w := httptest.NewRecorder()
	_, _, ok := parseCreateShare(w, newCreateShareRequest(t, ""))
	if ok {
		t.Fatal("expected rejection for a request with no body at all")
	}
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestParseCreateShare_MissingExpiresAtRejected(t *testing.T) {
	w := httptest.NewRecorder()
	_, _, ok := parseCreateShare(w, newCreateShareRequest(t, `{}`))
	if ok {
		t.Fatal("expected rejection when expires_at is absent")
	}
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestParseCreateShare_MalformedJSONRejected(t *testing.T) {
	w := httptest.NewRecorder()
	_, _, ok := parseCreateShare(w, newCreateShareRequest(t, `{not json`))
	if ok {
		t.Fatal("expected rejection for malformed JSON")
	}
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestParseCreateShare_NonRFC3339ExpiresAtRejected(t *testing.T) {
	w := httptest.NewRecorder()
	_, _, ok := parseCreateShare(w, newCreateShareRequest(t, `{"expires_at":"tomorrow"}`))
	if ok {
		t.Fatal("expected rejection for a non-RFC3339 expires_at")
	}
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestParseCreateShare_PastExpiresAtRejected(t *testing.T) {
	past := time.Now().Add(-time.Hour).Format(time.RFC3339)
	w := httptest.NewRecorder()
	_, _, ok := parseCreateShare(w, newCreateShareRequest(t, `{"expires_at":"`+past+`"}`))
	if ok {
		t.Fatal("expected rejection for an expires_at already in the past")
	}
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestParseCreateShare_BeyondSevenDaysRejected(t *testing.T) {
	tooFar := time.Now().Add(maxShareTTL + time.Hour).Format(time.RFC3339)
	w := httptest.NewRecorder()
	_, _, ok := parseCreateShare(w, newCreateShareRequest(t, `{"expires_at":"`+tooFar+`"}`))
	if ok {
		t.Fatal("expected rejection for an expires_at more than 7 days out")
	}
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestParseCreateShare_ValidWithinWindowAccepted(t *testing.T) {
	cases := []time.Duration{time.Hour, 24 * time.Hour, maxShareTTL - time.Minute}
	for _, d := range cases {
		expiry := time.Now().Add(d).Format(time.RFC3339)
		w := httptest.NewRecorder()
		_, got, ok := parseCreateShare(w, newCreateShareRequest(t, `{"expires_at":"`+expiry+`"}`))
		if !ok {
			t.Fatalf("duration %s: expected acceptance, got status %d", d, w.Code)
		}
		if got == nil {
			t.Fatalf("duration %s: expected a non-nil parsed time", d)
		}
	}
}

func TestParseCreateShare_BundleFileIDsPassThrough(t *testing.T) {
	w := httptest.NewRecorder()
	expiry := time.Now().Add(time.Hour).Format(time.RFC3339)
	req, _, ok := parseCreateShare(w, newCreateShareRequest(t, `{"expires_at":"`+expiry+`","file_ids":["a","b","a"]}`))
	if !ok {
		t.Fatalf("expected acceptance, got status %d", w.Code)
	}
	if len(req.FileIDs) != 3 {
		t.Fatalf("FileIDs = %v, want the raw 3-element slice (dedup happens in CreateBundleShare, not parsing)", req.FileIDs)
	}
}
