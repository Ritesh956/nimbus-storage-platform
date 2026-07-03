package upload

import (
	"context"
	"log/slog"
	"time"

	"nimbus/internal/events"
	"nimbus/internal/platform/idgen"
	"nimbus/internal/platform/logging"
	"nimbus/internal/storage"
)

// FileCreator is the port upload uses to turn a completed chunk set into a
// real file+version, without importing file's data-access layer directly
// (docs/03-hld.md §1).
type FileCreator interface {
	CreateWithVersion(ctx context.Context, orgID, folderID, name, createdBy string, sizeBytes int64, checksumSHA256, mimeType string, chunkHashes []string) (fileID, versionID string, err error)
	// AddVersion adds a new version to an existing file (re-upload) — the
	// TargetFileID path, docs/09-roadmap.md Day 6.
	AddVersion(ctx context.Context, fileID, createdBy string, sizeBytes int64, checksumSHA256, mimeType string, chunkHashes []string) (versionID string, err error)
	// GetForUpload resolves an existing file's org/folder/name for
	// authorizing and labeling a re-upload session.
	GetForUpload(ctx context.Context, fileID string) (orgID, folderID, name string, err error)
}

type FolderOrgLookup interface {
	OrgIDOf(ctx context.Context, folderID string) (orgID string, err error)
}

type MembershipChecker interface {
	IsMember(ctx context.Context, orgID, userID string) (bool, error)
}

// ActivityRecorder and EventPublisher are both best-effort side effects of
// a successful completion (see types.go doc comment on why activity isn't
// exclusively worker-driven here) — a failure in either is logged, not
// propagated, so a logging/eventing hiccup never fails an otherwise-successful
// upload.
type ActivityRecorder interface {
	RecordUpload(ctx context.Context, orgID, actorUserID, fileID string) error
}

type EventPublisher interface {
	PublishUploadCompleted(ctx context.Context, evt events.UploadCompleted) error
}

type Service struct {
	repo              *Repository
	router            *storage.Router
	files             FileCreator
	folders           FolderOrgLookup
	members           MembershipChecker
	activity          ActivityRecorder
	publisher         EventPublisher
	replicationFactor int
}

func NewService(repo *Repository, router *storage.Router, files FileCreator, folders FolderOrgLookup, members MembershipChecker, activity ActivityRecorder, publisher EventPublisher, replicationFactor int) *Service {
	return &Service{
		repo:              repo,
		router:            router,
		files:             files,
		folders:           folders,
		members:           members,
		activity:          activity,
		publisher:         publisher,
		replicationFactor: replicationFactor,
	}
}

// CheckChunks is the dedup check: given content hashes the client is about
// to chunk-and-upload, return which ones the store doesn't have yet
// (FR-7 — dedup is global, across users, so this needs no org scoping).
func (s *Service) CheckChunks(ctx context.Context, hashes []string) ([]string, error) {
	return s.repo.FindMissingChunks(ctx, hashes)
}

// InitUpload creates an upload session, after checking authorization.
// POST /v1/uploads has no org_id in its body (docs/06-api-design.md §5) —
// org is derived from folder_id (new file) or from the target file (new
// version of an existing one), so authorization happens here rather than
// via a path-based middleware.
//
// targetFileID, if non-empty, means this is a re-upload adding a new
// version to an existing file (docs/09-roadmap.md Day 6) — folderID/name
// are then derived from that file rather than taken from the request.
func (s *Service) InitUpload(ctx context.Context, userID, folderID, targetFileID, name string, sizeBytes int64, mimeType string) (Upload, error) {
	var orgID string
	var err error
	var targetPtr *string

	if targetFileID != "" {
		var fileName string
		orgID, folderID, fileName, err = s.files.GetForUpload(ctx, targetFileID)
		if err != nil {
			return Upload{}, ErrTargetFileNotFound
		}
		if name == "" {
			name = fileName
		}
		targetPtr = &targetFileID
	} else {
		orgID, err = s.folders.OrgIDOf(ctx, folderID)
		if err != nil {
			return Upload{}, ErrInvalidFolder
		}
	}

	ok, err := s.members.IsMember(ctx, orgID, userID)
	if err != nil {
		return Upload{}, err
	}
	if !ok {
		return Upload{}, ErrForbidden
	}
	return s.repo.CreateUpload(ctx, orgID, folderID, name, sizeBytes, mimeType, userID, targetPtr)
}

// InitChunk resolves N healthy replicas for hash and presigns a PUT target
// on each (docs/02-system-design.md §2.3).
func (s *Service) InitChunk(ctx context.Context, u Upload, hash string) ([]ChunkTarget, time.Time, error) {
	if u.Status != StatusInProgress {
		return nil, time.Time{}, ErrUploadNotInProgress
	}
	nodeIDs, err := s.router.Resolve(hash, s.replicationFactor)
	if err != nil {
		return nil, time.Time{}, err
	}

	targets := make([]ChunkTarget, len(nodeIDs))
	var expiresAt time.Time
	for i, nodeID := range nodeIDs {
		putURL, exp, err := s.router.PresignPut(ctx, nodeID, hash)
		if err != nil {
			return nil, time.Time{}, err
		}
		targets[i] = ChunkTarget{NodeID: string(nodeID), PutURL: putURL}
		expiresAt = exp
	}

	if err := s.repo.UpsertChunkAttemptPresigned(ctx, u.ID, hash); err != nil {
		return nil, time.Time{}, err
	}
	return targets, expiresAt, nil
}

