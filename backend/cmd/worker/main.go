// Command worker is the nimbus-worker entrypoint: the NATS consumer that
// reassembles chunks, generates thumbnails, and writes activity events
// (docs/02-system-design.md §6, docs/09-roadmap.md Day 9). Thin wiring
// only, matching cmd/api's shape — domain logic lives in internal/processing.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/nats-io/nats.go/jetstream"

	"nimbus/internal/activity"
	"nimbus/internal/events"
	"nimbus/internal/file"
	"nimbus/internal/platform/config"
	"nimbus/internal/platform/db"
	"nimbus/internal/platform/logging"
	"nimbus/internal/processing"
	"nimbus/internal/storage"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	logger := logging.New(cfg.Env)
	slog.SetDefault(logger)
	logger.Info("starting nimbus-worker", "env", cfg.Env)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pg, err := db.NewPostgres(ctx, cfg.PostgresDSN)
	if err != nil {
		return err
	}
	defer pg.Close()

	rdb, err := db.NewRedis(ctx, cfg.RedisAddr)
	if err != nil {
		return err
	}
	defer rdb.Close()

	nc, err := db.NewNATS(cfg.NATSURL)
	if err != nil {
		return err
	}
	defer nc.Close()

	js, err := jetstream.New(nc)
	if err != nil {
		return fmt.Errorf("jetstream: %w", err)
	}
	if err := events.EnsureStream(ctx, js); err != nil {
		return fmt.Errorf("ensure NATS stream: %w", err)
	}

	// The worker builds its own Router (same construction as nimbus-api) —
	// health state is in-process, so it needs its own probe loop to know
	// which nodes are healthy (docs/04-lld.md §5: Resolve only ever touches
	// in-memory state).
	storageNodes := make([]storage.StorageNode, len(cfg.StorageNodes))
	for i, n := range cfg.StorageNodes {
		storageNodes[i] = storage.StorageNode{ID: storage.NodeID(n.ID), Endpoint: n.Endpoint, PublicEndpoint: n.PublicEndpoint}
	}
	storageRepo := storage.NewRepository(pg)
	router, err := storage.NewRouter(storageRepo, rdb, storageNodes, cfg.MinIOAccessKey, cfg.MinIOSecretKey, logger)
	if err != nil {
		return fmt.Errorf("storage router: %w", err)
	}
	if err := router.Bootstrap(ctx); err != nil {
		return fmt.Errorf("storage bootstrap: %w", err)
	}
	if err := router.EnsureBuckets(ctx); err != nil {
		return fmt.Errorf("storage ensure buckets: %w", err)
	}
	go router.HealthCheckLoop(ctx)

	fileRepo := file.NewRepository(pg)
	activitySvc := activity.NewService(activity.NewRepository(pg))
	processor := processing.NewProcessor(fileRepo, storageRepo, router, activitySvc, logger)

	if err := events.Subscribe(ctx, js, processor.Process); err != nil {
		return fmt.Errorf("subscribe to upload.completed: %w", err)
	}

	logger.Info("worker ready, consuming upload.completed events")
	<-ctx.Done()
	logger.Info("worker shutting down")
	return nil
}
