package upload

import (
	"context"
	"errors"
	"testing"
)

// Audit §14 flagged quota enforcement (checkQuota/ErrQuotaExceeded) as
// having no dedicated coverage anywhere — not even a smoke script. checkQuota
// is unexported but depends only on the UsageReader port, so it's fully
// unit-testable white-box without a real Postgres — the InitUpload/
// CompleteUpload wiring around it is covered end-to-end in
// quota_integration_test.go.

type stubUsageReader struct {
	usedBytes int64
	err       error
}

func (s stubUsageReader) OrgUsageBytes(ctx context.Context, orgID string) (int64, error) {
	return s.usedBytes, s.err
}

func TestCheckQuota_DisabledWhenQuotaIsZeroOrNegative(t *testing.T) {
	for _, quota := range []int64{0, -1} {
		s := &Service{orgQuotaBytes: quota, usage: stubUsageReader{usedBytes: 999999}}
		if err := s.checkQuota(context.Background(), "org-1", 1_000_000_000); err != nil {
			t.Fatalf("quota=%d: got err %v, want nil (quota disabled)", quota, err)
		}
	}
}

func TestCheckQuota_UnderQuotaAllowed(t *testing.T) {
	s := &Service{orgQuotaBytes: 1000, usage: stubUsageReader{usedBytes: 400}}
	if err := s.checkQuota(context.Background(), "org-1", 500); err != nil {
		t.Fatalf("400 used + 500 new against a 1000 quota: got err %v, want nil", err)
	}
}

func TestCheckQuota_ExactlyAtQuotaAllowed(t *testing.T) {
	s := &Service{orgQuotaBytes: 1000, usage: stubUsageReader{usedBytes: 400}}
	if err := s.checkQuota(context.Background(), "org-1", 600); err != nil {
		t.Fatalf("400 used + 600 new landing exactly on a 1000 quota: got err %v, want nil (boundary is inclusive)", err)
	}
}

func TestCheckQuota_OverQuotaRejected(t *testing.T) {
	s := &Service{orgQuotaBytes: 1000, usage: stubUsageReader{usedBytes: 400}}
	err := s.checkQuota(context.Background(), "org-1", 601)
	if !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("400 used + 601 new against a 1000 quota: got err %v, want ErrQuotaExceeded", err)
	}
}

func TestCheckQuota_UsageLookupErrorPropagates(t *testing.T) {
	wantErr := errors.New("boom")
	s := &Service{orgQuotaBytes: 1000, usage: stubUsageReader{err: wantErr}}
	if err := s.checkQuota(context.Background(), "org-1", 10); !errors.Is(err, wantErr) {
		t.Fatalf("got err %v, want the underlying usage-lookup error to propagate", err)
	}
}

// InitUpload's own size-cap check (maxUploadBytes) runs before quota and
// before any DB access at all — genuinely unit-testable in full.

type stubFolderOrgLookup struct{ orgID string }

func (s stubFolderOrgLookup) OrgIDOf(ctx context.Context, folderID string) (string, error) {
	return s.orgID, nil
}

func TestInitUpload_OversizedFileRejectedBeforeAnyDBAccess(t *testing.T) {
	s := &Service{maxUploadBytes: 1000, folders: stubFolderOrgLookup{orgID: "org-1"}}
	_, err := s.InitUpload(context.Background(), "user-1", "folder-1", "", "big.bin", 1001, "application/octet-stream")
	if !errors.Is(err, ErrFileTooLarge) {
		t.Fatalf("got err %v, want ErrFileTooLarge", err)
	}
}