// CommitChunk verifies the replicas actually agree on what was written,
// then records the chunk as committed both for this upload and globally.
//
// S3-compatible ETags are MD5 digests of the object content, not our
// SHA-256 content-address — so this can't compare an ETag directly against
// hash. What it *can* do, and does, is require every replica's ETag to be
// identical: since MD5 is deterministic, two replicas that received the
// same bytes produce the same ETag, so a mismatch here is real evidence of
// divergence between the two PUTs (NFR-5's "catch injected corruption").
// Verifying the ETag equals MD5(original content) would need either
// client-computed MD5 alongside SHA-256, or S3 checksum trailers — noted
// as a roadmap item, not implemented here.
func (s *Service) CommitChunk(ctx context.Context, u Upload, hash string, sizeBytes int64, etags map[string]string) error {
	if u.Status != StatusInProgress {
		return ErrUploadNotInProgress
	}
	state, err := s.repo.GetChunkAttemptState(ctx, u.ID, hash)
	if err != nil {
		return err
	}
	if state != ChunkPresigned {
		return ErrInvalidState
	}
	if len(etags) == 0 {
		return ErrChecksumMismatch
	}
	var first string
	for _, etag := range etags {
		if etag == "" {
			return ErrChecksumMismatch
		}
		if first == "" {
			first = etag
			continue
		}
		if etag != first {
			return ErrChecksumMismatch
		}
	}

	if err := s.repo.MarkChunkCommitted(ctx, u.ID, hash, etags); err != nil {
		return err
	}
	if err := s.repo.UpsertGlobalChunk(ctx, hash, sizeBytes); err != nil {
		return err
	}
	for nodeID := range etags {
		if err := s.repo.UpsertChunkLocation(ctx, hash, nodeID); err != nil {
			return err
		}
	}
	return nil
}

// CompleteUpload turns a fully-committed (or deduped) chunk set into a real
// file+version. idempotencyKey, if non-empty, makes a retried call return
// the original result instead of erroring or double-creating
// (docs/06-api-design.md §5).
func (s *Service) CompleteUpload(ctx context.Context, u Upload, idempotencyKey string, chunkOrder []string, sizeBytes int64, checksumSHA256 string) (fileID, versionID string, err error) {
	if idempotencyKey != "" {
		if existing, err := s.repo.GetByIdempotencyKey(ctx, u.CreatedBy, idempotencyKey); err == nil {
			if existing.FileID != nil && existing.VersionID != nil {
				return *existing.FileID, *existing.VersionID, nil
			}
		}
	}
	if u.Status != StatusInProgress {
		return "", "", ErrUploadNotInProgress
	}
	if len(chunkOrder) == 0 {
		return "", "", ErrEmptyChunkOrder
	}
	missing, err := s.repo.FindMissingChunks(ctx, chunkOrder)
	if err != nil {
		return "", "", err
	}
	if len(missing) > 0 {
		return "", "", ErrMissingChunks
	}

	if u.TargetFileID != nil {
		fileID = *u.TargetFileID
		versionID, err = s.files.AddVersion(ctx, fileID, u.CreatedBy, sizeBytes, checksumSHA256, u.MimeType, chunkOrder)
	} else {
		fileID, versionID, err = s.files.CreateWithVersion(ctx, u.OrgID, u.FolderID, u.Name, u.CreatedBy, sizeBytes, checksumSHA256, u.MimeType, chunkOrder)
	}
	if err != nil {
		return "", "", err
	}

	var key *string
	if idempotencyKey != "" {
		key = &idempotencyKey
	}
	if err := s.repo.CompleteUpload(ctx, u.ID, fileID, versionID, key); err != nil {
		if err == ErrAlreadyCompleting {
			// Someone else's concurrent /complete call won the race; return
			// their result rather than error (see repository.go comment).
			winner, ferr := s.repo.GetUpload(ctx, u.ID)
			if ferr == nil && winner.FileID != nil && winner.VersionID != nil {
				return *winner.FileID, *winner.VersionID, nil
			}
		}
		return "", "", err
	}

	if err := s.activity.RecordUpload(ctx, u.OrgID, u.CreatedBy, fileID); err != nil {
		slog.Default().Warn("failed to record upload activity", "error", err, "upload_id", u.ID)
	}
	evt := events.UploadCompleted{
		EventID:   idgen.NewUUID(),
		FileID:    fileID,
		VersionID: versionID,
		OrgID:     u.OrgID,
		RequestID: logging.RequestIDFromContext(ctx),
	}
	if err := s.publisher.PublishUploadCompleted(ctx, evt); err != nil {
		slog.Default().Warn("failed to publish upload.completed event", "error", err, "upload_id", u.ID)
	}

	return fileID, versionID, nil
}
