package processing

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"go.opentelemetry.io/otel/codes"

	"nimbus/internal/activity"
	"nimbus/internal/events"
	"nimbus/internal/file"
	"nimbus/internal/platform/tracing"
	"nimbus/internal/storage"
)

var tracer = tracing.Tracer("nimbus/internal/processing")

// traced and tracedValue wrap fn in a child span named name — the
// events.Subscribe-started "process <subject>" span
// (internal/events/consumer.go) is this package's only caller, so every
// span here nests under it, giving the upload-complete → thumbnail-generated
// trace roadmap #14 asked for real internal structure (reassemble / generate
// / store as separate, timed child spans) instead of one opaque span.
func traced(ctx context.Context, name string, fn func(context.Context) error) error {
	ctx, span := tracer.Start(ctx, name)
	defer span.End()
	if err := fn(ctx); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	return nil
}

func tracedValue[T any](ctx context.Context, name string, fn func(context.Context) (T, error)) (T, error) {
	ctx, span := tracer.Start(ctx, name)
	defer span.End()
	v, err := fn(ctx)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	return v, err
}

type Processor struct {
	files       *file.Repository
	storageRepo *storage.Repository
	router      *storage.Router
	activity    *activity.Service
	logger      *slog.Logger
}

func NewProcessor(files *file.Repository, storageRepo *storage.Repository, router *storage.Router, activitySvc *activity.Service, logger *slog.Logger) *Processor {
	return &Processor{files: files, storageRepo: storageRepo, router: router, activity: activitySvc, logger: logger}
}

// Process handles one upload.completed event: reassemble -> thumbnail ->
// store -> record. A returned error triggers NATS redelivery (events.Subscribe),
// so only conditions actually worth retrying (transient I/O) should return
// one — an unsupported/corrupt file is a permanent no-op, not a retry.
func (p *Processor) Process(ctx context.Context, evt events.UploadCompleted) error {
	logger := p.logger.With("event_id", evt.EventID, "file_id", evt.FileID, "version_id", evt.VersionID, "request_id", evt.RequestID)

	version, err := p.files.GetVersion(ctx, evt.FileID, evt.VersionID)
	if err != nil {
		return fmt.Errorf("load version: %w", err)
	}

	var thumb []byte
	switch {
	case strings.HasPrefix(version.MimeType, "image/"):
		data, err := tracedValue(ctx, "reassemble_chunks", func(ctx context.Context) ([]byte, error) {
			return p.reassemble(ctx, evt.VersionID)
		})
		if err != nil {
			return fmt.Errorf("reassemble chunks: %w", err)
		}
		thumb, err = tracedValue(ctx, "generate_thumbnail", func(context.Context) ([]byte, error) {
			return generateImageThumbnail(data)
		})
		if err != nil {
			// A corrupt or unsupported-subformat image will never succeed
			// on redelivery — log and skip rather than retry forever.
			logger.Warn("thumbnail generation failed, skipping", "error", err)
			return nil
		}
	case version.MimeType == "application/pdf":
		data, err := tracedValue(ctx, "reassemble_chunks", func(ctx context.Context) ([]byte, error) {
			return p.reassemble(ctx, evt.VersionID)
		})
		if err != nil {
			return fmt.Errorf("reassemble chunks: %w", err)
		}
		thumb, err = tracedValue(ctx, "generate_thumbnail", func(context.Context) ([]byte, error) {
			return generatePDFThumbnail(data)
		})
		if err != nil {
			// A PDF pdfium can't parse won't succeed on redelivery either —
			// fall back to the placeholder rather than retry or skip, so the
			// file still gets *a* thumbnail.
			logger.Warn("pdf page render failed, using placeholder", "error", err)
			if thumb, err = generatePDFPlaceholder(); err != nil {
				return fmt.Errorf("generate pdf placeholder: %w", err)
			}
		}
	default:
		logger.Info("no thumbnail handler for mime type, skipping", "mime_type", version.MimeType)
		return nil
	}

	key := thumbnailObjectKey(evt.VersionID)
	nodeIDs, err := p.router.Resolve(key, 1)
	if err != nil {
		return fmt.Errorf("resolve thumbnail storage node: %w", err)
	}
	if err := traced(ctx, "store_thumbnail", func(ctx context.Context) error {
		return p.router.PutObject(ctx, nodeIDs[0], key, thumb, "image/jpeg")
	}); err != nil {
		return fmt.Errorf("store thumbnail: %w", err)
	}

	if err := p.files.SetThumbnailKey(ctx, evt.VersionID, key); err != nil {
		return fmt.Errorf("update thumbnail_key: %w", err)
	}

	if err := p.activity.RecordThumbnail(ctx, evt.OrgID, evt.FileID); err != nil {
		// Best-effort, same rationale as upload's own activity write
		// (internal/activity/types.go doc comment): an audit-log hiccup
		// shouldn't undo an otherwise-successful thumbnail.
		logger.Warn("failed to record thumbnail activity", "error", err)
	}

	logger.Info("thumbnail generated", "key", key, "bytes", len(thumb), "node", nodeIDs[0])
	return nil
}

