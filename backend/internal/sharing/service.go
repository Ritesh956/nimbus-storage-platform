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

// FileScope answers "which org / which folder does this *live* file belong
// to" — bundle-member validation at creation, and subtree membership for a
// folder share's download path. Live means non-trashed: a trashed file
// can't be added to a bundle and stops resolving inside a folder share.
type FileScope interface {
	LiveFileOrg(ctx context.Context, fileID string) (string, error)
	LiveFileFolder(ctx context.Context, fileID string) (string, error)
}

// FolderResolver is the folder-side port for folder shares: a public-safe
// projection of the shared folder, its children (for the public listing),
// and the subtree test that decides whether a requested folder/file is
// actually inside the share.
type FolderResolver interface {
	GetFolderForShare(ctx context.Context, folderID string) (FolderInfo, error)
	ListShareChildren(ctx context.Context, folderID string) ([]FolderInfo, []FileInfo, error)
	IsSelfOrDescendant(ctx context.Context, folderID, candidateID string) (bool, error)
}

// file.Service.DownloadPlan already matches this signature exactly, so
// it's passed as a DownloadPlanner directly — no adapter needed.
type DownloadPlanner interface {
	DownloadPlan(ctx context.Context, fileID, versionID string) ([]file.DownloadPlanChunk, error)
}

type Service struct {
	repo    *Repository
	files   FileLookup
	scope   FileScope
	folders FolderResolver
	plans   DownloadPlanner
}

func NewService(repo *Repository, files FileLookup, scope FileScope, folders FolderResolver, plans DownloadPlanner) *Service {
	return &Service{repo: repo, files: files, scope: scope, folders: folders, plans: plans}
}

func (s *Service) CreateShare(ctx context.Context, orgID, fileID, createdBy string, expiresAt *time.Time) (ShareLink, error) {
	return s.repo.CreateFileShare(ctx, orgID, fileID, createdBy, expiresAt)
}

func (s *Service) CreateFolderShare(ctx context.Context, orgID, folderID, createdBy string, expiresAt *time.Time) (ShareLink, error) {
	return s.repo.CreateFolderShare(ctx, orgID, folderID, createdBy, expiresAt)
}

// CreateBundleShare validates every requested file is live and belongs to
// orgID (the caller's membership in orgID was already checked by the route
// middleware). A "bundle" of one normalizes to a plain file share — the
// recipient gets the simpler single-file page and nothing is lost.
func (s *Service) CreateBundleShare(ctx context.Context, orgID string, fileIDs []string, createdBy string, expiresAt *time.Time) (ShareLink, error) {
	seen := make(map[string]bool, len(fileIDs))
	unique := make([]string, 0, len(fileIDs))
	for _, id := range fileIDs {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		unique = append(unique, id)
	}
	if len(unique) == 0 {
		return ShareLink{}, ErrEmptyBundle
	}
	for _, id := range unique {
		fileOrg, err := s.scope.LiveFileOrg(ctx, id)
		if err != nil || fileOrg != orgID {
			return ShareLink{}, ErrFileNotShareable
		}
	}
	if len(unique) == 1 {
		return s.repo.CreateFileShare(ctx, orgID, unique[0], createdBy, expiresAt)
	}
	return s.repo.CreateBundleShare(ctx, orgID, unique, createdBy, expiresAt)
}

// GetLinkByToken exposes the raw link (including OrgID) for the revoke
// handler's authorization check.
func (s *Service) GetLinkByToken(ctx context.Context, token string) (ShareLink, error) {
	return s.repo.GetByToken(ctx, token)
}

func (s *Service) Revoke(ctx context.Context, token string) error {
	return s.repo.Delete(ctx, token)
}

type ResolvedShare struct {
	Kind ShareKind
	// Kind == KindFile
	File         FileInfo
	DownloadPlan []file.DownloadPlanChunk
	// Kind == KindFolder — the shared folder itself; deeper levels come
	// from ResolveFolderChildren as the visitor navigates.
	Folder     FolderInfo
	Subfolders []FolderInfo
	// Kind == KindFolder (direct children) or KindBundle (members).
	Files []FileInfo
}

