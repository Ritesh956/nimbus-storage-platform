package sharing

import (
	"context"
	"errors"
	"testing"
)

// Audit §14: sharing had zero automated tests despite being the module the
// audit itself named as the highest-value smoke-script port target
// (scripts/smoke-sharing.js). CreateBundleShare's dedup/validation logic
// runs entirely against the FileScope port before ever touching
// s.repo/s.files/s.folders/s.plans, so it's unit-testable here with a
// stub — the DB-backed create/resolve/expiry/revoke paths are covered in
// repository_integration_test.go.

type stubFileScope struct {
	orgOf map[string]string // fileID -> orgID; missing key = lookup error
}

func (s stubFileScope) LiveFileOrg(ctx context.Context, fileID string) (string, error) {
	org, ok := s.orgOf[fileID]
	if !ok {
		return "", errors.New("not found")
	}
	return org, nil
}

func (s stubFileScope) LiveFileFolder(ctx context.Context, fileID string) (string, error) {
	return "", errors.New("unused in these tests")
}

func TestCreateBundleShare_EmptyFileIDsRejected(t *testing.T) {
	s := &Service{scope: stubFileScope{}}
	_, err := s.CreateBundleShare(context.Background(), "org-1", nil, "user-1", nil)
	if !errors.Is(err, ErrEmptyBundle) {
		t.Fatalf("got err %v, want ErrEmptyBundle", err)
	}
}

func TestCreateBundleShare_OnlyBlankIDsRejected(t *testing.T) {
	s := &Service{scope: stubFileScope{}}
	_, err := s.CreateBundleShare(context.Background(), "org-1", []string{"", ""}, "user-1", nil)
	if !errors.Is(err, ErrEmptyBundle) {
		t.Fatalf("got err %v, want ErrEmptyBundle (blank IDs should be dropped before the empty check)", err)
	}
}

func TestCreateBundleShare_FileFromAnotherOrgRejected(t *testing.T) {
	s := &Service{scope: stubFileScope{orgOf: map[string]string{"file-1": "org-2"}}}
	_, err := s.CreateBundleShare(context.Background(), "org-1", []string{"file-1"}, "user-1", nil)
	if !errors.Is(err, ErrFileNotShareable) {
		t.Fatalf("got err %v, want ErrFileNotShareable", err)
	}
}

func TestCreateBundleShare_UnknownFileRejected(t *testing.T) {
	s := &Service{scope: stubFileScope{orgOf: map[string]string{}}}
	_, err := s.CreateBundleShare(context.Background(), "org-1", []string{"ghost-file"}, "user-1", nil)
	if !errors.Is(err, ErrFileNotShareable) {
		t.Fatalf("got err %v, want ErrFileNotShareable for a file the scope lookup can't find", err)
	}
}

func TestShareLink_KindDiscrimination(t *testing.T) {
	fileID, folderID := "file-1", "folder-1"
	cases := []struct {
		name string
		link ShareLink
		want ShareKind
	}{
		{"file wins when FileID is set", ShareLink{FileID: &fileID}, KindFile},
		{"folder when FolderID is set", ShareLink{FolderID: &folderID}, KindFolder},
		{"bundle when neither is set", ShareLink{}, KindBundle},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.link.Kind(); got != c.want {
				t.Fatalf("Kind() = %s, want %s", got, c.want)
			}
		})
	}
}
