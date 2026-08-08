// Command worker is the nimbus-worker entrypoint: the NATS consumer that
// reassembles chunks, generates thumbnails, and writes activity events
// (docs/02-system-design.md §6, docs/09-roadmap.md Day 9). Thin wiring
// only, matching cmd/api's shape — domain logic lives in internal/processing.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"nimbus/internal/activity"
	"nimbus/internal/events"
	"nimbus/internal/file"
	"nimbus/internal/folder"
	"nimbus/internal/gc"
	"nimbus/internal/live"
	"nimbus/internal/platform/config"
	"nimbus/internal/platform/db"
	"nimbus/internal/platform/httpserver"
	"nimbus/internal/platform/logging"
	"nimbus/internal/platform/metrics"
	"nimbus/internal/processing"
	"nimbus/internal/storage"
)

// consumerLagPollInterval controls how often the worker refreshes
// nimbus_nats_consumer_pending from the JetStream consumer's own info call —
// cheap enough to poll well under the Prometheus scrape_interval (5s, see
// deploy/observability/prometheus.yml).
const consumerLagPollInterval = 3 * time.Second

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
	router, err := storage.NewRouter(storageRepo, rdb, storageNodes, cfg.MinIOAccessKey, cfg.MinIOSecretKey, logger, cfg.StorageSlowThreshold)
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
	activitySvc := activity.NewService(activity.NewRepository(pg), live.NewPublisher(rdb))
	processor := processing.NewProcessor(fileRepo, storageRepo, router, activitySvc, logger)
	// Background, not blocking startup: thumbnail consumption can begin
	// immediately; only the first PDF would wait on this.
	go processing.WarmupPDFium(logger)

	consumer, err := events.Subscribe(ctx, js, events.NewRepository(pg), processor.Process)
	if err != nil {
		return fmt.Errorf("subscribe to upload.completed: %w", err)
	}
	go pollConsumerLag(ctx, consumer, logger)

	if cfg.GCInterval > 0 {
		trashRetention := time.Duration(cfg.TrashRetentionDays) * 24 * time.Hour
		sweeper := gc.NewSweeper(pg, router, cfg.GCInterval, cfg.GCGrace, trashRetention, fileRepo, folder.NewRepository(pg), logger)
		go sweeper.Run(ctx)
		logger.Info("gc sweeper started", "interval", cfg.GCInterval, "grace", cfg.GCGrace, "trash_retention", trashRetention)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", httpserver.Liveness)
	mux.Handle("GET /metrics", promhttp.Handler())
	srv := httpserver.New(":"+cfg.HTTPPort, mux)
	go func() {
		if err := httpserver.Run(ctx, srv, logger, 5*time.Second); err != nil {
			logger.Error("worker http server error", "error", err)
		}
	}()

	logger.Info("worker ready, consuming upload.completed events")
	<-ctx.Done()
	logger.Info("worker shutting down")
	return nil
}

// pollConsumerLag refreshes nimbus_nats_consumer_pending from the
// consumer's own Info() until ctx is cancelled. Polling rather than
// computing lag from delivered messages keeps this accurate across
// worker restarts, since Info reflects JetStream's server-side view.
func pollConsumerLag(ctx context.Context, consumer jetstream.Consumer, logger *slog.Logger) {
	ticker := time.NewTicker(consumerLagPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			info, err := consumer.Info(ctx)
			if err != nil {
				logger.Warn("failed to fetch consumer info for lag metric", "error", err)
				continue
			}
			metrics.NATSConsumerPending.WithLabelValues(events.ThumbnailConsumerName).Set(float64(info.NumPending))
		}
	}
}
