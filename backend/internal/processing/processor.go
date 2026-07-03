package processing

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"nimbus/internal/activity"
	"nimbus/internal/events"
	"nimbus/internal/file"
	"nimbus/internal/storage"
)

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
		data, err := p.reassemble(ctx, evt.VersionID)
		if err != nil {
			return fmt.Errorf("reassemble chunks: %w", err)
		}
		thumb, err = generateImageThumbnail(data)
		if err != nil {
			// A corrupt or unsupported-subformat image will never succeed
			// on redelivery — log and skip rather than retry forever.
			logger.Warn("thumbnail generation failed, skipping", "error", err)
			return nil
		}
	case version.MimeType == "application/pdf":
		thumb, err = generatePDFPlaceholder()
		if err != nil {
			return fmt.Errorf("generate pdf placeholder: %w", err)
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
	if err := p.router.PutObject(ctx, nodeIDs[0], key, thumb, "image/jpeg"); err != nil {
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

		var obj io.ReadCloser
		var getErr error
		for _, nodeID := range nodeIDs {
			obj, getErr = p.router.GetObject(ctx, nodeID, c.Hash)
			if getErr == nil {
				break
			}
		}
		if getErr != nil {
			return nil, fmt.Errorf("fetch chunk %s from any replica: %w", c.Hash, getErr)
		}
		_, copyErr := io.Copy(&buf, obj)
		obj.Close()
		if copyErr != nil {
			return nil, fmt.Errorf("read chunk %s: %w", c.Hash, copyErr)
		}
	}
	return buf.Bytes(), nil
}

func thumbnailObjectKey(versionID string) string { return "thumb:" + versionID }
