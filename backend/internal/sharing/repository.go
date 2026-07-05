package sharing

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"nimbus/internal/platform/idgen"
)

const shareColumns = `id, org_id, file_id, folder_id, token, created_by, expires_at, created_at`

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func scanShare(row pgx.Row) (ShareLink, error) {
	var s ShareLink
	err := row.Scan(&s.ID, &s.OrgID, &s.FileID, &s.FolderID, &s.Token, &s.CreatedBy, &s.ExpiresAt, &s.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ShareLink{}, ErrNotFound
	}
	return s, err
}

func (r *Repository) CreateFileShare(ctx context.Context, orgID, fileID, createdBy string, expiresAt *time.Time) (ShareLink, error) {
	return scanShare(r.pool.QueryRow(ctx,
		`INSERT INTO share_links (id, org_id, file_id, token, created_by, expires_at) VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING `+shareColumns,
		idgen.NewUUID(), orgID, fileID, newToken(), createdBy, expiresAt))
}

func (r *Repository) CreateFolderShare(ctx context.Context, orgID, folderID, createdBy string, expiresAt *time.Time) (ShareLink, error) {
	return scanShare(r.pool.QueryRow(ctx,
		`INSERT INTO share_links (id, org_id, folder_id, token, created_by, expires_at) VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING `+shareColumns,
		idgen.NewUUID(), orgID, folderID, newToken(), createdBy, expiresAt))
}

// CreateBundleShare inserts the link and its member rows in one
// transaction — a bundle link without members would resolve as empty.
func (r *Repository) CreateBundleShare(ctx context.Context, orgID string, fileIDs []string, createdBy string, expiresAt *time.Time) (ShareLink, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return ShareLink{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	link, err := scanShare(tx.QueryRow(ctx,
		`INSERT INTO share_links (id, org_id, token, created_by, expires_at) VALUES ($1, $2, $3, $4, $5)
		 RETURNING `+shareColumns,
		idgen.NewUUID(), orgID, newToken(), createdBy, expiresAt))
	if err != nil {
		return ShareLink{}, err
	}
	for _, fileID := range fileIDs {
		if _, err := tx.Exec(ctx,
			`INSERT INTO share_link_files (share_id, file_id) VALUES ($1, $2)`, link.ID, fileID); err != nil {
			return ShareLink{}, err
		}
	}
	return link, tx.Commit(ctx)
}

func (r *Repository) GetByToken(ctx context.Context, token string) (ShareLink, error) {
	return scanShare(r.pool.QueryRow(ctx, `SELECT `+shareColumns+` FROM share_links WHERE token = $1`, token))
}

func (r *Repository) Delete(ctx context.Context, token string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM share_links WHERE token = $1`, token)
	return err
}

// BundleFileIDs lists a bundle link's member file IDs. Purged files vanish
// from here via the ON DELETE CASCADE on share_link_files.file_id.
func (r *Repository) BundleFileIDs(ctx context.Context, shareID string) ([]string, error) {
	rows, err := r.pool.Query(ctx, `SELECT file_id FROM share_link_files WHERE share_id = $1`, shareID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (r *Repository) IsBundleMember(ctx context.Context, shareID, fileID string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM share_link_files WHERE share_id = $1 AND file_id = $2)`,
		shareID, fileID).Scan(&exists)
	return exists, err
}

// newToken produces a 43-char base64url string from 32 random bytes,
// matching the share_links.token char(43) column (docs/05-database-design.md).
func newToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
