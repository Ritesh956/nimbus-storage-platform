// Command api is the nimbus-api entrypoint: thin wiring only (docs/08 §cmd),
// domain logic lives under internal/.
package main

import (
	"context"
	"errors"
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
	"nimbus/internal/live"
	"nimbus/internal/org"
	"nimbus/internal/platform/config"
	"nimbus/internal/platform/db"
	"nimbus/internal/platform/httpserver"
	"nimbus/internal/platform/logging"
	"nimbus/internal/platform/mail"
	"nimbus/internal/platform/ratelimit"
	"nimbus/internal/search"
	"nimbus/internal/sharing"
	"nimbus/internal/storage"
	"nimbus/internal/upload"
)

// userLookupAdapter satisfies org.UserLookup using auth.Repository, so org
// never imports auth's data-access layer directly (docs/03-hld.md §1).
type userLookupAdapter struct{ repo *auth.Repository }

func (a userLookupAdapter) GetUserByEmail(ctx context.Context, email string) (string, error) {
	u, err := a.repo.GetUserByEmail(ctx, email)
	if err != nil {
		return "", err
	}
	return u.ID, nil
}

// membershipAdapter satisfies folder.MembershipChecker and
// file.MembershipChecker (identical method sets) using org.Repository.
type membershipAdapter struct{ repo *org.Repository }

