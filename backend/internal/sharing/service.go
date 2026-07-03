package sharing

import (
	"context"
	"time"

	"nimbus/internal/file"
)

// FileLookup is the port sharing uses to fetch a public-safe file
// projection, without importing file's data-access layer directly
// (docs/03-hld.md §1).
type FileLookup interface {
	GetForShare(ctx context.Context, fileID string) (FileInfo, error)
}

// FileOrgLookup is used only to authorize DELETE /v1/shares/{token} (revoke
// requires membership in the underlying file's org — there's no {fileId}
// in that route's path, so it can't go through file.RequireAccess).
type FileOrgLookup interface {
	OrgIDOf(ctx context.Context, fileID string) (orgID string, err error)
}

// file.Service.DownloadPlan already matches this signature exactly, so
// it's passed as a DownloadPlanner directly — no adapter needed.
type DownloadPlanner interface {
	DownloadPlan(ctx context.Context, fileID, versionID string) ([]file.DownloadPlanChunk, error)
}

type Service struct {
	repo    *Repository
	files   FileLookup
	fileOrg FileOrgLookup
	plans   DownloadPlanner
}

func NewService(repo *Repository, files FileLookup, fileOrg FileOrgLookup, plans DownloadPlanner) *Service {
	return &Service{repo: repo, files: files, fileOrg: fileOrg, plans: plans}
}

func (s *Service) CreateShare(ctx context.Context, fileID, createdBy string, expiresAt *time.Time) (ShareLink, error) {
	return s.repo.Create(ctx, fileID, createdBy, expiresAt)
}

// GetLinkByToken exposes the raw link (including FileID) for the revoke
// handler's authorization check — see FileOrgLookup's doc comment.
func (s *Service) GetLinkByToken(ctx context.Context, token string) (ShareLink, error) {
	return s.repo.GetByToken(ctx, token)
}

func (s *Service) FileOrgID(ctx context.Context, fileID string) (string, error) {
	return s.fileOrg.OrgIDOf(ctx, fileID)
}

func (s *Service) Revoke(ctx context.Context, token string) error {
	return s.repo.Delete(ctx, token)
}

type ResolvedShare struct {
	File         FileInfo
	DownloadPlan []file.DownloadPlanChunk
}

// Resolve is the public, unauthenticated read path behind
// GET /v1/shares/{token} (docs/06-api-design.md §7).
func (s *Service) Resolve(ctx context.Context, token string) (ResolvedShare, error) {
	link, err := s.repo.GetByToken(ctx, token)
	if err != nil {
		return ResolvedShare{}, err
	}
	if link.ExpiresAt != nil && time.Now().After(*link.ExpiresAt) {
		return ResolvedShare{}, ErrExpired
	}

	info, err := s.files.GetForShare(ctx, link.FileID)
	if err != nil {
		return ResolvedShare{}, err
	}
	if info.LatestVersionID == nil {
		return ResolvedShare{}, ErrFileHasNoVersion
	}

	plan, err := s.plans.DownloadPlan(ctx, info.ID, *info.LatestVersionID)
	if err != nil {
		return ResolvedShare{}, err
	}
	return ResolvedShare{File: info, DownloadPlan: plan}, nil
}
