package file

import (
	"context"

	"nimbus/internal/storage"
)

// FolderOrgLookup is the port file uses to validate that a move target
// belongs to the same org, without importing folder's data-access layer
// directly (docs/03-hld.md §1). The FK on files.folder_id already
// guarantees the folder exists; this closes the remaining gap (moving a
// file into a folder that exists but belongs to a different org).
type FolderOrgLookup interface {
	OrgIDOf(ctx context.Context, folderID string) (orgID string, err error)
}

// file depends on storage directly (not via a port) per its documented
// dependency in docs/03-hld.md §1 — storage is shared distributed-systems
// infrastructure other domain modules legitimately depend on, unlike the
// peer-to-peer module boundaries the ports above exist to enforce.
type Service struct {
	repo        *Repository
	folders     FolderOrgLookup
	storageRepo *storage.Repository
	router      *storage.Router
}

func NewService(repo *Repository, folders FolderOrgLookup, storageRepo *storage.Repository, router *storage.Router) *Service {
	return &Service{repo: repo, folders: folders, storageRepo: storageRepo, router: router}
}

func (s *Service) Get(ctx context.Context, id string) (File, error) {
	return s.repo.Get(ctx, id)
}

func (s *Service) GetAny(ctx context.Context, id string) (File, error) {
	return s.repo.GetAny(ctx, id)
}

func (s *Service) ListInFolder(ctx context.Context, folderID string) ([]File, error) {
	return s.repo.ListInFolder(ctx, folderID)
}

func (s *Service) Update(ctx context.Context, current File, name *string, folderID *string) (File, error) {
	if folderID != nil {
		orgID, err := s.folders.OrgIDOf(ctx, *folderID)
		if err != nil || orgID != current.OrgID {
			return File{}, ErrInvalidFolder
		}
	}
	return s.repo.Update(ctx, current.ID, name, folderID)
}

func (s *Service) Delete(ctx context.Context, id string) error {
	return s.repo.SoftDelete(ctx, id)
}

func (s *Service) Restore(ctx context.Context, id string) error {
	return s.repo.Restore(ctx, id)
}

func (s *Service) Purge(ctx context.Context, current File) error {
	if current.DeletedAt == nil {
		return ErrNotTrashed
	}
	return s.repo.Purge(ctx, current.ID)
}

func (s *Service) ListVersions(ctx context.Context, fileID string) ([]Version, error) {
	return s.repo.ListVersions(ctx, fileID)
}

// DownloadPlan resolves versionID's chunk list into presigned GET URLs,
// healthy replica first with a fallback second, per docs/02-system-design.md
// §2.4 and docs/06-api-design.md §6.
func (s *Service) DownloadPlan(ctx context.Context, fileID, versionID string) ([]DownloadPlanChunk, error) {
	if _, err := s.repo.GetVersion(ctx, fileID, versionID); err != nil {
		return nil, err
	}
	chunks, err := s.repo.GetVersionChunks(ctx, versionID)
	if err != nil {
		return nil, err
	}

	plan := make([]DownloadPlanChunk, len(chunks))
	for i, c := range chunks {
		nodeIDs, err := s.storageRepo.LocationsForChunk(ctx, c.Hash)
		if err != nil {
			return nil, err
		}
		orderHealthyFirst(nodeIDs, s.router)

		targets := make([]string, 0, len(nodeIDs))
		for _, nodeID := range nodeIDs {
			url, _, err := s.router.PresignGet(ctx, nodeID, c.Hash)
			if err != nil {
				continue // skip a replica whose node can't be presigned right now; try the rest
			}
			targets = append(targets, url)
		}
		plan[i] = DownloadPlanChunk{Sequence: c.Sequence, Hash: c.Hash, Targets: targets}
	}
	return plan, nil
}

// orderHealthyFirst stably moves healthy nodes ahead of down ones, so the
// client's primary attempt is the one most likely to succeed while still
// keeping a fallback in the list (docs/02-system-design.md §2.4).
func orderHealthyFirst(nodeIDs []storage.NodeID, router *storage.Router) {
	healthyCount := 0
	for i, id := range nodeIDs {
		if router.IsHealthy(id) {
			nodeIDs[healthyCount], nodeIDs[i] = nodeIDs[i], nodeIDs[healthyCount]
			healthyCount++
		}
	}
}

// RestoreVersion validates versionID belongs to current, then repoints
// latest_version_id at it.
func (s *Service) RestoreVersion(ctx context.Context, current File, versionID string) (File, error) {
	if _, err := s.repo.GetVersion(ctx, current.ID, versionID); err != nil {
		return File{}, err
	}
	return s.repo.RestoreVersion(ctx, current.ID, versionID)
}
