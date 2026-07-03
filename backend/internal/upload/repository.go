package upload

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"nimbus/internal/platform/idgen"
)

const uniqueViolation = "23505"

const uploadColumns = `id, org_id, folder_id, name, declared_size_bytes, mime_type, created_by, status, idempotency_key, target_file_id, file_id, version_id, created_at`

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func scanUpload(row pgx.Row) (Upload, error) {
	var u Upload
	err := row.Scan(&u.ID, &u.OrgID, &u.FolderID, &u.Name, &u.DeclaredSizeBytes, &u.MimeType,
		&u.CreatedBy, &u.Status, &u.IdempotencyKey, &u.TargetFileID, &u.FileID, &u.VersionID, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Upload{}, ErrUploadNotFound
	}
	return u, err
}

// CreateUpload starts a session. targetFileID, if non-nil, means this
// upload will add a new version to an existing file rather than create one
// (Upload.TargetFileID).
func (r *Repository) CreateUpload(ctx context.Context, orgID, folderID, name string, sizeBytes int64, mimeType, createdBy string, targetFileID *string) (Upload, error) {
	return scanUpload(r.pool.QueryRow(ctx,
		`INSERT INTO uploads (id, org_id, folder_id, name, declared_size_bytes, mime_type, created_by, target_file_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING `+uploadColumns,
		idgen.NewUUID(), orgID, folderID, name, sizeBytes, mimeType, createdBy, targetFileID))
}

func (r *Repository) GetUpload(ctx context.Context, id string) (Upload, error) {
	return scanUpload(r.pool.QueryRow(ctx, `SELECT `+uploadColumns+` FROM uploads WHERE id = $1`, id))
}

// GetByIdempotencyKey returns ErrUploadNotFound if no upload with this key
// exists yet for this user — that's the expected first-call outcome, not
// an error condition for the caller.
func (r *Repository) GetByIdempotencyKey(ctx context.Context, createdBy, key string) (Upload, error) {
	return scanUpload(r.pool.QueryRow(ctx,
		`SELECT `+uploadColumns+` FROM uploads WHERE created_by = $1 AND idempotency_key = $2`,
		createdBy, key))
}

// CheckExistingChunks returns which of hashes already exist in the global
// content-addressed store — the dedup check behind POST /v1/chunks/check.
func (r *Repository) CheckExistingChunks(ctx context.Context, hashes []string) (map[string]bool, error) {
	rows, err := r.pool.Query(ctx, `SELECT hash FROM chunks WHERE hash = ANY($1)`, hashes)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	exists := make(map[string]bool, len(hashes))
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			return nil, err
		}
		exists[h] = true
	}
	return exists, rows.Err()
}

// UpsertChunkAttemptPresigned records (or re-records, on a retried init)
// that hash has been presigned for uploadID.
func (r *Repository) UpsertChunkAttemptPresigned(ctx context.Context, uploadID, hash string) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO upload_chunks (upload_id, chunk_hash, state) VALUES ($1, $2, 'presigned')
		 ON CONFLICT (upload_id, chunk_hash) DO UPDATE SET state = 'presigned', updated_at = now()
		 WHERE upload_chunks.state != 'committed'`,
		uploadID, hash)
	return err
}

func (r *Repository) GetChunkAttemptState(ctx context.Context, uploadID, hash string) (ChunkState, error) {
	var state ChunkState
	err := r.pool.QueryRow(ctx,
		`SELECT state FROM upload_chunks WHERE upload_id = $1 AND chunk_hash = $2`, uploadID, hash,
	).Scan(&state)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrChunkNotFound
	}
	return state, err
}

// MarkChunkCommitted transitions a chunk attempt from presigned to
// committed. Returns ErrInvalidState if it wasn't in presigned state
// (LLD §2's server-enforced state machine — commits aren't trusted blindly
// from the client).
func (r *Repository) MarkChunkCommitted(ctx context.Context, uploadID, hash string, etags map[string]string) error {
	etagsJSON, err := json.Marshal(etags)
	if err != nil {
		return err
	}
	tag, err := r.pool.Exec(ctx,
		`UPDATE upload_chunks SET state = 'committed', etags = $3, updated_at = now()
		 WHERE upload_id = $1 AND chunk_hash = $2 AND state = 'presigned'`,
		uploadID, hash, etagsJSON)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrInvalidState
	}
	return nil
}

func (r *Repository) UpsertGlobalChunk(ctx context.Context, hash string, sizeBytes int64) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO chunks (hash, size_bytes) VALUES ($1, $2) ON CONFLICT (hash) DO NOTHING`,
		hash, sizeBytes)
	return err
}

func (r *Repository) UpsertChunkLocation(ctx context.Context, hash, nodeID string) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO chunk_locations (chunk_hash, node_id, status) VALUES ($1, $2, 'committed')
		 ON CONFLICT (chunk_hash, node_id) DO UPDATE SET status = 'committed'`,
		hash, nodeID)
	return err
}

// FindMissingChunks returns whichever of hashes do NOT exist in the global
// chunk store — used to validate chunk_order at /complete. An empty result
// means every hash is accounted for, whether committed in this upload or
// deduped from a prior one.
func (r *Repository) FindMissingChunks(ctx context.Context, hashes []string) ([]string, error) {
	exists, err := r.CheckExistingChunks(ctx, hashes)
	if err != nil {
		return nil, err
	}
	// Non-nil even when empty: a nil slice marshals to JSON `null`, not
	// `[]`, which broke naive client-side `.length` checks on an
	// all-deduped response.
	missing := []string{}
	for _, h := range hashes {
		if !exists[h] {
			missing = append(missing, h)
		}
	}
	return missing, nil
}

// CompleteUpload marks uploadID completed and links it to the newly
// created file/version, using a compare-and-swap on status = 'in_progress'
// so that two concurrent /complete calls for the same upload can't both
// "win". The loser gets ErrAlreadyCompleting and (per Service.CompleteUpload)
// returns the winner's result instead of erroring — the one case this
// doesn't fully close is a genuinely simultaneous double-complete creating
// an orphaned file/version row from the loser's already-committed
// CreateWithVersion call; acceptable at this project's single-actor demo
// scale rather than adding distributed locking for it.
func (r *Repository) CompleteUpload(ctx context.Context, uploadID, fileID, versionID string, idempotencyKey *string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE uploads SET status = 'completed', file_id = $2, version_id = $3, idempotency_key = $4
		 WHERE id = $1 AND status = 'in_progress'`,
		uploadID, fileID, versionID, idempotencyKey)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
			return ErrAlreadyCompleting
		}
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrAlreadyCompleting
	}
	return nil
}
