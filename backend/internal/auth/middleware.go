package auth

import (
	"context"
	"net/http"
	"strings"

	"nimbus/internal/platform/httpserver"
)

type ctxKey int

const userIDKey ctxKey = iota

func WithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, userIDKey, userID)
}

// UserIDFromContext returns the authenticated user's ID, set by Middleware.
// Empty string means the request reached this point unauthenticated, which
// should only happen on routes not wrapped by Middleware.
func UserIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(userIDKey).(string)
	return id
}

// Middleware requires a valid, non-blacklisted Bearer access token and
// injects the user ID into the request context (docs/03-hld.md §2).
func Middleware(svc *Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := bearerToken(r)
			if !ok {
				httpserver.WriteError(w, r, httpserver.ErrUnauthorized, "missing bearer token")
				return
			}
			userID, err := svc.VerifyAccessToken(r.Context(), token)
			if err != nil {
				httpserver.WriteError(w, r, httpserver.ErrUnauthorized, "invalid or expired token")
				return
			}
			next.ServeHTTP(w, r.WithContext(WithUserID(r.Context(), userID)))
		})
	}
}

// RequirePlatformAdmin gates the /v1/admin/* cluster-ops routes (node
// health, hash ring, DLQ) — platform-scoped reads that were previously
// open to any authenticated user. Must run inside Middleware (it reads the
// user ID from context). Cluster ops is a deployment-level concern, so the
// check is a per-user flag, not an org role — org owners get org
// governance (GET /v1/orgs/{orgId}/usage), not cluster internals.
func RequirePlatformAdmin(repo *Repository) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u, err := repo.GetUserByID(r.Context(), UserIDFromContext(r.Context()))
			if err != nil || !u.IsPlatformAdmin {
				httpserver.WriteError(w, r, httpserver.ErrForbidden, "platform admin access required")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func bearerToken(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(h, prefix) {
		return "", false
	}
	token := strings.TrimSpace(strings.TrimPrefix(h, prefix))
	if token == "" {
		return "", false
	}
	return token, true
}
