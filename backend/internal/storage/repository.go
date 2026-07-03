package storage

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// UpsertNode registers a node (or updates its endpoint if already known).
// Called at startup for the configured node set, and would be called again
// for any runtime admin-triggered registration (docs/07-distributed-architecture.md §1).
func (r *Repository) UpsertNode(ctx context.Context, id, endpoint string) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO storage_nodes (id, endpoint) VALUES ($1, $2)
		 ON CONFLICT (id) DO UPDATE SET endpoint = excluded.endpoint`,
		id, endpoint)
	return err
}

func (r *Repository) SetHealthy(ctx context.Context, id string, at time.Time) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE storage_nodes SET status = 'healthy', last_heartbeat_at = $2 WHERE id = $1`, id, at)
	return err
}

func (r *Repository) SetDown(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `UPDATE storage_nodes SET status = 'down' WHERE id = $1`, id)
	return err
}

type NodeRecord struct {
	ID              string
	Endpoint        string
	Status          string
	LastHeartbeatAt *time.Time
}

// ListNodes is the read model behind GET /v1/admin/nodes
// (docs/06-api-design.md §9) — reads from Postgres, the durable record,
// rather than Redis or in-process state.
func (r *Repository) ListNodes(ctx context.Context) ([]NodeRecord, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, endpoint, status, last_heartbeat_at FROM storage_nodes ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []NodeRecord
	for rows.Next() {
		var n NodeRecord
		if err := rows.Scan(&n.ID, &n.Endpoint, &n.Status, &n.LastHeartbeatAt); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// LocationsForChunk returns the nodes a chunk was actually committed to
// (docs/02-system-design.md §2.4: reads use the recorded chunk_locations,
// not a fresh ring placement — Resolve is for new writes, this is for
// "where did this content really land").
func (r *Repository) LocationsForChunk(ctx context.Context, chunkHash string) ([]NodeID, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT node_id FROM chunk_locations WHERE chunk_hash = $1 AND status = 'committed'`, chunkHash)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []NodeID
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, NodeID(id))
	}
	return out, rows.Err()
}
