package search

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// Search queries files by org plus whichever filters are set, joined
// against each file's latest version for type/size (docs/06-api-design.md
// §8). files.id is a UUID (not naturally ordered), so keyset pagination
// needs the composite (created_at, id) cursor built below, unlike
// activity's simpler bigserial-id cursor.
func (r *Repository) Search(ctx context.Context, orgID string, f Filters) ([]Result, string, error) {
	var cursorTime *time.Time
	var cursorID string
	if f.Cursor != "" {
		t, id, err := decodeCursor(f.Cursor)
		if err != nil {
			return nil, "", err
		}
		cursorTime, cursorID = &t, id
	}

	rows, err := r.pool.Query(ctx,
		`SELECT f.id, f.name, f.folder_id, f.created_by, f.created_at, fv.size_bytes, fv.mime_type
		 FROM files f
		 LEFT JOIN file_versions fv ON fv.id = f.latest_version_id
		 WHERE f.org_id = $1 AND f.deleted_at IS NULL
		   AND ($2 = '' OR f.name_tsv @@ plainto_tsquery('simple', regexp_replace($2, '[^a-zA-Z0-9]+', ' ', 'g')))
		   AND ($3 = '' OR fv.mime_type ILIKE $3 || '%')
		   AND ($4 = '' OR f.created_by = $4::uuid)
		   AND ($5::timestamptz IS NULL OR f.created_at >= $5)
		   AND ($6::timestamptz IS NULL OR f.created_at <= $6)
		   AND ($7::bigint IS NULL OR fv.size_bytes >= $7)
		   AND ($8::bigint IS NULL OR fv.size_bytes <= $8)
		   AND ($9::timestamptz IS NULL OR (f.created_at, f.id) < ($9, $10::uuid))
		 ORDER BY f.created_at DESC, f.id DESC
		 LIMIT $11`,
		orgID, f.Query, f.Type, f.OwnerID, f.DateFrom, f.DateTo, f.SizeMin, f.SizeMax,
		cursorTime, nullableUUID(cursorID), f.Limit,
	)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()

	var out []Result
	for rows.Next() {
		var res Result
		if err := rows.Scan(&res.FileID, &res.Name, &res.FolderID, &res.OwnerID, &res.CreatedAt, &res.SizeBytes, &res.MimeType); err != nil {
			return nil, "", err
		}
		out = append(out, res)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}

	var next string
	if len(out) == f.Limit {
		last := out[len(out)-1]
		next = encodeCursor(last.CreatedAt, last.FileID)
	}
	return out, next, nil
}

// nullableUUID lets the query's ($10::uuid) placeholder bind cleanly when
// there's no cursor yet (empty string cast to uuid would error; NULL is
// fine and the $9 IS NULL guard short-circuits before $10 is evaluated).
func nullableUUID(id string) any {
	if id == "" {
		return nil
	}
	return id
}

func encodeCursor(t time.Time, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(t.Format(time.RFC3339Nano) + "|" + id))
}

func decodeCursor(cursor string) (time.Time, string, error) {
	b, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, "", fmt.Errorf("invalid cursor: %w", err)
	}
	parts := strings.SplitN(string(b), "|", 2)
	if len(parts) != 2 {
		return time.Time{}, "", fmt.Errorf("invalid cursor")
	}
	t, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return time.Time{}, "", fmt.Errorf("invalid cursor: %w", err)
	}
	return t, parts[1], nil
}
