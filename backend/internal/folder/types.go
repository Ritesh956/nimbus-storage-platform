// Package folder owns the folder tree — see docs/03-hld.md §1.
package folder

import (
	"context"
	"errors"
	"time"
)

type Folder struct {
	ID        string
	OrgID     string
	ParentID  *string // nil = root
	Name      string
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt *time.Time
}

// FileSummary is the file projection folder needs for
// GET /v1/folders/{folderId}/children — deliberately not the full File type
// so this package doesn't need to import file's domain model, just the
// small FileLister port it depends on (docs/03-hld.md §1 module boundary).
// SizeBytes/MimeType are nil for a file with no readable latest version.
type FileSummary struct {
	ID           string
	Name         string
	SizeBytes    *int64
	MimeType     *string
	HasThumbnail bool
}

// PathEntry is one hop of a folder's ancestor chain (breadcrumbs).
type PathEntry struct {
	ID   string
	Name string
}

type FileLister interface {
	ListInFolder(ctx context.Context, folderID string) ([]FileSummary, error)
}

var (
	ErrNotFound      = errors.New("folder not found")
	ErrNameConflict  = errors.New("a folder with this name already exists here")
	ErrInvalidParent = errors.New("parent folder not found or in a different organization")
	ErrCyclicMove    = errors.New("cannot move a folder into itself or one of its own descendants")
)
