package file

import (
	"context"
	"errors"
	"net/http"

	"nimbus/internal/auth"
	"nimbus/internal/platform/httpserver"
)

// MembershipChecker is the port file uses to authorize access without
// importing org's data-access layer directly (docs/03-hld.md §1).
type MembershipChecker interface {
	IsMember(ctx context.Context, orgID, userID string) (bool, error)
}

type ctxKey int

const fileKey ctxKey = iota

func WithFile(ctx context.Context, f File) context.Context {
	return context.WithValue(ctx, fileKey, f)
}

func FileFromContext(ctx context.Context) (File, bool) {
	f, ok := ctx.Value(fileKey).(File)
	return f, ok
}

// RequireAccess loads the file named by {fileId} (must not be trashed),
// 404s if missing, 403s if the caller isn't a member of its org. Routes
// targeting a trashed file (Restore, Purge) can't use this — see
// handler.go.
func RequireAccess(repo *Repository, members MembershipChecker) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			f, err := repo.Get(r.Context(), r.PathValue("fileId"))
			if err != nil {
				if errors.Is(err, ErrNotFound) {
					httpserver.WriteError(w, r, httpserver.ErrNotFound, "file not found")
					return
				}
				httpserver.WriteError(w, r, httpserver.ErrInternal, "failed to load file")
				return
			}
			userID := auth.UserIDFromContext(r.Context())
			ok, err := members.IsMember(r.Context(), f.OrgID, userID)
			if err != nil {
				httpserver.WriteError(w, r, httpserver.ErrInternal, "failed to check membership")
				return
			}
			if !ok {
				httpserver.WriteError(w, r, httpserver.ErrForbidden, "not a member of this organization")
				return
			}
			next.ServeHTTP(w, r.WithContext(WithFile(r.Context(), f)))
		})
	}
}
