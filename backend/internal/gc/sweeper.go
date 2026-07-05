// Package gc is nimbus-worker's chunk garbage collector (post-v1 backlog
// #10, docs/07-distributed-architecture.md §6). Content-addressed dedup
// means deleting a file never frees storage — purging a file removes its
// file_version_chunks references but leaves the chunks row and the MinIO
// objects behind forever. The sweeper reclaims them in two phases:
//
//	mark:  chunks referenced by no file version, pinned by no in-progress
//	       upload session, and unseen by any dedup check for GCGrace are
//	       flipped live -> doomed. Doomed chunks read as *missing* to the
//	       dedup check (upload.Repository.CheckExistingChunks), so any new
//	       upload of the same content re-sends the bytes and its commit
//	       resurrects the chunk (UpsertGlobalChunk's conflict arm).
//	sweep: chunks doomed for a further GCGrace are deleted for real — MinIO
//	       objects first, then the chunks row — inside a per-chunk
//	       transaction that re-verifies doomed-and-unreferenced under
//	       FOR UPDATE. A replica whose node is down aborts that chunk's
//	       transaction; the doomed row is the durable work record and the
//	       next tick retries (object deletion is idempotent).
//
// The refcount is computed (NOT EXISTS against file_version_chunks), not a
// stored counter: version rows are deleted by ON DELETE CASCADE from file
// purge, so a stored count would need triggers to stay honest, while the
// computed probe is one indexed lookup and can't drift.
//
// This package deliberately queries file_version_chunks and upload_chunks —
// other modules' tables — directly, an explicit exception to the
// no-cross-module-SQL convention (CLAUDE.md): reference counting is
// definitionally cross-cutting, and the whole point of the mark/sweep
// queries is that each decision is a single atomic statement. Decomposing
// them into per-module round trips would reintroduce exactly the
// check-then-act races the transactions exist to close.
//
// Residual race, documented rather than hidden: MinIO object writes aren't
// coordinated through Postgres, so a client PUT landing between the sweep's
// in-transaction re-verify and its object deletion can still lose bytes. In
// practice that requires a chunk nobody referenced or checked for a full
// GCGrace, then doomed for another GCGrace, to be re-uploaded during the
// milliseconds the sweeper spends deleting it — and the commit's row upsert
// serializes against the sweep's row lock, so the DB never disagrees with
// itself about whether the chunk exists. Closing it fully would need
// generation-versioned object keys; out of scope for v1, per docs/07 §6.
package gc

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"nimbus/internal/platform/metrics"
	"nimbus/internal/storage"
)

// uploadPinTTL bounds how long an in-progress upload session pins its
// committed chunks against the mark phase. Without a cutoff, an abandoned
// session (status stuck 'in_progress' forever — nothing expires uploads)
// would pin its chunks eternally, which is the exact leak GC exists to fix.
// 24h is far beyond any plausible real session.
const uploadPinTTL = 24 * time.Hour

// TrashPurger hard-deletes rows trashed longer ago than olderThan — the
// port file.Repository and folder.Repository satisfy for FR-11's
// retention-window purge. Trash purge runs first in each GC tick because
// it's what frees file_version_chunks references for the mark phase.
type TrashPurger interface {
	PurgeExpiredTrash(ctx context.Context, olderThan time.Duration) (int64, error)
}

type Sweeper struct {
	pool     *pgxpool.Pool
	router   *storage.Router
	interval time.Duration
	grace    time.Duration
	// trashRetention <= 0 disables trash auto-purge; files before folders,
	// so an expired folder's guard never sees expired-but-still-present
	// files as a reason to keep the folder around another tick.
	trashRetention time.Duration
	files          TrashPurger
	folders        TrashPurger
	logger         *slog.Logger
}

func NewSweeper(pool *pgxpool.Pool, router *storage.Router, interval, grace, trashRetention time.Duration, files, folders TrashPurger, logger *slog.Logger) *Sweeper {
	return &Sweeper{pool: pool, router: router, interval: interval, grace: grace, trashRetention: trashRetention, files: files, folders: folders, logger: logger}
}

