package file

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"nimbus/internal/platform/idgen"
)

const foreignKeyViolation = "23503"

const fileColumns = `id, org_id, folder_id, name, latest_version_id, created_by, created_at, updated_at, deleted_at`

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func scanFile(row pgx.Row) (File, error) {
	var f File
	err := row.Scan(&f.ID, &f.OrgID, &f.FolderID, &f.Name, &f.LatestVersionID, &f.CreatedBy, &f.CreatedAt, &f.UpdatedAt, &f.DeletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return File{}, ErrNotFound
	}
	return f, err
}

func (r *Repository) Get(ctx context.Context, id string) (File, error) {
	return scanFile(r.pool.QueryRow(ctx,
		`SELECT `+fileColumns+` FROM files WHERE id = $1 AND deleted_at IS NULL`, id))
}

// GetAny loads a file regardless of trash state — needed by Restore/Purge,
// which by definition target an already-trashed file.
func (r *Repository) GetAny(ctx context.Context, id string) (File, error) {
	return scanFile(r.pool.QueryRow(ctx, `SELECT `+fileColumns+` FROM files WHERE id = $1`, id))
}

// ListInFolder joins each file with its latest version's display metadata
// (size, mime, thumbnail presence) so the file browser can render a row —
// including whether to fetch a thumbnail — without a per-file round-trip.
func (r *Repository) ListInFolder(ctx context.Context, folderID string) ([]ListEntry, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT f.id, f.org_id, f.folder_id, f.name, f.latest_version_id, f.created_by, f.created_at, f.updated_at, f.deleted_at,
		        v.size_bytes, v.mime_type, v.thumbnail_key IS NOT NULL
		 FROM files f
		 LEFT JOIN file_versions v ON v.id = f.latest_version_id
		 WHERE f.folder_id = $1 AND f.deleted_at IS NULL ORDER BY f.name`, folderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ListEntry
	for rows.Next() {
		var e ListEntry
		if err := rows.Scan(&e.ID, &e.OrgID, &e.FolderID, &e.Name, &e.LatestVersionID, &e.CreatedBy, &e.CreatedAt, &e.UpdatedAt, &e.DeletedAt,
			&e.SizeBytes, &e.MimeType, &e.HasThumbnail); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ListTrashed serves the trash UI (docs/09-roadmap.md Day 10's FR-25) — a
// real gap that existed until now: idx_files_org_deleted was designed for
// exactly this in the original Day 1 schema ("-- trash listing") but no
// endpoint ever queried it; restore-by-ID existed with nothing to show a
// user what they could restore.
func (r *Repository) ListTrashed(ctx context.Context, orgID string) ([]File, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+fileColumns+` FROM files WHERE org_id = $1 AND deleted_at IS NOT NULL ORDER BY deleted_at DESC`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []File
	for rows.Next() {
		var f File
		if err := rows.Scan(&f.ID, &f.OrgID, &f.FolderID, &f.Name, &f.LatestVersionID, &f.CreatedBy, &f.CreatedAt, &f.UpdatedAt, &f.DeletedAt); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// Update applies a rename and/or move (to a folder already validated to be
// in the same org — see Service.Update). name == nil / folderID == nil
// each leave that field unchanged.
func (r *Repository) Update(ctx context.Context, id string, name *string, folderID *string) (File, error) {
	var row pgx.Row
	switch {
	case name != nil && folderID != nil:
		row = r.pool.QueryRow(ctx,
			`UPDATE files SET name = $1, folder_id = $2, updated_at = now()
			 WHERE id = $3 AND deleted_at IS NULL RETURNING `+fileColumns,
			*name, *folderID, id)
	case name != nil:
		row = r.pool.QueryRow(ctx,
			`UPDATE files SET name = $1, updated_at = now()
			 WHERE id = $2 AND deleted_at IS NULL RETURNING `+fileColumns,
			*name, id)
	case folderID != nil:
		row = r.pool.QueryRow(ctx,
			`UPDATE files SET folder_id = $1, updated_at = now()
			 WHERE id = $2 AND deleted_at IS NULL RETURNING `+fileColumns,
			*folderID, id)
	default:
		return r.Get(ctx, id)
	}

	f, err := scanFile(row)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == foreignKeyViolation {
			return File{}, ErrInvalidFolder
		}
		return File{}, err
	}
	return f, nil
}

func (r *Repository) SoftDelete(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE files SET deleted_at = now(), updated_at = now() WHERE id = $1 AND deleted_at IS NULL`, id)
	return err
}

