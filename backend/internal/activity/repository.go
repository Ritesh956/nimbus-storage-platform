package activity

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) Record(ctx context.Context, orgID string, actorUserID *string, verb, targetType, targetID string) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO activity_events (org_id, actor_user_id, verb, target_type, target_id) VALUES ($1, $2, $3, $4, $5)`,
		orgID, actorUserID, verb, targetType, targetID)
	return err
}

// List returns events newest-first, keyset-paginated on the bigserial id
// (monotonically increasing with insertion order, so no composite cursor
// is needed the way search's UUID-keyed table requires).
func (r *Repository) List(ctx context.Context, orgID, cursor string, limit int) ([]Event, string, error) {
	var beforeID int64 = 1<<63 - 1 // no cursor = start from the newest
	if cursor != "" {
		decoded, err := decodeCursor(cursor)
		if err != nil {
			return nil, "", err
		}
		beforeID = decoded
	}

	rows, err := r.pool.Query(ctx,
		`SELECT id, org_id, actor_user_id, verb, target_type, target_id, created_at
		 FROM activity_events WHERE org_id = $1 AND id < $2
		 ORDER BY id DESC LIMIT $3`,
		orgID, beforeID, limit)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()

	var out []Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.OrgID, &e.ActorUserID, &e.Verb, &e.TargetType, &e.TargetID, &e.CreatedAt); err != nil {
			return nil, "", err
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}

	var next string
	if len(out) == limit {
		next = encodeCursor(out[len(out)-1].ID)
	}
	return out, next, nil
}

func encodeCursor(id int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(id, 10)))
}

func decodeCursor(cursor string) (int64, error) {
	b, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0, fmt.Errorf("invalid cursor: %w", err)
	}
	id, err := strconv.ParseInt(string(b), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid cursor: %w", err)
	}
	return id, nil
}