// Run ticks trash-purge+mark+sweep every interval until ctx is cancelled.
func (s *Sweeper) Run(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

func (s *Sweeper) tick(ctx context.Context) {
	if s.trashRetention > 0 {
		s.purgeTrash(ctx)
	}
	doomed, err := s.mark(ctx)
	if err != nil {
		s.logger.Error("gc mark phase failed", "error", err)
	} else if doomed > 0 {
		s.logger.Info("gc mark phase doomed chunks", "count", doomed)
	}
	if err := s.sweep(ctx); err != nil {
		s.logger.Error("gc sweep phase failed", "error", err)
	}
}

func (s *Sweeper) purgeTrash(ctx context.Context) {
	files, err := s.files.PurgeExpiredTrash(ctx, s.trashRetention)
	if err != nil {
		s.logger.Error("trash auto-purge (files) failed", "error", err)
	}
	folders, err := s.folders.PurgeExpiredTrash(ctx, s.trashRetention)
	if err != nil {
		s.logger.Error("trash auto-purge (folders) failed", "error", err)
	}
	if files > 0 || folders > 0 {
		s.logger.Info("trash auto-purge removed expired items", "files", files, "folders", folders, "retention", s.trashRetention)
	}
}

// mark dooms every live chunk that is (a) referenced by no file version,
// (b) not pinned by a recent in-progress upload session's chunk attempts,
// and (c) unseen by any dedup check for the grace window. One statement:
// the candidate test and the transition are atomic per row.
func (s *Sweeper) mark(ctx context.Context) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE chunks c SET gc_state = 'doomed', doomed_at = now()
		WHERE c.gc_state = 'live'
		  AND c.last_seen_at < now() - ($1 * interval '1 second')
		  AND NOT EXISTS (
			SELECT 1 FROM file_version_chunks fvc WHERE fvc.chunk_hash = c.hash
		  )
		  AND NOT EXISTS (
			SELECT 1 FROM upload_chunks uc
			JOIN uploads u ON u.id = uc.upload_id
			WHERE uc.chunk_hash = c.hash
			  AND u.status = 'in_progress'
			  AND u.created_at > now() - ($2 * interval '1 second')
		  )`,
		s.grace.Seconds(), uploadPinTTL.Seconds())
	if err != nil {
		return 0, err
	}
	metrics.GCChunksDoomedTotal.Add(float64(tag.RowsAffected()))
	return tag.RowsAffected(), nil
}

// sweep physically deletes chunks that have sat doomed for a further grace
// window. Candidates are listed outside any transaction; each chunk is then
// reaped in its own small transaction so one stuck chunk (down node) never
// blocks the rest.
func (s *Sweeper) sweep(ctx context.Context) error {
	rows, err := s.pool.Query(ctx,
		`SELECT hash FROM chunks
		 WHERE gc_state = 'doomed' AND doomed_at < now() - ($1 * interval '1 second')`,
		s.grace.Seconds())
	if err != nil {
		return err
	}
	candidates := []string{}
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			rows.Close()
			return err
		}
		candidates = append(candidates, h)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	for _, hash := range candidates {
		if err := s.reapChunk(ctx, hash); err != nil {
			metrics.GCSweepFailuresTotal.Inc()
			s.logger.Warn("gc sweep: chunk not reaped, will retry next tick", "chunk_hash", hash, "error", err)
		}
	}
	return nil
}

// reapChunk deletes one doomed chunk: re-verify under a row lock, delete
// the MinIO objects from every recorded location, then delete the chunks
// row (chunk_locations cascades). The row lock serializes against a
// concurrent commit's resurrection upsert; the FK from file_version_chunks
// (ON DELETE RESTRICT) makes the final DELETE the arbiter — a reference
// created between re-verify and DELETE fails the transaction loudly rather
// than orphaning a version's chunk.
func (s *Sweeper) reapChunk(ctx context.Context, hash string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var sizeBytes int64
	err = tx.QueryRow(ctx, `
		SELECT c.size_bytes FROM chunks c
		WHERE c.hash = $1 AND c.gc_state = 'doomed'
		  AND NOT EXISTS (
			SELECT 1 FROM file_version_chunks fvc WHERE fvc.chunk_hash = c.hash
		  )
		  AND NOT EXISTS (
			SELECT 1 FROM upload_chunks uc
			JOIN uploads u ON u.id = uc.upload_id
			WHERE uc.chunk_hash = c.hash AND u.status = 'in_progress'
		  )
		FOR UPDATE OF c`, hash).Scan(&sizeBytes)
	if err == pgx.ErrNoRows {
		// Resurrected or re-referenced since the candidate scan — not ours
		// to touch anymore.
		return nil
	}
	if err != nil {
		return err
	}

	locRows, err := tx.Query(ctx, `SELECT node_id FROM chunk_locations WHERE chunk_hash = $1`, hash)
	if err != nil {
		return err
	}
	var nodeIDs []storage.NodeID
	for locRows.Next() {
		var id string
		if err := locRows.Scan(&id); err != nil {
			locRows.Close()
			return err
		}
		nodeIDs = append(nodeIDs, storage.NodeID(id))
	}
	locRows.Close()
	if err := locRows.Err(); err != nil {
		return err
	}

	// Objects before row: if a node is unreachable we abort with the doomed
	// row intact and retry the whole chunk next tick — already-deleted
	// replicas re-delete as no-ops. The reverse order (row first) could
	// commit the row deletion and then fail the object deletes, leaving
	// bytes on disk that nothing records or will ever retry.
	for _, nodeID := range nodeIDs {
		if err := s.router.RemoveObject(ctx, nodeID, hash); err != nil {
			return err
		}
	}

	if _, err := tx.Exec(ctx, `DELETE FROM chunks WHERE hash = $1`, hash); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}

	metrics.GCChunksReapedTotal.Inc()
	metrics.GCBytesReclaimedTotal.Add(float64(sizeBytes))
	s.logger.Info("gc reaped chunk", "chunk_hash", hash, "bytes", sizeBytes, "replicas", len(nodeIDs))
	return nil
}
