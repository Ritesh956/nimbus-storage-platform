// Package search owns metadata queries over files — see docs/03-hld.md §1.
package search

import "time"

// Filters mirrors the query params on GET /v1/orgs/{orgId}/search
// (docs/06-api-design.md §8). Zero-value fields mean "no filter" — every
// field is optional.
type Filters struct {
	Query    string
	Type     string // prefix match against mime_type, e.g. "image" matches "image/png"
	OwnerID  string
	DateFrom *time.Time
	DateTo   *time.Time
	SizeMin  *int64
	SizeMax  *int64
	Cursor   string
	Limit    int
}

type Result struct {
	FileID    string
	Name      string
	FolderID  string
	OwnerID   string
	CreatedAt time.Time
	SizeBytes *int64 // nil if the file somehow has no version yet
	MimeType  *string
}
