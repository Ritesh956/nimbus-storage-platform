package events

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"

	"nimbus/internal/platform/idgen"
)

// DeadEvent is one permanently-failed delivery, recorded by the worker
// when a message exhausts maxDeliver (see Subscribe) and surfaced via the
// admin DLQ endpoints for inspection and retry.
type DeadEvent struct {
	ID         string
	Subject    string
	Payload    json.RawMessage
	Error      string
	Deliveries int
	Status     string // "dead" | "retried"
	CreatedAt  time.Time
	RetriedAt  *time.Time
}

var ErrDeadEventNotFound = errors.New("dead event not found")

// Repository persists dead events. It lives in this package (not a domain
// module) because a dead letter is eventing infrastructure — the payload is
// opaque here, keyed only by subject.
type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) InsertDeadEvent(ctx context.Context, subject string, payload []byte, errMsg string, deliveries int) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO dead_events (id, subject, payload, error, deliveries) VALUES ($1, $2, $3, $4, $5)`,
		idgen.NewUUID(), subject, payload, errMsg, deliveries)
	return err
}

func (r *Repository) ListDeadEvents(ctx context.Context) ([]DeadEvent, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, subject, payload, error, deliveries, status, created_at, retried_at
		 FROM dead_events ORDER BY created_at DESC LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []DeadEvent
	for rows.Next() {
		var e DeadEvent
		if err := rows.Scan(&e.ID, &e.Subject, &e.Payload, &e.Error, &e.Deliveries, &e.Status, &e.CreatedAt, &e.RetriedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (r *Repository) GetDeadEvent(ctx context.Context, id string) (DeadEvent, error) {
	var e DeadEvent
	err := r.pool.QueryRow(ctx,
		`SELECT id, subject, payload, error, deliveries, status, created_at, retried_at
		 FROM dead_events WHERE id = $1`, id).
		Scan(&e.ID, &e.Subject, &e.Payload, &e.Error, &e.Deliveries, &e.Status, &e.CreatedAt, &e.RetriedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return DeadEvent{}, ErrDeadEventNotFound
	}
	return e, err
}

// MarkRetried flips a dead event to retried — only from 'dead', so a
// double-click can't republish twice.
func (r *Repository) MarkRetried(ctx context.Context, id string) (bool, error) {
	tag, err := r.pool.Exec(ctx,
		`UPDATE dead_events SET status = 'retried', retried_at = now() WHERE id = $1 AND status = 'dead'`, id)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// RevertRetry undoes MarkRetried when the republish that justified it
// failed — keeps the row retryable instead of stranded (see DLQHandler.Retry).
func (r *Repository) RevertRetry(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE dead_events SET status = 'dead', retried_at = NULL WHERE id = $1`, id)
	return err
}

// RegisterMetrics wires nimbus_dlq_dead_events, a live gauge read straight
// from Postgres at scrape time rather than an in-process counter — dead
// events are inserted by nimbus-worker but resolved (retried) via
// nimbus-api's admin endpoint, so an in-process gauge split by process (like
// StorageNodeHealthy) would go negative the moment a retry lands on a
// process that never saw the matching insert. A scrape-time DB read is
// always exact and needs no background goroutine or cross-process state
// (audit roadmap item #9, quick-wins session — the alert rule itself lives
// in Grafana's provisioned rule group, see
// deploy/observability/grafana/provisioning/alerting/).
func (r *Repository) RegisterMetrics(logger *slog.Logger) {
	prometheus.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "nimbus_dlq_dead_events",
		Help: "Current count of dead_events rows with status='dead' — exhausted NATS redelivery, awaiting operator retry.",
	}, func() float64 {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		var n float64
		if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM dead_events WHERE status = 'dead'`).Scan(&n); err != nil {
			logger.Warn("dlq metrics: count query failed, reporting 0", "error", err)
			return 0
		}
		return n
	}))
}
