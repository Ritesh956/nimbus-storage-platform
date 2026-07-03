// Package sharing owns share links and access resolution for them —
// see docs/03-hld.md §1 (module: sharing).
package sharing

import (
	"errors"
	"time"
)

type ShareLink struct {
	ID        string
	FileID    string
	Token     string
	CreatedBy string
	ExpiresAt *time.Time
	CreatedAt time.Time
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

var (
	ErrNotFound         = errors.New("share link not found")
	ErrExpired          = errors.New("share link has expired")
	ErrFileHasNoVersion = errors.New("shared file has no version to serve")
)
