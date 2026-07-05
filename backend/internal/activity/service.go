package activity

import "context"

const (
	defaultLimit = 20
	maxLimit     = 100
)

// LiveNotifier is the port live.Publisher satisfies (backlog #12): fire-and-
// forget announcement of a just-recorded event so connected browsers hear
// about it without polling. Every Record* method calls it after a successful
// insert — activity.Service is the one funnel both nimbus-api and
// nimbus-worker events already pass through, which is what makes it the
// right hook point.
type LiveNotifier interface {
	NotifyActivity(ctx context.Context, orgID, verb, targetType, targetID string)
}

type Service struct {
	repo     *Repository
	notifier LiveNotifier
}

func NewService(repo *Repository, notifier LiveNotifier) *Service {
	return &Service{repo: repo, notifier: notifier}
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
	if err := s.repo.Record(ctx, orgID, &actorUserID, VerbUploaded, TargetTypeFile, fileID); err != nil {
		return err
	}
	s.notifier.NotifyActivity(ctx, orgID, VerbUploaded, TargetTypeFile, fileID)
	return nil
}

// RecordThumbnail is written by nimbus-worker after a successful thumbnail
// generation — actor is nil (system-generated), which is exactly what
// activity_events.actor_user_id's nullable design already anticipated
// (docs/05-database-design.md).
func (s *Service) RecordThumbnail(ctx context.Context, orgID, fileID string) error {
	if err := s.repo.Record(ctx, orgID, nil, VerbThumbnailGenerated, TargetTypeFile, fileID); err != nil {
		return err
	}
	s.notifier.NotifyActivity(ctx, orgID, VerbThumbnailGenerated, TargetTypeFile, fileID)
	return nil
}
