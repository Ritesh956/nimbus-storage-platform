package folder

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"nimbus/internal/platform/idgen"
)

const (
	uniqueViolation     = "23505"
	foreignKeyViolation = "23503"
)

const folderColumns = `id, org_id, parent_id, name, created_at, updated_at, deleted_at`

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func scanFolder(row pgx.Row) (Folder, error) {
	var f Folder
	err := row.Scan(&f.ID, &f.OrgID, &f.ParentID, &f.Name, &f.CreatedAt, &f.UpdatedAt, &f.DeletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Folder{}, ErrNotFound
	}
	return f, err
}

func asConflictOrParentErr(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case uniqueViolation:
			return ErrNameConflict
		case foreignKeyViolation:
			return ErrInvalidParent
		}
	}
	return err
}

func (r *Repository) Create(ctx context.Context, orgID string, parentID *string, name string) (Folder, error) {
	f, err := scanFolder(r.pool.QueryRow(ctx,
		`INSERT INTO folders (id, org_id, parent_id, name) VALUES ($1, $2, $3, $4)
		 RETURNING `+folderColumns,
		idgen.NewUUID(), orgID, parentID, name,
	))
	if err != nil {
		return Folder{}, asConflictOrParentErr(err)
	}
	return f, nil
}

// CreateRoot satisfies org.FolderCreator exactly (same no-adapter-needed
// pattern as upload.FileCreator/file.Repository) — a thin wrapper since
// Create already supports parentID == nil for "root".
func (r *Repository) CreateRoot(ctx context.Context, orgID, name string) error {
	_, err := r.Create(ctx, orgID, nil, name)
	return err
}

func (r *Repository) Get(ctx context.Context, id string) (Folder, error) {
	return scanFolder(r.pool.QueryRow(ctx,
		`SELECT `+folderColumns+` FROM folders WHERE id = $1 AND deleted_at IS NULL`, id))
}

// GetAny loads a folder regardless of trash state — used by Restore, which
// by definition targets an already-deleted folder.
func (r *Repository) GetAny(ctx context.Context, id string) (Folder, error) {
	return scanFolder(r.pool.QueryRow(ctx,
		`SELECT `+folderColumns+` FROM folders WHERE id = $1`, id))
}

