// Command api is the nimbus-api entrypoint: thin wiring only (docs/08 §cmd),
// domain logic lives under internal/. run() itself only sequences shared
// infra setup (config, Postgres/Redis/NATS, the readiness checker) and calls
// into one wire_*.go file per concern (auth+org, storage+cluster-ops,
// filesystem, activity, upload+sharing) — see adapters.go for the
// cross-module port adapters those files use, and each wire_*.go's own doc
// comment for why it's grouped the way it is. Splitting this out of one long
// function was itself a fix for an audit finding (docs/00-project-state.md,
// full-codebase audit §01) that the original single-file version had no
// seam for skimming or testing the wiring in isolation.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"nimbus/internal/activity"
	"nimbus/internal/auth"
	"nimbus/internal/events"
	"nimbus/internal/file"
	"nimbus/internal/folder"
	"nimbus/internal/platform/config"
	"nimbus/internal/platform/db"
	"nimbus/internal/platform/httpserver"
	"nimbus/internal/platform/logging"
	"nimbus/internal/platform/mail"
	"nimbus/internal/platform/ratelimit"
	"nimbus/internal/platform/tracing"
	"nimbus/internal/sharing"
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
	slog.SetDefault(logger) // lets handlers log unexpected errors via slog.Default() without threading a logger through every signature
	logger.Info("starting nimbus-api", "env", cfg.Env, "port", cfg.HTTPPort)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	shutdownTracing, err := tracing.Setup(ctx, "nimbus-api", cfg.OTelExporterEndpoint)
	if err != nil {
		return fmt.Errorf("tracing setup: %w", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdownTracing(shutdownCtx); err != nil {
			logger.Warn("tracing shutdown", "error", err)
		}
	}()

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
	eventPublisher := events.NewPublisher(js)

	ready := httpserver.NewReadinessChecker()
	ready.Register("postgres", func(ctx context.Context) error { return pg.Ping(ctx) })
	ready.Register("redis", func(ctx context.Context) error { return rdb.Ping(ctx).Err() })
	ready.Register("nats", func(ctx context.Context) error {
		if !nc.IsConnected() {
			return fmt.Errorf("not connected")
		}
		return nil
	})

	var mailer auth.Mailer
	if cfg.SMTPAddr != "" {
		mailer = mail.NewSMTPSender(cfg.SMTPAddr, cfg.SMTPFrom)
	} else {
		logger.Warn("NIMBUS_SMTP_ADDR not set — password-reset links will be logged, not emailed")
	}

	authRepo := auth.NewRepository(pg)

	if err := authRepo.EnsureSeededAdmin(ctx, cfg.AdminEmail, cfg.AdminPassword); err != nil {
		return fmt.Errorf("seed platform admin: %w", err)
	}
	logger.Info("seeded platform admin", "email", cfg.AdminEmail)

	// These repos are constructed once, here, because more than one
	// wire_*.go concern needs each of them (e.g. fileRepo backs org's usage
	// view, storage's admin ring view, the filesystem routes, and upload —
	// four different concerns, one Repository).
	folderRepo := folder.NewRepository(pg)
	fileRepo := file.NewRepository(pg)
	sharingRepo := sharing.NewRepository(pg)
	activityRepo := activity.NewRepository(pg)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", httpserver.Liveness)
	mux.HandleFunc("GET /readyz", ready.Handler())
	mux.Handle("GET /metrics", promhttp.Handler())

	authSvc, requireAuth, requirePlatformAdmin, orgRepo, requireMember :=
		wireAuth(mux, pg, rdb, cfg, logger, mailer, authRepo, folderRepo, fileRepo, sharingRepo, activityRepo)

	members := membershipAdapter{repo: orgRepo}

	router, storageRepo, err := wireStorage(ctx, mux, pg, rdb, cfg, logger, requireAuth, requirePlatformAdmin, fileRepo, eventPublisher)
	if err != nil {
		return err
	}

	fileSvc, requireFolderAccess, requireFileAccess :=
		wireFiles(mux, pg, requireAuth, requireMember, folderRepo, fileRepo, storageRepo, router, members)

	activitySvc := wireActivity(mux, rdb, requireAuth, requireMember, activityRepo)

	wireUpload(mux, pg, cfg, requireAuth, requireMember, requireFileAccess, requireFolderAccess,
		router, fileRepo, folderRepo, sharingRepo, fileSvc, members, activitySvc, eventPublisher, orgRepo)

	middlewares := []func(http.Handler) http.Handler{
		httpserver.CORS(cfg.CORSOrigin), // outermost: must short-circuit OPTIONS preflight before mux routing
		httpserver.RequestID,
		// Tracing wraps Recoverer (not the reverse) so a recovered panic's
		// 500 status still lands on the span's http.status_code attribute
		// instead of the span ending before that attribute is set — see
		// httpserver.Tracing's own doc comment.
		httpserver.Tracing(mux),
		httpserver.Recoverer(logger),
		httpserver.RequestLogger(logger),
		httpserver.Metrics(mux),
	}
	if cfg.RateLimitRPS > 0 {
		limiter := ratelimit.NewLimiter(rdb, cfg.RateLimitRPS, cfg.RateLimitBurst, logger)
		// Bucket key: authenticated callers by user ID (signature-only peek —
		// no blacklist round-trip; real auth still happens in Middleware),
		// everyone else by client IP. Innermost of the global chain per
		// docs/04-lld.md §4, so 429s still hit the logger and metrics.
		middlewares = append(middlewares, limiter.Middleware(func(r *http.Request) string {
			if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
				if uid := authSvc.PeekUserID(strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))); uid != "" {
					return "user:" + uid
				}
			}
			return "ip:" + ratelimit.ClientIP(r)
		}))
	}
	handler := httpserver.Chain(mux, middlewares...)

	srv := httpserver.New(":"+cfg.HTTPPort, handler)
	return httpserver.Run(ctx, srv, logger, 15*time.Second)
}
