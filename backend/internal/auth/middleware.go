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