func (r *Repository) ListChildren(ctx context.Context, orgID string, parentID *string) ([]Folder, error) {
	var rows pgx.Rows
	var err error
	if parentID == nil {
		rows, err = r.pool.Query(ctx,
			`SELECT `+folderColumns+` FROM folders
			 WHERE org_id = $1 AND parent_id IS NULL AND deleted_at IS NULL ORDER BY name`, orgID)
	} else {
		rows, err = r.pool.Query(ctx,
			`SELECT `+folderColumns+` FROM folders
			 WHERE org_id = $1 AND parent_id = $2 AND deleted_at IS NULL ORDER BY name`, orgID, *parentID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Folder
	for rows.Next() {
		var f Folder
		if err := rows.Scan(&f.ID, &f.OrgID, &f.ParentID, &f.Name, &f.CreatedAt, &f.UpdatedAt, &f.DeletedAt); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// ListTrashed serves the trash UI (docs/09-roadmap.md Day 10's FR-25) —
// the folder-side counterpart of file.Repository.ListTrashed; see that
// method's comment for why this didn't exist until now.
func (r *Repository) ListTrashed(ctx context.Context, orgID string) ([]Folder, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+folderColumns+` FROM folders WHERE org_id = $1 AND deleted_at IS NOT NULL ORDER BY updated_at DESC`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Folder
	for rows.Next() {
		var f Folder
		if err := rows.Scan(&f.ID, &f.OrgID, &f.ParentID, &f.Name, &f.CreatedAt, &f.UpdatedAt, &f.DeletedAt); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// Update applies a rename and/or move. name == nil leaves the name
// unchanged. parentID == nil leaves the parent unchanged; a non-nil
// pointer to a nil *string moves the folder to root; a non-nil pointer to
// a non-nil *string moves it under that parent.
func (r *Repository) Update(ctx context.Context, id string, name *string, parentID **string) (Folder, error) {
	var row pgx.Row
	switch {
	case name != nil && parentID != nil:
		row = r.pool.QueryRow(ctx,
			`UPDATE folders SET name = $1, parent_id = $2, updated_at = now()
			 WHERE id = $3 AND deleted_at IS NULL RETURNING `+folderColumns,
			*name, *parentID, id)
	case name != nil:
		row = r.pool.QueryRow(ctx,
			`UPDATE folders SET name = $1, updated_at = now()
			 WHERE id = $2 AND deleted_at IS NULL RETURNING `+folderColumns,
			*name, id)
	case parentID != nil:
		row = r.pool.QueryRow(ctx,
			`UPDATE folders SET parent_id = $1, updated_at = now()
			 WHERE id = $2 AND deleted_at IS NULL RETURNING `+folderColumns,
			*parentID, id)
	default:
		return r.Get(ctx, id)
	}

	f, err := scanFolder(row)
	if err != nil {
		return Folder{}, asConflictOrParentErr(err)
	}
	return f, nil
}

// Ancestors returns the chain from the root folder down to folderID
// itself (inclusive), for breadcrumb rendering. No deleted_at filter: the
// caller was already authorized against the (live) folder itself, and a
// trashed ancestor left behind by an independent restore shouldn't blow a
// hole in the trail.
func (r *Repository) Ancestors(ctx context.Context, folderID string) ([]PathEntry, error) {
	rows, err := r.pool.Query(ctx,
		`WITH RECURSIVE chain AS (
			SELECT id, parent_id, name, 0 AS depth FROM folders WHERE id = $1
			UNION ALL
			SELECT f.id, f.parent_id, f.name, c.depth + 1 FROM folders f JOIN chain c ON f.id = c.parent_id
		 )
		 SELECT id, name FROM chain ORDER BY depth DESC`, folderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []PathEntry
	for rows.Next() {
		var e PathEntry
		if err := rows.Scan(&e.ID, &e.Name); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// IsSelfOrDescendant reports whether candidateID equals folderID or is a
// descendant of it — used to reject a move that would create a cycle.
func (r *Repository) IsSelfOrDescendant(ctx context.Context, folderID, candidateID string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx,
		`WITH RECURSIVE subtree AS (
			SELECT id FROM folders WHERE id = $1
			UNION ALL
			SELECT f.id FROM folders f JOIN subtree s ON f.parent_id = s.id
		 )
		 SELECT EXISTS (SELECT 1 FROM subtree WHERE id = $2)`,
		folderID, candidateID,
	).Scan(&exists)
	return exists, err
}

const subtreeCTE = `WITH RECURSIVE subtree AS (
	SELECT id FROM folders WHERE id = $1
	UNION ALL
	SELECT f.id FROM folders f JOIN subtree s ON f.parent_id = s.id
)`

// SoftDeleteCascade trashes the folder and every descendant folder/file
// (docs/06-api-design.md §4: DELETE .../folders cascades to children).
func (r *Repository) SoftDeleteCascade(ctx context.Context, folderID string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if _, err = tx.Exec(ctx, subtreeCTE+`
		UPDATE folders SET deleted_at = now(), updated_at = now()
		WHERE id IN (SELECT id FROM subtree) AND deleted_at IS NULL`, folderID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, subtreeCTE+`
		UPDATE files SET deleted_at = now(), updated_at = now()
		WHERE folder_id IN (SELECT id FROM subtree) AND deleted_at IS NULL`, folderID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// RestoreCascade is the symmetric undo. Simplification: it restores every
// currently-trashed descendant, which could over-restore something trashed
// independently before this folder was — acceptable at this project's
// scope (see docs/09-roadmap.md non-goals), not worth a "trashed together"
// marker for a single-actor demo system.
func (r *Repository) RestoreCascade(ctx context.Context, folderID string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if _, err = tx.Exec(ctx, subtreeCTE+`
		UPDATE folders SET deleted_at = NULL, updated_at = now()
		WHERE id IN (SELECT id FROM subtree) AND deleted_at IS NOT NULL`, folderID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, subtreeCTE+`
		UPDATE files SET deleted_at = NULL, updated_at = now()
		WHERE folder_id IN (SELECT id FROM subtree) AND deleted_at IS NOT NULL`, folderID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// PurgeExpiredTrash hard-deletes folders trashed longer ago than olderThan —
// FR-11's retention window, called from nimbus-worker's GC tick
// (gc.TrashPurger). Deleting a folders row CASCADEs to its whole subtree
// (descendant folders and files, whatever their own deleted_at), so each
// candidate is guarded: skipped if its subtree still contains anything live
// (e.g. a file individually restored after the folder was trashed — restore
// doesn't check ancestors). Trashed-but-unexpired descendants do go down
// with an expired ancestor; they've been unreachable inside it since it was
// trashed, so the ancestor's clock is the one that counts.
func (r *Repository) PurgeExpiredTrash(ctx context.Context, olderThan time.Duration) (int64, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id FROM folders
		 WHERE deleted_at IS NOT NULL AND deleted_at < now() - ($1 * interval '1 second')`,
		olderThan.Seconds())
	if err != nil {
		return 0, err
	}
	var candidates []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		candidates = append(candidates, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	var purged int64
	for _, id := range candidates {
		// The guard and delete are one statement, so nothing restored
		// between them can be lost. A candidate inside another candidate's
		// subtree just vanishes first via the ancestor's cascade — its own
		// delete then matches zero rows, which is fine.
		tag, err := r.pool.Exec(ctx, subtreeCTE+`
			DELETE FROM folders WHERE id = $1
			  AND NOT EXISTS (
				SELECT 1 FROM folders f JOIN subtree s ON f.id = s.id WHERE f.deleted_at IS NULL
			  )
			  AND NOT EXISTS (
				SELECT 1 FROM files fi JOIN subtree s ON fi.folder_id = s.id WHERE fi.deleted_at IS NULL
			  )`, id)
		if err != nil {
			return purged, err
		}
		purged += tag.RowsAffected()
	}
	return purged, nil
}