func (a membershipAdapter) IsMember(ctx context.Context, orgID, userID string) (bool, error) {
	_, err := a.repo.GetMembership(ctx, orgID, userID)
	if errors.Is(err, org.ErrNotMember) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// folderOrgLookupAdapter satisfies file.FolderOrgLookup using
// folder.Repository, so file can validate a move target's org without
// importing folder's data-access layer directly.
type folderOrgLookupAdapter struct{ repo *folder.Repository }

func (a folderOrgLookupAdapter) OrgIDOf(ctx context.Context, folderID string) (string, error) {
	f, err := a.repo.Get(ctx, folderID)
	if err != nil {
		return "", err
	}
	return f.OrgID, nil
}

// fileListerAdapter satisfies folder.FileLister using file.Repository, so
// folder can list a folder's files without importing file's data-access
// layer directly.
type fileListerAdapter struct{ repo *file.Repository }

func (a fileListerAdapter) ListInFolder(ctx context.Context, folderID string) ([]folder.FileSummary, error) {
	files, err := a.repo.ListInFolder(ctx, folderID)
	if err != nil {
		return nil, err
	}
	out := make([]folder.FileSummary, len(files))
	for i, f := range files {
		out[i] = folder.FileSummary{ID: f.ID, Name: f.Name, SizeBytes: f.SizeBytes, MimeType: f.MimeType, HasThumbnail: f.HasThumbnail}
	}
	return out, nil
}

// fileChunkResolverAdapter satisfies storage.FileChunkResolver using
// file.Repository — resolves a file's latest version's chunk list for the
// admin ring view (backlog #13).
type fileChunkResolverAdapter struct{ repo *file.Repository }

func (a fileChunkResolverAdapter) LatestVersionChunks(ctx context.Context, fileID string) ([]storage.ChunkRef, error) {
	f, err := a.repo.GetAny(ctx, fileID)
	if err != nil {
		return nil, err
	}
	if f.LatestVersionID == nil {
		return []storage.ChunkRef{}, nil
	}
	chunks, err := a.repo.GetVersionChunks(ctx, *f.LatestVersionID)
	if err != nil {
		return nil, err
	}
	out := make([]storage.ChunkRef, len(chunks))
	for i, c := range chunks {
		out[i] = storage.ChunkRef{Sequence: c.Sequence, Hash: c.Hash}
	}
	return out, nil
}

// fileShareLookupAdapter satisfies sharing.FileLookup using file.Repository,
// projecting only the public-safe fields (docs/06-api-design.md §7).
type fileShareLookupAdapter struct{ repo *file.Repository }

func (a fileShareLookupAdapter) GetForShare(ctx context.Context, fileID string) (sharing.FileInfo, error) {
	f, err := a.repo.Get(ctx, fileID)
	if err != nil {
		return sharing.FileInfo{}, err
	}
	info := sharing.FileInfo{ID: f.ID, Name: f.Name, LatestVersionID: f.LatestVersionID}
	if f.LatestVersionID != nil {
		v, err := a.repo.GetVersion(ctx, f.ID, *f.LatestVersionID)
		if err != nil {
			return sharing.FileInfo{}, err
		}
		info.SizeBytes, info.MimeType, info.ChecksumSHA256 = v.SizeBytes, v.MimeType, v.ChecksumSHA256
	}
	return info, nil
}

// fileScopeAdapter satisfies sharing.FileScope using file.Repository —
// org/folder of a live (non-trashed) file, for bundle-member validation
// and folder-share subtree checks.
type fileScopeAdapter struct{ repo *file.Repository }

func (a fileScopeAdapter) LiveFileOrg(ctx context.Context, fileID string) (string, error) {
	f, err := a.repo.Get(ctx, fileID)
	if err != nil {
		return "", err
	}
	return f.OrgID, nil
}

func (a fileScopeAdapter) LiveFileFolder(ctx context.Context, fileID string) (string, error) {
	f, err := a.repo.Get(ctx, fileID)
	if err != nil {
		return "", err
	}
	return f.FolderID, nil
}

// folderShareAdapter satisfies sharing.FolderResolver using
// folder.Repository + file.Repository — public-safe folder projections,
// share listings, and the subtree test folder shares scope-check against.
type folderShareAdapter struct {
	folders *folder.Repository
	files   *file.Repository
}

func (a folderShareAdapter) GetFolderForShare(ctx context.Context, folderID string) (sharing.FolderInfo, error) {
	f, err := a.folders.Get(ctx, folderID) // trashed folders 404 — trashing a shared folder kills the link
	if err != nil {
		return sharing.FolderInfo{}, err
	}
	return sharing.FolderInfo{ID: f.ID, Name: f.Name}, nil
}

func (a folderShareAdapter) ListShareChildren(ctx context.Context, folderID string) ([]sharing.FolderInfo, []sharing.FileInfo, error) {
	f, err := a.folders.Get(ctx, folderID)
	if err != nil {
		return nil, nil, err
	}
	subfolders, err := a.folders.ListChildren(ctx, f.OrgID, &f.ID)
	if err != nil {
		return nil, nil, err
	}
	folderInfos := make([]sharing.FolderInfo, len(subfolders))
	for i, sf := range subfolders {
		folderInfos[i] = sharing.FolderInfo{ID: sf.ID, Name: sf.Name}
	}
	entries, err := a.files.ListInFolder(ctx, folderID)
	if err != nil {
		return nil, nil, err
	}
	fileInfos := make([]sharing.FileInfo, len(entries))
	for i, e := range entries {
		info := sharing.FileInfo{ID: e.ID, Name: e.Name, LatestVersionID: e.LatestVersionID}
		if e.SizeBytes != nil {
			info.SizeBytes = *e.SizeBytes
		}
		if e.MimeType != nil {
			info.MimeType = *e.MimeType
		}
		fileInfos[i] = info
	}
	return folderInfos, fileInfos, nil
}

func (a folderShareAdapter) IsSelfOrDescendant(ctx context.Context, folderID, candidateID string) (bool, error) {
	return a.folders.IsSelfOrDescendant(ctx, folderID, candidateID)
}

// orgStorageStatsAdapter satisfies org.StorageStatsReader using
// file.Repository — the storage half of the owner usage view.
type orgStorageStatsAdapter struct{ repo *file.Repository }

func (a orgStorageStatsAdapter) OrgStorageStats(ctx context.Context, orgID string) (org.StorageStats, error) {
	used, err := a.repo.OrgUsageBytes(ctx, orgID)
	if err != nil {
		return org.StorageStats{}, err
	}
	live, trashed, err := a.repo.OrgFileCounts(ctx, orgID)
	if err != nil {
		return org.StorageStats{}, err
	}
	return org.StorageStats{UsedBytes: used, LiveFiles: live, TrashedFiles: trashed}, nil
}

// orgActivityStatsAdapter satisfies org.ActivityStatsReader using
// activity.Repository — per-member and per-verb aggregates for the usage
// view.
type orgActivityStatsAdapter struct{ repo *activity.Repository }

func (a orgActivityStatsAdapter) ActorStats(ctx context.Context, orgID string, since time.Time) (map[string]org.MemberActivityStat, error) {
	stats, err := a.repo.ActorStats(ctx, orgID, since)
	if err != nil {
		return nil, err
	}
	out := make(map[string]org.MemberActivityStat, len(stats))
	for userID, s := range stats {
		last := s.LastActiveAt
		out[userID] = org.MemberActivityStat{Events: s.EventsSince, LastActiveAt: &last}
	}
	return out, nil
}

func (a orgActivityStatsAdapter) VerbCounts(ctx context.Context, orgID string, since time.Time) (map[string]int, error) {
	return a.repo.VerbCounts(ctx, orgID, since)
}

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
	authSvc := auth.NewService(authRepo, rdb, cfg.JWTSecret, cfg.AccessTokenTTL, cfg.RefreshTokenTTL, mailer, cfg.WebBaseURL)
	authHandler := auth.NewHandler(authSvc)
	requireAuth := auth.Middleware(authSvc)
	requirePlatformAdmin := auth.RequirePlatformAdmin(authRepo)

	if n, err := authRepo.PromotePlatformAdmins(ctx, cfg.PlatformAdminEmails); err != nil {
		return fmt.Errorf("promote platform admins: %w", err)
	} else if n > 0 {
		logger.Info("promoted platform admins", "count", n)
	}

	// These repos are built early (ahead of their own modules' wiring
	// below) because org.NewService needs them: folderRepo as a
	// FolderCreator, and the other three behind the usage-view ports
	// (file/sharing/activity also satisfy later wiring — see below).
	folderRepo := folder.NewRepository(pg)
	fileRepo := file.NewRepository(pg)
	sharingRepo := sharing.NewRepository(pg)
	activityRepo := activity.NewRepository(pg)

	orgRepo := org.NewRepository(pg)
	orgSvc := org.NewService(orgRepo, userLookupAdapter{repo: authRepo}, folderRepo,
		org.UsageSources{
			Storage:  orgStorageStatsAdapter{repo: fileRepo},
			Shares:   sharingRepo, // ActiveLinkCount matches org.ShareLinkCounter directly
			Activity: orgActivityStatsAdapter{repo: activityRepo},
		}, cfg.OrgQuotaBytes)
	orgHandler := org.NewHandler(orgSvc)
	requireMember := org.RequireRole(orgRepo, org.RoleMember)
	requireOwner := org.RequireRole(orgRepo, org.RoleOwner)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", httpserver.Liveness)
	mux.HandleFunc("GET /readyz", ready.Handler())
	mux.Handle("GET /metrics", promhttp.Handler())

	mux.HandleFunc("POST /v1/auth/register", authHandler.Register)
	mux.HandleFunc("POST /v1/auth/login", authHandler.Login)
	mux.HandleFunc("POST /v1/auth/login/totp", authHandler.TOTPLogin)
	mux.HandleFunc("POST /v1/auth/refresh", authHandler.Refresh)
	mux.HandleFunc("POST /v1/auth/logout", authHandler.Logout)
	mux.HandleFunc("POST /v1/auth/password/forgot", authHandler.ForgotPassword)
	mux.HandleFunc("POST /v1/auth/password/reset", authHandler.ResetPassword)
	mux.Handle("GET /v1/auth/me", requireAuth(http.HandlerFunc(authHandler.Me)))
	mux.Handle("GET /v1/auth/totp", requireAuth(http.HandlerFunc(authHandler.TOTPStatus)))
	mux.Handle("POST /v1/auth/totp/setup", requireAuth(http.HandlerFunc(authHandler.TOTPSetup)))
	mux.Handle("POST /v1/auth/totp/confirm", requireAuth(http.HandlerFunc(authHandler.TOTPConfirm)))
	mux.Handle("DELETE /v1/auth/totp", requireAuth(http.HandlerFunc(authHandler.TOTPDisable)))

	mux.Handle("POST /v1/orgs", requireAuth(http.HandlerFunc(orgHandler.Create)))
	mux.Handle("GET /v1/orgs", requireAuth(http.HandlerFunc(orgHandler.ListMine)))
	mux.Handle("GET /v1/orgs/{orgId}/members", requireAuth(requireMember(http.HandlerFunc(orgHandler.ListMembers))))
	mux.Handle("POST /v1/orgs/{orgId}/members", requireAuth(requireOwner(http.HandlerFunc(orgHandler.AddMember))))
	mux.Handle("DELETE /v1/orgs/{orgId}/members/{userId}", requireAuth(requireOwner(http.HandlerFunc(orgHandler.RemoveMember))))
	// Org governance (owner-gated), distinct from /v1/admin/* cluster ops.
	mux.Handle("GET /v1/orgs/{orgId}/usage", requireAuth(requireOwner(http.HandlerFunc(orgHandler.Usage))))

	members := membershipAdapter{repo: orgRepo}

	if len(cfg.StorageNodes) < cfg.ReplicationFactor {
		logger.Warn("fewer storage nodes configured than the replication factor",
			"configured", len(cfg.StorageNodes), "replication_factor", cfg.ReplicationFactor)
	}
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

	storageHandler := storage.NewHandler(storageRepo, router, fileChunkResolverAdapter{repo: fileRepo}, cfg.ReplicationFactor)

	// /v1/admin/* is cluster ops (platform-wide reads: node health, ring,
	// DLQ across all orgs) — platform-admin only, not org-role gated.
	mux.Handle("GET /v1/admin/nodes", requireAuth(requirePlatformAdmin(http.HandlerFunc(storageHandler.ListNodes))))
	mux.Handle("GET /v1/admin/ring", requireAuth(requirePlatformAdmin(http.HandlerFunc(storageHandler.Ring))))

	dlqHandler := events.NewDLQHandler(events.NewRepository(pg), eventPublisher)
	mux.Handle("GET /v1/admin/dlq", requireAuth(requirePlatformAdmin(http.HandlerFunc(dlqHandler.List))))
	mux.Handle("POST /v1/admin/dlq/{id}/retry", requireAuth(requirePlatformAdmin(http.HandlerFunc(dlqHandler.Retry))))

	folderSvc := folder.NewService(folderRepo)
	folderHandler := folder.NewHandler(folderSvc, fileListerAdapter{repo: fileRepo}, members)
	requireFolderAccess := folder.RequireAccess(folderRepo, members)

	fileSvc := file.NewService(fileRepo, folderOrgLookupAdapter{repo: folderRepo}, storageRepo, router)
	fileHandler := file.NewHandler(fileSvc, members)
	requireFileAccess := file.RequireAccess(fileRepo, members)

	mux.Handle("POST /v1/orgs/{orgId}/folders", requireAuth(requireMember(http.HandlerFunc(folderHandler.Create))))
	mux.Handle("GET /v1/orgs/{orgId}/folders", requireAuth(requireMember(http.HandlerFunc(folderHandler.ListRoot))))
	mux.Handle("GET /v1/orgs/{orgId}/trash/folders", requireAuth(requireMember(http.HandlerFunc(folderHandler.ListTrashed))))
	mux.Handle("GET /v1/orgs/{orgId}/trash/files", requireAuth(requireMember(http.HandlerFunc(fileHandler.ListTrashed))))
	mux.Handle("GET /v1/folders/{folderId}/children", requireAuth(requireFolderAccess(http.HandlerFunc(folderHandler.ListChildren))))
	mux.Handle("GET /v1/folders/{folderId}/path", requireAuth(requireFolderAccess(http.HandlerFunc(folderHandler.Path))))
	mux.Handle("PATCH /v1/folders/{folderId}", requireAuth(requireFolderAccess(http.HandlerFunc(folderHandler.Update))))
	mux.Handle("DELETE /v1/folders/{folderId}", requireAuth(requireFolderAccess(http.HandlerFunc(folderHandler.Delete))))
	mux.Handle("POST /v1/folders/{folderId}/restore", requireAuth(http.HandlerFunc(folderHandler.Restore)))

	mux.Handle("PATCH /v1/files/{fileId}", requireAuth(requireFileAccess(http.HandlerFunc(fileHandler.Update))))
	mux.Handle("DELETE /v1/files/{fileId}", requireAuth(requireFileAccess(http.HandlerFunc(fileHandler.Delete))))
	mux.Handle("POST /v1/files/{fileId}/restore", requireAuth(http.HandlerFunc(fileHandler.Restore)))
	mux.Handle("DELETE /v1/files/{fileId}/purge", requireAuth(http.HandlerFunc(fileHandler.Purge)))
	mux.Handle("GET /v1/files/{fileId}/thumbnail", requireAuth(requireFileAccess(http.HandlerFunc(fileHandler.Thumbnail))))
	mux.Handle("GET /v1/files/{fileId}/versions", requireAuth(requireFileAccess(http.HandlerFunc(fileHandler.ListVersions))))
	mux.Handle("GET /v1/files/{fileId}/versions/{versionId}/download-plan", requireAuth(requireFileAccess(http.HandlerFunc(fileHandler.DownloadPlan))))
	mux.Handle("POST /v1/files/{fileId}/versions/{versionId}/restore", requireAuth(requireFileAccess(http.HandlerFunc(fileHandler.RestoreVersion))))

	activitySvc := activity.NewService(activityRepo, live.NewPublisher(rdb))
	activityHandler := activity.NewHandler(activitySvc)
	liveHandler := live.NewHandler(rdb)

	mux.Handle("GET /v1/orgs/{orgId}/activity", requireAuth(requireMember(http.HandlerFunc(activityHandler.List))))
	mux.Handle("GET /v1/orgs/{orgId}/events", requireAuth(requireMember(http.HandlerFunc(liveHandler.Stream))))

	searchRepo := search.NewRepository(pg)
	searchSvc := search.NewService(searchRepo)
	searchHandler := search.NewHandler(searchSvc)

	mux.Handle("GET /v1/orgs/{orgId}/search", requireAuth(requireMember(http.HandlerFunc(searchHandler.Search))))

	// fileRepo.CreateWithVersion/AddVersion/GetForUpload already match
	// upload.FileCreator's signatures exactly, and activitySvc/eventPublisher
	// already match upload.ActivityRecorder/EventPublisher — all passed
	// directly, no adapters needed.
	uploadRepo := upload.NewRepository(pg)
	uploadSvc := upload.NewService(uploadRepo, router, fileRepo, folderOrgLookupAdapter{repo: folderRepo}, members, activitySvc, eventPublisher, fileRepo, cfg.ReplicationFactor, cfg.MaxUploadBytes, cfg.OrgQuotaBytes)
	uploadHandler := upload.NewHandler(uploadSvc)
	requireUploadAccess := upload.RequireAccess(uploadRepo, members)

	mux.Handle("POST /v1/chunks/check", requireAuth(http.HandlerFunc(uploadHandler.CheckChunks)))
	mux.Handle("POST /v1/uploads", requireAuth(http.HandlerFunc(uploadHandler.InitUpload)))
	mux.Handle("POST /v1/uploads/{uploadId}/chunks/{hash}/init", requireAuth(requireUploadAccess(http.HandlerFunc(uploadHandler.InitChunk))))
	mux.Handle("POST /v1/uploads/{uploadId}/chunks/{hash}/commit", requireAuth(requireUploadAccess(http.HandlerFunc(uploadHandler.CommitChunk))))
	mux.Handle("POST /v1/uploads/{uploadId}/complete", requireAuth(requireUploadAccess(http.HandlerFunc(uploadHandler.Complete))))

	sharingSvc := sharing.NewService(sharingRepo, fileShareLookupAdapter{repo: fileRepo}, fileScopeAdapter{repo: fileRepo},
		folderShareAdapter{folders: folderRepo, files: fileRepo}, fileSvc)
	sharingHandler := sharing.NewHandler(sharingSvc, members)

	mux.Handle("POST /v1/files/{fileId}/share", requireAuth(requireFileAccess(http.HandlerFunc(sharingHandler.Create))))
	mux.Handle("POST /v1/folders/{folderId}/share", requireAuth(requireFolderAccess(http.HandlerFunc(sharingHandler.CreateFolder))))
	mux.Handle("POST /v1/orgs/{orgId}/shares", requireAuth(requireMember(http.HandlerFunc(sharingHandler.CreateBundle))))
	// The /v1/shares/* reads are public — no requireAuth; that's the point of a share link.
	mux.HandleFunc("GET /v1/shares/{token}", sharingHandler.Resolve)
	mux.HandleFunc("GET /v1/shares/{token}/folders/{folderId}", sharingHandler.Children)
	mux.HandleFunc("GET /v1/shares/{token}/files/{fileId}/download-plan", sharingHandler.DownloadPlan)
	mux.Handle("DELETE /v1/shares/{token}", requireAuth(http.HandlerFunc(sharingHandler.Delete)))

	// Remaining domain routes are registered here as each module lands
	// (admin org-usage).

	middlewares := []func(http.Handler) http.Handler{
		httpserver.CORS(cfg.CORSOrigin), // outermost: must short-circuit OPTIONS preflight before mux routing
		httpserver.RequestID,
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
