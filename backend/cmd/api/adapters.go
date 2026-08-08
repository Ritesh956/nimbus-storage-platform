package main

// Cross-module adapters: each satisfies a small port interface owned by the
// module that needs it (docs/03-hld.md §1's "no module reaches into
// another's Postgres tables" rule), using another module's already-public
// Repository methods. Kept in their own file, separate from the wire_*.go
// files that construct and connect services, so the two concerns — "how do
// two modules talk to each other" vs. "what gets built in what order" —
// don't have to be read together to skim either one.

import (
	"context"
	"errors"
	"time"

	"nimbus/internal/activity"
	"nimbus/internal/auth"
	"nimbus/internal/file"
	"nimbus/internal/folder"
	"nimbus/internal/org"
	"nimbus/internal/sharing"
	"nimbus/internal/storage"
)

// userLookupAdapter satisfies org.UserLookup using auth.Repository, so org
// never imports auth's data-access layer directly (docs/03-hld.md §1).
type userLookupAdapter struct{ repo *auth.Repository }

func (a userLookupAdapter) GetUserByEmail(ctx context.Context, email string) (string, error) {
	u, err := a.repo.GetUserByEmail(ctx, email)
	if err != nil {
		return "", err
	}
	return u.ID, nil
}

// membershipAdapter satisfies folder.MembershipChecker and
// file.MembershipChecker (identical method sets) using org.Repository.
type membershipAdapter struct{ repo *org.Repository }

