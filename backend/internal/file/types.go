// Package file owns files, versions, and trash — see docs/03-hld.md §1.
package file

import (
	"errors"
	"time"
)

type File struct {
	ID              string
	OrgID           string
	FolderID        string
	Name            string
	LatestVersionID *string
	CreatedBy       string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	DeletedAt       *time.Time
}

type Version struct {
	ID             string
	FileID         string
	SizeBytes      int64
	ChecksumSHA256 string
	MimeType       string
	ThumbnailKey   *string
	CreatedBy      string
	CreatedAt      time.Time
}

// ListEntry is a File plus its latest version's display metadata — what
// the folder-children listing renders a row from without a per-file
// round-trip. SizeBytes/MimeType are nil for a file whose latest version
// row is missing (never expected in practice; see Repository.CreateWithVersion).
type ListEntry struct {
	File
	SizeBytes    *int64
	MimeType     *string
	HasThumbnail bool
}

// VersionChunk is one entry in a version's ordered chunk list.
type VersionChunk struct {
	Sequence int
	Hash     string
}

// DownloadPlanChunk is one chunk's download instructions: presigned GET
// URLs for its replicas, healthy-first, primary-plus-fallback
// (docs/06-api-design.md §6).
type DownloadPlanChunk struct {
	Sequence int
	Hash     string
	Targets  []string
}

var (
	ErrNotFound        = errors.New("file not found")
	ErrInvalidFolder   = errors.New("target folder not found or in a different organization")
	ErrNotTrashed      = errors.New("file must be trashed before it can be purged")
	ErrVersionNotFound = errors.New("version not found for this file")
	ErrNoThumbnail     = errors.New("no thumbnail exists for this file")
)
