package upload

import (
	"context"
	"errors"
	"net/http"

	"nimbus/internal/auth"
	"nimbus/internal/platform/httpserver"
)

type ctxKey int

const uploadKey ctxKey = iota

func WithUpload(ctx context.Context, u Upload) context.Context {
	return context.WithValue(ctx, uploadKey, u)
}

func UploadFromContext(ctx context.Context) (Upload, bool) {
	u, ok := ctx.Value(uploadKey).(Upload)
	return u, ok
}

// RequireAccess loads the upload named by {uploadId}, 404s if missing,
// 403s if the caller isn't a member of its org, then stores it in context.
func RequireAccess(repo *Repository, members MembershipChecker) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u, err := repo.GetUpload(r.Context(), r.PathValue("uploadId"))
			if err != nil {
				if errors.Is(err, ErrUploadNotFound) {
					httpserver.WriteError(w, r, httpserver.ErrNotFound, "upload not found")
					return
				}
				httpserver.WriteError(w, r, httpserver.ErrInternal, "failed to load upload")
				return
			}
			userID := auth.UserIDFromContext(r.Context())
			ok, err := members.IsMember(r.Context(), u.OrgID, userID)
			if err != nil {
				httpserver.WriteError(w, r, httpserver.ErrInternal, "failed to check membership")
				return
			}
			if !ok {
				httpserver.WriteError(w, r, httpserver.ErrForbidden, "not a member of this organization")
				return
			}
			next.ServeHTTP(w, r.WithContext(WithUpload(r.Context(), u)))
		})
	}
}