// Resolve is the public, unauthenticated read path behind
// GET /v1/shares/{token} (docs/06-api-design.md §7).
func (s *Service) Resolve(ctx context.Context, token string) (ResolvedShare, error) {
	link, err := s.liveLink(ctx, token)
	if err != nil {
		return ResolvedShare{}, err
	}

	switch link.Kind() {
	case KindFile:
		info, plan, err := s.filePlan(ctx, *link.FileID)
		if err != nil {
			return ResolvedShare{}, err
		}
		return ResolvedShare{Kind: KindFile, File: info, DownloadPlan: plan}, nil

	case KindFolder:
		folder, subfolders, files, err := s.folderListing(ctx, *link.FolderID)
		if err != nil {
			return ResolvedShare{}, err
		}
		return ResolvedShare{Kind: KindFolder, Folder: folder, Subfolders: subfolders, Files: files}, nil

	default: // KindBundle
		ids, err := s.repo.BundleFileIDs(ctx, link.ID)
		if err != nil {
			return ResolvedShare{}, err
		}
		files := make([]FileInfo, 0, len(ids))
		for _, id := range ids {
			info, err := s.files.GetForShare(ctx, id)
			if err != nil {
				continue // trashed since sharing — drop from the listing rather than break the whole bundle
			}
			files = append(files, info)
		}
		return ResolvedShare{Kind: KindBundle, Files: files}, nil
	}
}

// ResolveFolderChildren serves navigation inside a folder share: the
// public listing of any folder within the shared subtree.
func (s *Service) ResolveFolderChildren(ctx context.Context, token, folderID string) (FolderInfo, []FolderInfo, []FileInfo, error) {
	link, err := s.liveLink(ctx, token)
	if err != nil {
		return FolderInfo{}, nil, nil, err
	}
	if link.Kind() != KindFolder {
		return FolderInfo{}, nil, nil, ErrNotInShare
	}
	ok, err := s.folders.IsSelfOrDescendant(ctx, *link.FolderID, folderID)
	if err != nil {
		return FolderInfo{}, nil, nil, err
	}
	if !ok {
		return FolderInfo{}, nil, nil, ErrNotInShare
	}
	return s.folderListing(ctx, folderID)
}

// ResolveDownloadPlan authorizes fileID against the link's scope, then
// builds the presigned download plan — the public download path for
// folder- and bundle-share items (a single-file share embeds its plan in
// Resolve directly).
func (s *Service) ResolveDownloadPlan(ctx context.Context, token, fileID string) (FileInfo, []file.DownloadPlanChunk, error) {
	link, err := s.liveLink(ctx, token)
	if err != nil {
		return FileInfo{}, nil, err
	}

	var covered bool
	switch link.Kind() {
	case KindFile:
		covered = *link.FileID == fileID
	case KindBundle:
		covered, err = s.repo.IsBundleMember(ctx, link.ID, fileID)
	case KindFolder:
		var folderID string
		folderID, err = s.scope.LiveFileFolder(ctx, fileID)
		if err != nil {
			return FileInfo{}, nil, ErrNotInShare
		}
		covered, err = s.folders.IsSelfOrDescendant(ctx, *link.FolderID, folderID)
	}
	if err != nil {
		return FileInfo{}, nil, err
	}
	if !covered {
		return FileInfo{}, nil, ErrNotInShare
	}

	return s.filePlan(ctx, fileID)
}

func (s *Service) liveLink(ctx context.Context, token string) (ShareLink, error) {
	link, err := s.repo.GetByToken(ctx, token)
	if err != nil {
		return ShareLink{}, err
	}
	if link.ExpiresAt != nil && time.Now().After(*link.ExpiresAt) {
		return ShareLink{}, ErrExpired
	}
	return link, nil
}

func (s *Service) filePlan(ctx context.Context, fileID string) (FileInfo, []file.DownloadPlanChunk, error) {
	info, err := s.files.GetForShare(ctx, fileID)
	if err != nil {
		return FileInfo{}, nil, err
	}
	if info.LatestVersionID == nil {
		return FileInfo{}, nil, ErrFileHasNoVersion
	}
	plan, err := s.plans.DownloadPlan(ctx, info.ID, *info.LatestVersionID)
	if err != nil {
		return FileInfo{}, nil, err
	}
	return info, plan, nil
}

func (s *Service) folderListing(ctx context.Context, folderID string) (FolderInfo, []FolderInfo, []FileInfo, error) {
	folder, err := s.folders.GetFolderForShare(ctx, folderID)
	if err != nil {
		return FolderInfo{}, nil, nil, ErrNotFound // shared folder has been trashed — the link is effectively dead
	}
	subfolders, files, err := s.folders.ListShareChildren(ctx, folderID)
	if err != nil {
		return FolderInfo{}, nil, nil, err
	}
	return folder, subfolders, files, nil
}
