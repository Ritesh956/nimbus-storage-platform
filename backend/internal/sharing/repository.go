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

const shareColumns = `id, file_id, token, created_by, expires_at, created_at`

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func scanShare(row pgx.Row) (ShareLink, error) {
	var s ShareLink
	err := row.Scan(&s.ID, &s.FileID, &s.Token, &s.CreatedBy, &s.ExpiresAt, &s.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ShareLink{}, ErrNotFound
	}
	return s, err
}

func (r *Repository) Create(ctx context.Context, fileID, createdBy string, expiresAt *time.Time) (ShareLink, error) {
	return scanShare(r.pool.QueryRow(ctx,
		`INSERT INTO share_links (id, file_id, token, created_by, expires_at) VALUES ($1, $2, $3, $4, $5)
		 RETURNING `+shareColumns,
		idgen.NewUUID(), fileID, newToken(), createdBy, expiresAt))
}

func (r *Repository) GetByToken(ctx context.Context, token string) (ShareLink, error) {
	return scanShare(r.pool.QueryRow(ctx, `SELECT `+shareColumns+` FROM share_links WHERE token = $1`, token))
}

func (r *Repository) Delete(ctx context.Context, token string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM share_links WHERE token = $1`, token)
	return err
}

// newToken produces a 43-char base64url string from 32 random bytes,
// matching the share_links.token char(43) column (docs/05-database-design.md).
func newToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
