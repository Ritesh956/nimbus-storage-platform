package storage

// Repair closes the audit's §02 finding that "recovery restores routing but
// not necessarily the replica count": today, a node coming back up (probe
// succeeding again) only ever means Router.health flips back to healthy —
// nothing re-verifies that the bytes chunk_locations says are committed
// there are actually still on disk. Standalone MinIO with no distributed
// mode underneath (a deliberate choice, docs/02-system-design.md §1) means
// losing that node's volume loses its copies outright; without a repair
// pass, chunk_locations keeps claiming a replica exists after it doesn't.
//
// Manual-trigger by design, not automatic-on-recovery: an automatic pass
// firing on every Down→Healthy transition risks a repair storm the moment a
// node flaps, and at this project's scale an operator clicking a button
// (POST /v1/admin/nodes/{nodeId}/repair) is a reasonable MVP for "the
// primitive exists" without also having to design storm/backoff protection
// for a case (real volume loss) that a health-flap doesn't actually cause.

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/minio/minio-go/v7"

	"nimbus/internal/platform/metrics"
)

// RepairResult summarizes one RepairNode pass.
type RepairResult struct {
	Checked      int `json:"checked"`
	Restored     int `json:"restored"`
	Unrepairable int `json:"unrepairable"`
}

// RepairNode re-verifies every chunk recorded as committed on node,
// restoring any that are physically missing by copying bytes from another
// committed replica. A chunk with no surviving replica anywhere (or whose
// source read / target write itself fails) is counted Unrepairable and
// left marked degraded rather than committed — a truthful record beats a
// silently-wrong one.
func (rt *Router) RepairNode(ctx context.Context, node NodeID) (RepairResult, error) {
	rt.mu.RLock()
	cli := rt.internalMinio[node]
	rt.mu.RUnlock()
	if cli == nil {
		return RepairResult{}, fmt.Errorf("unknown node %s", node)
	}

	hashes, err := rt.repo.CommittedChunksForNode(ctx, node)
	if err != nil {
		return RepairResult{}, fmt.Errorf("list committed chunks for %s: %w", node, err)
	}

	var res RepairResult
	for _, hash := range hashes {
		res.Checked++
		metrics.StorageRepairChunksCheckedTotal.Inc()

		if _, err := cli.StatObject(ctx, bucketName, hash, minio.StatObjectOptions{}); err == nil {
			continue // still there — nothing to do
		}

		if !rt.restoreChunk(ctx, hash, node) {
			res.Unrepairable++
			metrics.StorageRepairChunksUnrepairableTotal.Inc()
			continue
		}
		res.Restored++
		metrics.StorageRepairChunksRestoredTotal.Inc()
	}
	return res, nil
}

// restoreChunk copies hash from any other committed replica onto node,
// marking the (chunk, node) row degraded first so a crash mid-repair
// doesn't leave a physically-missing chunk still claiming 'committed'.
func (rt *Router) restoreChunk(ctx context.Context, hash string, node NodeID) bool {
	if err := rt.repo.MarkChunkLocationDegraded(ctx, hash, node); err != nil {
		rt.logger.Error("repair: failed to mark chunk degraded", "chunk", hash, "node", node, "error", err)
		return false
	}

	locations, err := rt.repo.LocationsForChunk(ctx, hash)
	if err != nil {
		rt.logger.Error("repair: failed to look up other locations", "chunk", hash, "node", node, "error", err)
		return false
	}
	var source NodeID
	for _, loc := range locations {
		if loc != node {
			source = loc
			break
		}
	}
	if source == "" {
		rt.logger.Error("repair: no surviving replica to restore from", "chunk", hash, "node", node)
		return false
	}

	obj, err := rt.GetObject(ctx, source, hash)
	if err != nil {
		rt.logger.Error("repair: failed to read source replica", "chunk", hash, "source", source, "node", node, "error", err)
		return false
	}
	data, err := io.ReadAll(obj)
	closeErr := obj.Close()
	if err != nil {
		rt.logger.Error("repair: failed to read source replica bytes", "chunk", hash, "source", source, "node", node, "error", err)
		return false
	}
	if closeErr != nil && !errors.Is(closeErr, io.EOF) {
		rt.logger.Warn("repair: source object close error (bytes already read, continuing)", "chunk", hash, "source", source, "error", closeErr)
	}

	if err := rt.PutObject(ctx, node, hash, data, ""); err != nil {
		rt.logger.Error("repair: failed to write restored chunk", "chunk", hash, "node", node, "error", err)
		return false
	}
	if err := rt.repo.MarkChunkLocationRepaired(ctx, hash, node); err != nil {
		rt.logger.Error("repair: restored bytes but failed to flip status back to committed", "chunk", hash, "node", node, "error", err)
		return false
	}
	return true
}