func (a membershipAdapter) IsMember(ctx context.Context, orgID, userID string) (bool, error) {
	_, err := a.repo.GetMembership(ctx, orgID, userID)
	if errors.Is(err, org.ErrNotMember) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// folderOrgLookupAdapter satisfies file.FolderOrgLookup using
// folder.Repository, so file can validate a move target's org without
// importing folder's data-access layer directly.
type folderOrgLookupAdapter struct{ repo *folder.Repository }

func (a folderOrgLookupAdapter) OrgIDOf(ctx context.Context, folderID string) (string, error) {
	f, err := a.repo.Get(ctx, folderID)
	if err != nil {
		return "", err
	}
	return f.OrgID, nil
}

// fileListerAdapter satisfies folder.FileLister using file.Repository, so
// folder can list a folder's files without importing file's data-access
// layer directly.
type fileListerAdapter struct{ repo *file.Repository }

func (a fileListerAdapter) ListInFolder(ctx context.Context, folderID string) ([]folder.FileSummary, error) {
	files, err := a.repo.ListInFolder(ctx, folderID)
	if err != nil {
		return nil, err
	}
	out := make([]folder.FileSummary, len(files))
	for i, f := range files {
		out[i] = folder.FileSummary{ID: f.ID, Name: f.Name, SizeBytes: f.SizeBytes, MimeType: f.MimeType, HasThumbnail: f.HasThumbnail}
	}
	return out, nil
}

// fileChunkResolverAdapter satisfies storage.FileChunkResolver using
// file.Repository — resolves a file's latest version's chunk list for the
// admin ring view (backlog #13).
type fileChunkResolverAdapter struct{ repo *file.Repository }

func (a fileChunkResolverAdapter) LatestVersionChunks(ctx context.Context, fileID string) ([]storage.ChunkRef, error) {
	f, err := a.repo.GetAny(ctx, fileID)
	if err != nil {
		return nil, err
	}
	if f.LatestVersionID == nil {
		return []storage.ChunkRef{}, nil
	}
	chunks, err := a.repo.GetVersionChunks(ctx, *f.LatestVersionID)
	if err != nil {
		return nil, err
	}
	out := make([]storage.ChunkRef, len(chunks))
	for i, c := range chunks {
		out[i] = storage.ChunkRef{Sequence: c.Sequence, Hash: c.Hash}
	}
	return out, nil
}

// fileShareLookupAdapter satisfies sharing.FileLookup using file.Repository,
// projecting only the public-safe fields (docs/06-api-design.md §7).
type fileShareLookupAdapter struct{ repo *file.Repository }

func (a fileShareLookupAdapter) GetForShare(ctx context.Context, fileID string) (sharing.FileInfo, error) {
	f, err := a.repo.Get(ctx, fileID)
	if err != nil {
		return sharing.FileInfo{}, err
	}
	info := sharing.FileInfo{ID: f.ID, Name: f.Name, LatestVersionID: f.LatestVersionID}
	if f.LatestVersionID != nil {
		v, err := a.repo.GetVersion(ctx, f.ID, *f.LatestVersionID)
		if err != nil {
			return sharing.FileInfo{}, err
		}
		info.SizeBytes, info.MimeType, info.ChecksumSHA256 = v.SizeBytes, v.MimeType, v.ChecksumSHA256
	}
	return info, nil
}

// fileScopeAdapter satisfies sharing.FileScope using file.Repository —
// org/folder of a live (non-trashed) file, for bundle-member validation
// and folder-share subtree checks.
type fileScopeAdapter struct{ repo *file.Repository }

func (a fileScopeAdapter) LiveFileOrg(ctx context.Context, fileID string) (string, error) {
	f, err := a.repo.Get(ctx, fileID)
	if err != nil {
		return "", err
	}
	return f.OrgID, nil
}

func (a fileScopeAdapter) LiveFileFolder(ctx context.Context, fileID string) (string, error) {
	f, err := a.repo.Get(ctx, fileID)
	if err != nil {
		return "", err
	}
	return f.FolderID, nil
}

// folderShareAdapter satisfies sharing.FolderResolver using
// folder.Repository + file.Repository — public-safe folder projections,
// share listings, and the subtree test folder shares scope-check against.
type folderShareAdapter struct {
	folders *folder.Repository
	files   *file.Repository
}

func (a folderShareAdapter) GetFolderForShare(ctx context.Context, folderID string) (sharing.FolderInfo, error) {
	f, err := a.folders.Get(ctx, folderID) // trashed folders 404 — trashing a shared folder kills the link
	if err != nil {
		return sharing.FolderInfo{}, err
	}
	return sharing.FolderInfo{ID: f.ID, Name: f.Name}, nil
}

func (a folderShareAdapter) ListShareChildren(ctx context.Context, folderID string) ([]sharing.FolderInfo, []sharing.FileInfo, error) {
	f, err := a.folders.Get(ctx, folderID)
	if err != nil {
		return nil, nil, err
	}
	subfolders, err := a.folders.ListChildren(ctx, f.OrgID, &f.ID)
	if err != nil {
		return nil, nil, err
	}
	folderInfos := make([]sharing.FolderInfo, len(subfolders))
	for i, sf := range subfolders {
		folderInfos[i] = sharing.FolderInfo{ID: sf.ID, Name: sf.Name}
	}
	entries, err := a.files.ListInFolder(ctx, folderID)
	if err != nil {
		return nil, nil, err
	}
	fileInfos := make([]sharing.FileInfo, len(entries))
	for i, e := range entries {
		info := sharing.FileInfo{ID: e.ID, Name: e.Name, LatestVersionID: e.LatestVersionID}
		if e.SizeBytes != nil {
			info.SizeBytes = *e.SizeBytes
		}
		if e.MimeType != nil {
			info.MimeType = *e.MimeType
		}
		fileInfos[i] = info
	}
	return folderInfos, fileInfos, nil
}

func (a folderShareAdapter) IsSelfOrDescendant(ctx context.Context, folderID, candidateID string) (bool, error) {
	return a.folders.IsSelfOrDescendant(ctx, folderID, candidateID)
}

// orgStorageStatsAdapter satisfies org.StorageStatsReader using
// file.Repository — the storage half of the owner usage view.
type orgStorageStatsAdapter struct{ repo *file.Repository }

func (a orgStorageStatsAdapter) OrgStorageStats(ctx context.Context, orgID string) (org.StorageStats, error) {
	used, err := a.repo.OrgUsageBytes(ctx, orgID)
	if err != nil {
		return org.StorageStats{}, err
	}
	live, trashed, err := a.repo.OrgFileCounts(ctx, orgID)
	if err != nil {
		return org.StorageStats{}, err
	}
	return org.StorageStats{UsedBytes: used, LiveFiles: live, TrashedFiles: trashed}, nil
}

// orgActivityStatsAdapter satisfies org.ActivityStatsReader using
// activity.Repository — per-member and per-verb aggregates for the usage
// view.
type orgActivityStatsAdapter struct{ repo *activity.Repository }

func (a orgActivityStatsAdapter) ActorStats(ctx context.Context, orgID string, since time.Time) (map[string]org.MemberActivityStat, error) {
	stats, err := a.repo.ActorStats(ctx, orgID, since)
	if err != nil {
		return nil, err
	}
	out := make(map[string]org.MemberActivityStat, len(stats))
	for userID, s := range stats {
		last := s.LastActiveAt
		out[userID] = org.MemberActivityStat{Events: s.EventsSince, LastActiveAt: &last}
	}
	return out, nil
}

func (a orgActivityStatsAdapter) VerbCounts(ctx context.Context, orgID string, since time.Time) (map[string]int, error) {
	return a.repo.VerbCounts(ctx, orgID, since)
}
