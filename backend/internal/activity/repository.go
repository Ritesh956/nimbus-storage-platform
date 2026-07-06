package activity

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"
	"time"

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

// ActorStat is one member's slice of the org's activity, for the owner
// usage view (GET /v1/orgs/{orgId}/usage).
type ActorStat struct {
	EventsSince  int
	LastActiveAt time.Time
}

// ActorStats aggregates per-actor counts since `since` and each actor's
// all-time latest event in this org, keyed by user ID. Events with a NULL
// actor (system-derived) are excluded — they aren't attributable to a
// member.
func (r *Repository) ActorStats(ctx context.Context, orgID string, since time.Time) (map[string]ActorStat, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT actor_user_id, COUNT(*) FILTER (WHERE created_at >= $2), MAX(created_at)
		 FROM activity_events
		 WHERE org_id = $1 AND actor_user_id IS NOT NULL
		 GROUP BY actor_user_id`, orgID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]ActorStat)
	for rows.Next() {
		var userID string
		var s ActorStat
		if err := rows.Scan(&userID, &s.EventsSince, &s.LastActiveAt); err != nil {
			return nil, err
		}
		out[userID] = s
	}
	return out, rows.Err()
}

// VerbCounts is the org-wide event breakdown by verb since `since`.
func (r *Repository) VerbCounts(ctx context.Context, orgID string, since time.Time) (map[string]int, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT verb, COUNT(*) FROM activity_events
		 WHERE org_id = $1 AND created_at >= $2 GROUP BY verb`, orgID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]int)
	for rows.Next() {
		var verb string
		var n int
		if err := rows.Scan(&verb, &n); err != nil {
			return nil, err
		}
		out[verb] = n
	}
	return out, rows.Err()
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
