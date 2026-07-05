// Package sharing owns share links and access resolution for them —
// see docs/03-hld.md §1 (module: sharing).
package sharing

import (
	"errors"
	"time"
)

// ShareKind is a link's scope. Exactly one applies per link (migration
// 000009): a single file, a folder (navigable subtree), or a hand-picked
// multi-file bundle.
type ShareKind string

const (
	KindFile   ShareKind = "file"
	KindFolder ShareKind = "folder"
	KindBundle ShareKind = "bundle"
)

type ShareLink struct {
	ID        string
	OrgID     string
	FileID    *string
	FolderID  *string
	Token     string
	CreatedBy string
	ExpiresAt *time.Time
	CreatedAt time.Time
}

// Kind derives the scope from which reference the row carries — a bundle
// is "neither", its members living in share_link_files.
func (l ShareLink) Kind() ShareKind {
	switch {
	case l.FileID != nil:
		return KindFile
	case l.FolderID != nil:
		return KindFolder
	default:
		return KindBundle
	}
}

// FileInfo is the minimal, public-safe file projection returned to an
// unauthenticated share-link visitor — deliberately excludes org_id,
// folder_id, created_by, etc. (docs/06-api-design.md §7: GET /v1/shares/{token}
// is public, no auth header, so it must not leak internal org structure).
type FileInfo struct {
	ID              string
	Name            string
	LatestVersionID *string
	SizeBytes       int64
	MimeType        string
	ChecksumSHA256  string
}

// FolderInfo is FileInfo's folder counterpart — id and display name only.
type FolderInfo struct {
	ID   string
	Name string
}

var (
	ErrNotFound         = errors.New("share link not found")
	ErrExpired          = errors.New("share link has expired")
	ErrFileHasNoVersion = errors.New("shared file has no version to serve")
	// ErrNotInShare means the requested item exists but isn't covered by
	// this link's scope — surfaced as a 404, not a 403, so a token holder
	// can't probe which file/folder IDs exist outside their share.
	ErrNotInShare       = errors.New("item is not part of this share")
	ErrEmptyBundle      = errors.New("a bundle share needs at least one file")
	ErrFileNotShareable = errors.New("file is trashed, missing, or in another organization")
)
