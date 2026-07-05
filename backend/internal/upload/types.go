// Package upload owns the chunk lifecycle: dedup check, presigned
// placement, commit, and completing an upload into a real file+version —
// see docs/03-hld.md §1 and docs/04-lld.md §2.
package upload

import (
	"errors"
	"time"
)

type Status string

const (
	StatusInProgress Status = "in_progress"
	StatusCompleted  Status = "completed"
	StatusAborted    Status = "aborted"
)

type Upload struct {
	ID                string
	OrgID             string
	FolderID          string
	Name              string
	DeclaredSizeBytes int64
	MimeType          string
	CreatedBy         string
	Status            Status
	IdempotencyKey    *string
	// TargetFileID, when set, means this upload adds a new version to an
	// existing file rather than creating one — see Service.InitUpload
	// (docs/09-roadmap.md Day 6, a gap in the original
	// docs/06-api-design.md contract).
	TargetFileID *string
	FileID       *string
	VersionID    *string
	CreatedAt    time.Time
}

type ChunkState string

const (
	ChunkPending   ChunkState = "pending"
	ChunkPresigned ChunkState = "presigned"
	ChunkCommitted ChunkState = "committed"
)

// ChunkTarget is one presigned replica destination returned by InitChunk.
type ChunkTarget struct {
	NodeID string
	PutURL string
}

var (
	ErrUploadNotFound      = errors.New("upload not found")
	ErrInvalidFolder       = errors.New("folder not found or in a different organization")
	ErrTargetFileNotFound  = errors.New("target file not found")
	ErrForbidden           = errors.New("not a member of this organization")
	ErrChunkNotFound       = errors.New("chunk attempt not found - call init first")
	ErrInvalidState        = errors.New("chunk is not in the expected state for this operation")
	ErrChecksumMismatch    = errors.New("replica checksums do not agree - possible corruption")
	ErrUploadNotInProgress = errors.New("upload is not in progress")
	ErrEmptyChunkOrder     = errors.New("chunk_order must not be empty")
	ErrMissingChunks       = errors.New("one or more chunks in chunk_order were never committed")
	ErrFileTooLarge        = errors.New("file exceeds the maximum upload size")
	ErrQuotaExceeded       = errors.New("organization storage quota exceeded")
	// ErrAlreadyCompleting signals a concurrent /complete call for the same
	// upload won the race (CAS on status = 'in_progress' matched zero
	// rows). The service re-fetches and returns the winner's result rather
	// than erroring — see Service.CompleteUpload.
	ErrAlreadyCompleting = errors.New("upload already completed by a concurrent request")
)
