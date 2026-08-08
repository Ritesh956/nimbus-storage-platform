package main

// Auth + org wiring — grouped in one file because they're mutually
// dependent at construction time: org.Service needs an auth-backed
// UserLookup (invite-by-email) and itself satisfies auth.OrgCreator
// (Register auto-creates a default org for every new user), so neither can
// be fully built without the other already having started.

import (
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"nimbus/internal/activity"
	"nimbus/internal/auth"
	"nimbus/internal/file"
	"nimbus/internal/folder"
	"nimbus/internal/org"
	"nimbus/internal/platform/config"
	"nimbus/internal/platform/ratelimit"
	"nimbus/internal/sharing"
)

// wireAuth constructs org.Service (and the usage-view ports it needs) ahead
// of auth.Service, registers every /v1/auth/* and org-governance route onto
// mux, and returns the pieces later wire_*.go files gate their own routes
// with (requireAuth, requireMember, requireOrgAdmin, requirePlatformAdmin)
// plus orgRepo, which the caller needs to build membershipAdapter.
func wireAuth(
	mux *http.ServeMux,
	pg *pgxpool.Pool,
	rdb *redis.Client,
	cfg config.Config,
	logger *slog.Logger,
	mailer auth.Mailer,
	authRepo *auth.Repository,
	folderRepo *folder.Repository,
	fileRepo *file.Repository,
	sharingRepo *sharing.Repository,
	activityRepo *activity.Repository,
) (authSvc *auth.Service, requireAuth, requirePlatformAdmin func(http.Handler) http.Handler,
	orgRepo *org.Repository, requireMember func(http.Handler) http.Handler) {

	orgRepo = org.NewRepository(pg)
	orgSvc := org.NewService(orgRepo, userLookupAdapter{repo: authRepo}, folderRepo,
		org.UsageSources{
			Storage:  orgStorageStatsAdapter{repo: fileRepo},
			Shares:   sharingRepo, // ActiveLinkCount matches org.ShareLinkCounter directly
			Activity: orgActivityStatsAdapter{repo: activityRepo},
		}, cfg.OrgQuotaBytes)
	orgHandler := org.NewHandler(orgSvc)
	requireMember = org.RequireRole(orgRepo, org.RoleMember)
	// Org governance (member management, usage view) opens at the admin
	// tier; the finer owner-vs-admin bounds live in org.Service. Not
	// returned — nothing outside this file gates a route on it.
	requireOrgAdmin := org.RequireRole(orgRepo, org.RoleAdmin)

	authSvc = auth.NewService(authRepo, rdb, cfg.JWTSecret, cfg.JWTSecretPrevious, cfg.AccessTokenTTL, cfg.RefreshTokenTTL, mailer, cfg.WebBaseURL, orgSvc)
	authHandler := auth.NewHandler(authSvc)
	requireAuth = auth.Middleware(authSvc)
	requirePlatformAdmin = auth.RequirePlatformAdmin(authRepo)

	mux.HandleFunc("POST /v1/auth/register", authHandler.Register)
	if cfg.LoginRateLimitRPS > 0 {
		// Tighter, IP-only bucket (see config.LoginRateLimitRPS) — these two
		// routes run before authentication exists, so the general per-user
		// limiter (wired in run()) never applies to them.
		loginLimiter := ratelimit.NewLimiter(rdb, cfg.LoginRateLimitRPS, cfg.LoginRateLimitBurst, logger)
		loginKeyFn := func(r *http.Request) string { return "login-ip:" + ratelimit.ClientIP(r) }
		mux.Handle("POST /v1/auth/login", loginLimiter.Middleware(loginKeyFn)(http.HandlerFunc(authHandler.Login)))
		mux.Handle("POST /v1/auth/login/totp", loginLimiter.Middleware(loginKeyFn)(http.HandlerFunc(authHandler.TOTPLogin)))
	} else {
		mux.HandleFunc("POST /v1/auth/login", authHandler.Login)
		mux.HandleFunc("POST /v1/auth/login/totp", authHandler.TOTPLogin)
	}
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
	mux.Handle("POST /v1/orgs/{orgId}/members", requireAuth(requireOrgAdmin(http.HandlerFunc(orgHandler.AddMember))))
	mux.Handle("DELETE /v1/orgs/{orgId}/members/{userId}", requireAuth(requireOrgAdmin(http.HandlerFunc(orgHandler.RemoveMember))))
	// Org governance (admin tier and up), distinct from /v1/admin/* cluster ops.
	mux.Handle("GET /v1/orgs/{orgId}/usage", requireAuth(requireOrgAdmin(http.HandlerFunc(orgHandler.Usage))))

	return authSvc, requireAuth, requirePlatformAdmin, orgRepo, requireMember
}
