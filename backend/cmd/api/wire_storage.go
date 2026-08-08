package main

// Storage + cluster-ops wiring: the hash-ring router (internal/storage) and
// the two admin surfaces that read from it or from the DLQ (internal/events)
// — nodes/ring health and dead-letter visibility. internal/admin was sketched
// in early docs and never materialized; these two modules are where cluster
// ops actually live (docs/00-project-state.md "Known issues"), and grouping
// their wiring here — rather than splitting storage's own concern from the
// admin routes built on top of it — is what keeps that discoverable.

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"nimbus/internal/events"
	"nimbus/internal/file"
	"nimbus/internal/platform/config"
	"nimbus/internal/storage"
)

// wireStorage builds the storage.Router (bootstrapping the ring, ensuring
// buckets exist, and starting its health-check loop), registers the
// /v1/admin/nodes, /v1/admin/ring, and /v1/admin/dlq* routes, and returns
// router + storageRepo for file.Service and upload.Service to build on.
func wireStorage(
	ctx context.Context,
	mux *http.ServeMux,
	pg *pgxpool.Pool,
	rdb *redis.Client,
	cfg config.Config,
	logger *slog.Logger,
	requireAuth, requirePlatformAdmin func(http.Handler) http.Handler,
	fileRepo *file.Repository,
	eventPublisher *events.Publisher,
) (router *storage.Router, storageRepo *storage.Repository, err error) {

	if len(cfg.StorageNodes) < cfg.ReplicationFactor {
		logger.Warn("fewer storage nodes configured than the replication factor",
			"configured", len(cfg.StorageNodes), "replication_factor", cfg.ReplicationFactor)
	}
	storageNodes := make([]storage.StorageNode, len(cfg.StorageNodes))
	for i, n := range cfg.StorageNodes {
		storageNodes[i] = storage.StorageNode{ID: storage.NodeID(n.ID), Endpoint: n.Endpoint, PublicEndpoint: n.PublicEndpoint}
	}
	storageRepo = storage.NewRepository(pg)
	router, err = storage.NewRouter(storageRepo, rdb, storageNodes, cfg.MinIOAccessKey, cfg.MinIOSecretKey, logger, cfg.StorageSlowThreshold)
	if err != nil {
		return nil, nil, fmt.Errorf("storage router: %w", err)
	}
	if err := router.Bootstrap(ctx); err != nil {
		return nil, nil, fmt.Errorf("storage bootstrap: %w", err)
	}
	if err := router.EnsureBuckets(ctx); err != nil {
		return nil, nil, fmt.Errorf("storage ensure buckets: %w", err)
	}
	go router.HealthCheckLoop(ctx)

	storageHandler := storage.NewHandler(storageRepo, router, fileChunkResolverAdapter{repo: fileRepo}, cfg.ReplicationFactor)

	// /v1/admin/* is cluster ops (platform-wide reads: node health, ring,
	// DLQ across all orgs) — platform-admin only, not org-role gated.
	mux.Handle("GET /v1/admin/nodes", requireAuth(requirePlatformAdmin(http.HandlerFunc(storageHandler.ListNodes))))
	mux.Handle("GET /v1/admin/ring", requireAuth(requirePlatformAdmin(http.HandlerFunc(storageHandler.Ring))))
	// Manual-trigger repair pass (audit §02) — see storage/repair.go's doc
	// comment for why this isn't automatic on node recovery.
	mux.Handle("POST /v1/admin/nodes/{nodeId}/repair", requireAuth(requirePlatformAdmin(http.HandlerFunc(storageHandler.Repair))))

	dlqRepo := events.NewRepository(pg)
	dlqRepo.RegisterMetrics(logger)
	dlqHandler := events.NewDLQHandler(dlqRepo, eventPublisher)
	mux.Handle("GET /v1/admin/dlq", requireAuth(requirePlatformAdmin(http.HandlerFunc(dlqHandler.List))))
	mux.Handle("POST /v1/admin/dlq/{id}/retry", requireAuth(requirePlatformAdmin(http.HandlerFunc(dlqHandler.Retry))))

	return router, storageRepo, nil
}