// reassemble fetches each of a version's chunks from wherever they were
// actually committed (storageRepo.LocationsForChunk — recorded ground
// truth, not a fresh ring placement, same distinction file.DownloadPlan
// makes) and concatenates them in sequence order.
func (p *Processor) reassemble(ctx context.Context, versionID string) ([]byte, error) {
	chunks, err := p.files.GetVersionChunks(ctx, versionID)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	for _, c := range chunks {
		nodeIDs, err := p.storageRepo.LocationsForChunk(ctx, c.Hash)
		if err != nil {
			return nil, err
		}
		if len(nodeIDs) == 0 {
			return nil, fmt.Errorf("no locations recorded for chunk %s", c.Hash)
		}

		data, err := p.fetchVerifiedChunk(ctx, c.Hash, nodeIDs)
		if err != nil {
			return nil, err
		}
		buf.Write(data)
	}
	return buf.Bytes(), nil
}

// fetchVerifiedChunk fetches a chunk from each replica in order, re-hashing
// the bytes and comparing against hash before accepting them — chunks are
// content-addressed (FR-7), so the hash a replica is stored under is also
// the expected digest of its bytes. This is FR-8's "checksum verification
// ... on read", scoped to the one path where the server actually holds
// reassembled bytes in memory (thumbnail generation): the direct-to-client
// file download path (file.Service.DownloadPlan) hands out presigned URLs
// straight to MinIO and never touches the server, so it stays
// client-verified only — see docs/01-srs.md's FR-8 note. A replica that
// fails the check is treated the same as one that's simply unreachable:
// try the next one before giving up.
func (p *Processor) fetchVerifiedChunk(ctx context.Context, hash string, nodeIDs []storage.NodeID) ([]byte, error) {
	var lastErr error
	for _, nodeID := range nodeIDs {
		obj, err := p.router.GetObject(ctx, nodeID, hash)
		if err != nil {
			lastErr = fmt.Errorf("fetch from %s: %w", nodeID, err)
			p.logger.Warn("failed to fetch chunk replica, trying next", "chunk_hash", hash, "node", nodeID, "error", err)
			continue
		}
		data, err := io.ReadAll(obj)
		obj.Close()
		if err != nil {
			lastErr = fmt.Errorf("read from %s: %w", nodeID, err)
			p.logger.Warn("failed to read chunk replica, trying next", "chunk_hash", hash, "node", nodeID, "error", err)
			continue
		}
		if sum := sha256.Sum256(data); hex.EncodeToString(sum[:]) != hash {
			lastErr = fmt.Errorf("checksum mismatch reading chunk %s from %s", hash, nodeID)
			p.logger.Warn("chunk failed server-side checksum verification, trying next replica", "chunk_hash", hash, "node", nodeID)
			continue
		}
		return data, nil
	}
	return nil, fmt.Errorf("no replica of chunk %s passed checksum verification: %w", hash, lastErr)
}

func thumbnailObjectKey(versionID string) string { return "thumb:" + versionID }
