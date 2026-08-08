package search

import (
	"encoding/base64"
	"testing"
	"time"
)

// Audit §14: search had zero automated tests. encodeCursor/decodeCursor are
// pure functions with no DB dependency, so they're unit-tested here —
// Repository.Search itself is a thin Postgres query, integration-tested in
// repository_integration_test.go.

func TestCursorRoundTrips(t *testing.T) {
	want := time.Date(2026, 3, 5, 12, 30, 0, 123456000, time.UTC)
	id := "550e8400-e29b-41d4-a716-446655440000"

	encoded := encodeCursor(want, id)
	gotTime, gotID, err := decodeCursor(encoded)
	if err != nil {
		t.Fatalf("decodeCursor: %v", err)
	}
	if !gotTime.Equal(want) {
		t.Fatalf("decoded time = %v, want %v", gotTime, want)
	}
	if gotID != id {
		t.Fatalf("decoded id = %s, want %s", gotID, id)
	}
}

func TestDecodeCursor_InvalidBase64Errors(t *testing.T) {
	if _, _, err := decodeCursor("not-valid-base64!!!"); err == nil {
		t.Fatal("expected an error for invalid base64")
	}
}

func TestDecodeCursor_MissingSeparatorErrors(t *testing.T) {
	// Valid base64, but no "|" separator inside — decodeCursor must reject
	// it rather than silently returning a zero-value ID.
	encoded := base64.RawURLEncoding.EncodeToString([]byte("no-separator-here"))
	if _, _, err := decodeCursor(encoded); err == nil {
		t.Fatal("expected an error for a cursor with no time|id separator")
	}
}

func TestDecodeCursor_MalformedTimeErrors(t *testing.T) {
	encoded := base64.RawURLEncoding.EncodeToString([]byte("not-a-timestamp|some-id"))
	if _, _, err := decodeCursor(encoded); err == nil {
		t.Fatal("expected an error for a malformed timestamp component")
	}
}
