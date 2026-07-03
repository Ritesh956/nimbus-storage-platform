package activity

import "context"

const (
	defaultLimit = 20
	maxLimit     = 100
)

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) List(ctx context.Context, orgID, cursor string, limit int) ([]Event, string, error) {
	if limit <= 0 || limit > maxLimit {
		limit = defaultLimit
	}
	return s.repo.List(ctx, orgID, cursor, limit)
}

// RecordUpload satisfies upload.ActivityRecorder directly (docs/03-hld.md
// §1: same-signature structural match, no adapter needed).
func (s *Service) RecordUpload(ctx context.Context, orgID, actorUserID, fileID string) error {
	return s.repo.Record(ctx, orgID, &actorUserID, VerbUploaded, TargetTypeFile, fileID)
}

// RecordThumbnail is written by nimbus-worker after a successful thumbnail
// generation — actor is nil (system-generated), which is exactly what
// activity_events.actor_user_id's nullable design already anticipated
// (docs/05-database-design.md).
func (s *Service) RecordThumbnail(ctx context.Context, orgID, fileID string) error {
	return s.repo.Record(ctx, orgID, nil, VerbThumbnailGenerated, TargetTypeFile, fileID)
}