func (r *Repository) Restore(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE files SET deleted_at = NULL, updated_at = now() WHERE id = $1 AND deleted_at IS NOT NULL`, id)
	return err
}

// Purge is a hard delete, only ever called on an already-trashed row
// (enforced in Service.Purge) — trash-then-purge is the only path to
// permanent deletion, never a direct one (docs/06-api-design.md §4). Runs in
// a transaction because the org's freed bytes (every version this file ever
// had, matching OrgUsageBytes's own logical-bytes accounting) have to be
// summed *before* the DELETE cascades file_versions away.
func (r *Repository) Purge(ctx context.Context, id string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var orgID string
	var freedBytes int64
	err = tx.QueryRow(ctx,
		`SELECT f.org_id, COALESCE(SUM(v.size_bytes), 0)
		 FROM files f LEFT JOIN file_versions v ON v.file_id = f.id
		 WHERE f.id = $1 AND f.deleted_at IS NOT NULL
		 GROUP BY f.org_id`, id).Scan(&orgID, &freedBytes)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // nothing trashed under this id — matches the old no-op DELETE
	}
	if err != nil {
		return err
	}

	if _, err = tx.Exec(ctx, `DELETE FROM files WHERE id = $1 AND deleted_at IS NOT NULL`, id); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE organizations SET usage_bytes = usage_bytes - $1 WHERE id = $2`, freedBytes, orgID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// PurgeExpiredTrash hard-deletes every file trashed longer ago than
// olderThan — FR-11's retention window, called from nimbus-worker's GC tick
// (gc.TrashPurger). The row deletions cascade to file_versions and
// file_version_chunks, which is what makes the freed chunks visible to the
// chunk sweeper's mark phase. Per-org freed bytes are summed before the
// DELETE (same reason as Purge above) and credited back in one UPDATE per
// affected org — now() is fixed for the lifetime of a transaction, so both
// statements see the identical cutoff despite running as separate queries.
func (r *Repository) PurgeExpiredTrash(ctx context.Context, olderThan time.Duration) (int64, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	rows, err := tx.Query(ctx,
		`SELECT f.org_id, COALESCE(SUM(v.size_bytes), 0)
		 FROM files f LEFT JOIN file_versions v ON v.file_id = f.id
		 WHERE f.deleted_at IS NOT NULL AND f.deleted_at < now() - ($1 * interval '1 second')
		 GROUP BY f.org_id`, olderThan.Seconds())
	if err != nil {
		return 0, err
	}
	type orgFreed struct {
		orgID string
		bytes int64
	}
	var freed []orgFreed
	for rows.Next() {
		var of orgFreed
		if err := rows.Scan(&of.orgID, &of.bytes); err != nil {
			rows.Close()
			return 0, err
		}
		freed = append(freed, of)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	rows.Close()

	tag, err := tx.Exec(ctx,
		`DELETE FROM files WHERE deleted_at IS NOT NULL AND deleted_at < now() - ($1 * interval '1 second')`,
		olderThan.Seconds())
	if err != nil {
		return 0, err
	}

	for _, of := range freed {
		if _, err = tx.Exec(ctx, `UPDATE organizations SET usage_bytes = usage_bytes - $1 WHERE id = $2`, of.bytes, of.orgID); err != nil {
			return 0, err
		}
	}

	if err = tx.Commit(ctx); err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// CreateWithVersion is the only way a file ever comes into existence — via
// a completed upload (docs/06-api-design.md §5: there is no bare
// POST /v1/files). It creates the file, its first version, and the
// version's chunk list in one transaction, and points latest_version_id at
// it — a file with no version, or a version chunk list out of sync with
// what's committed, should never be observable.
func (r *Repository) CreateWithVersion(ctx context.Context, orgID, folderID, name, createdBy string, sizeBytes int64, checksumSHA256, mimeType string, chunkHashes []string) (fileID, versionID string, err error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return "", "", err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	fileID = idgen.NewUUID()
	if _, err = tx.Exec(ctx,
		`INSERT INTO files (id, org_id, folder_id, name, created_by) VALUES ($1, $2, $3, $4, $5)`,
		fileID, orgID, folderID, name, createdBy,
	); err != nil {
		return "", "", err
	}

	versionID = idgen.NewUUID()
	if _, err = tx.Exec(ctx,
		`INSERT INTO file_versions (id, file_id, size_bytes, checksum_sha256, mime_type, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		versionID, fileID, sizeBytes, checksumSHA256, mimeType, createdBy,
	); err != nil {
		return "", "", err
	}

	for i, hash := range chunkHashes {
		if _, err = tx.Exec(ctx,
			`INSERT INTO file_version_chunks (version_id, sequence, chunk_hash) VALUES ($1, $2, $3)`,
			versionID, i, hash,
		); err != nil {
			return "", "", err
		}
	}

	if _, err = tx.Exec(ctx, `UPDATE files SET latest_version_id = $1 WHERE id = $2`, versionID, fileID); err != nil {
		return "", "", err
	}

	if _, err = tx.Exec(ctx, `UPDATE organizations SET usage_bytes = usage_bytes + $1 WHERE id = $2`, sizeBytes, orgID); err != nil {
		return "", "", err
	}

	if err = tx.Commit(ctx); err != nil {
		return "", "", err
	}
	return fileID, versionID, nil
}

// GetForUpload returns the (org, folder, name) an upload targeting an
// existing file needs to authorize and label itself — the read-side
// counterpart AddVersion's write side needs (docs/09-roadmap.md Day 6:
// re-upload as a new version of an existing file, a gap in the original
// docs/06-api-design.md contract).
func (r *Repository) GetForUpload(ctx context.Context, fileID string) (orgID, folderID, name string, err error) {
	f, err := r.Get(ctx, fileID)
	if err != nil {
		return "", "", "", err
	}
	return f.OrgID, f.FolderID, f.Name, nil
}

// AddVersion creates a new version under an existing file (re-upload), as
// opposed to CreateWithVersion which creates the file too. orgID is the
// caller's already-resolved org (upload.Service.CompleteUpload has it on
// hand as u.OrgID) — needed only to credit the right org's usage_bytes
// counter, not for any authorization check here.
func (r *Repository) AddVersion(ctx context.Context, orgID, fileID, createdBy string, sizeBytes int64, checksumSHA256, mimeType string, chunkHashes []string) (versionID string, err error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	versionID = idgen.NewUUID()
	if _, err = tx.Exec(ctx,
		`INSERT INTO file_versions (id, file_id, size_bytes, checksum_sha256, mime_type, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		versionID, fileID, sizeBytes, checksumSHA256, mimeType, createdBy,
	); err != nil {
		return "", err
	}

	for i, hash := range chunkHashes {
		if _, err = tx.Exec(ctx,
			`INSERT INTO file_version_chunks (version_id, sequence, chunk_hash) VALUES ($1, $2, $3)`,
			versionID, i, hash,
		); err != nil {
			return "", err
		}
	}

	if _, err = tx.Exec(ctx,
		`UPDATE files SET latest_version_id = $1, updated_at = now() WHERE id = $2`, versionID, fileID,
	); err != nil {
		return "", err
	}

	if _, err = tx.Exec(ctx, `UPDATE organizations SET usage_bytes = usage_bytes + $1 WHERE id = $2`, sizeBytes, orgID); err != nil {
		return "", err
	}

	if err = tx.Commit(ctx); err != nil {
		return "", err
	}
	return versionID, nil
}

const versionColumns = `id, file_id, size_bytes, checksum_sha256, mime_type, thumbnail_key, created_by, created_at`

func scanVersion(row pgx.Row) (Version, error) {
	var v Version
	err := row.Scan(&v.ID, &v.FileID, &v.SizeBytes, &v.ChecksumSHA256, &v.MimeType, &v.ThumbnailKey, &v.CreatedBy, &v.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Version{}, ErrVersionNotFound
	}
	return v, err
}

func (r *Repository) ListVersions(ctx context.Context, fileID string) ([]Version, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+versionColumns+` FROM file_versions WHERE file_id = $1 ORDER BY created_at DESC`, fileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Version
	for rows.Next() {
		var v Version
		if err := rows.Scan(&v.ID, &v.FileID, &v.SizeBytes, &v.ChecksumSHA256, &v.MimeType, &v.ThumbnailKey, &v.CreatedBy, &v.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// GetVersion returns versionID, scoped to fileID — a version ID alone
// isn't trusted without confirming it actually belongs to the file the
// caller has already been authorized against.
func (r *Repository) GetVersion(ctx context.Context, fileID, versionID string) (Version, error) {
	return scanVersion(r.pool.QueryRow(ctx,
		`SELECT `+versionColumns+` FROM file_versions WHERE id = $1 AND file_id = $2`, versionID, fileID))
}

func (r *Repository) GetVersionChunks(ctx context.Context, versionID string) ([]VersionChunk, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT sequence, chunk_hash FROM file_version_chunks WHERE version_id = $1 ORDER BY sequence`, versionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []VersionChunk
	for rows.Next() {
		var c VersionChunk
		if err := rows.Scan(&c.Sequence, &c.Hash); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// RestoreVersion repoints latest_version_id at an older version — cheap
// (no bytes duplicated, versions form a simple list not a branching tree),
// per the open question resolved in docs/06-api-design.md §11.
func (r *Repository) RestoreVersion(ctx context.Context, fileID, versionID string) (File, error) {
	return scanFile(r.pool.QueryRow(ctx,
		`UPDATE files SET latest_version_id = $1, updated_at = now()
		 WHERE id = $2 AND deleted_at IS NULL RETURNING `+fileColumns,
		versionID, fileID))
}

// OrgUsageBytes reads organizations.usage_bytes — a counter maintained
// incrementally by every write that changes an org's stored bytes
// (CreateWithVersion, AddVersion, Purge, PurgeExpiredTrash, plus
// folder.Repository.PurgeExpiredTrash's cascade purge), rather than the live
// SUM join across file_versions/files this used to run per call (audit §06:
// a full-table scan on every upload init and complete). The logical-bytes
// semantics are unchanged: trashed files still count (their bytes are only
// freed by purge, and excluding them would make trashing a quota-evasion
// trick); satisfies upload.UsageReader for quota enforcement.
func (r *Repository) OrgUsageBytes(ctx context.Context, orgID string) (int64, error) {
	var total int64
	err := r.pool.QueryRow(ctx,
		`SELECT usage_bytes FROM organizations WHERE id = $1`, orgID).Scan(&total)
	return total, err
}

// OrgFileCounts splits the org's files into live vs trashed, for the
// owner usage view (GET /v1/orgs/{orgId}/usage) — pairs with
// OrgUsageBytes, which deliberately counts both.
func (r *Repository) OrgFileCounts(ctx context.Context, orgID string) (live, trashed int, err error) {
	err = r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FILTER (WHERE deleted_at IS NULL),
		        COUNT(*) FILTER (WHERE deleted_at IS NOT NULL)
		 FROM files WHERE org_id = $1`, orgID).Scan(&live, &trashed)
	return live, trashed, err
}

// SetThumbnailKey records where nimbus-worker stored a generated thumbnail
// (docs/02-system-design.md §6) — called only after the storage write
// itself has already succeeded, so a non-nil thumbnail_key is reliable
// evidence the object actually exists.
func (r *Repository) SetThumbnailKey(ctx context.Context, versionID, key string) error {
	_, err := r.pool.Exec(ctx, `UPDATE file_versions SET thumbnail_key = $1 WHERE id = $2`, key, versionID)
	return err
}
