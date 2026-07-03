package folder

import (
	"context"
	"errors"
	"net/http"

	"nimbus/internal/auth"
	"nimbus/internal/platform/httpserver"
)

type ctxKey int

const folderKey ctxKey = iota

func WithFolder(ctx context.Context, f Folder) context.Context {
	return context.WithValue(ctx, folderKey, f)
}

func FolderFromContext(ctx context.Context) (Folder, bool) {
	f, ok := ctx.Value(folderKey).(Folder)
	return f, ok
}

// RequireAccess loads the folder named by {folderId} (must not be trashed),
// 404s if missing, 403s if the caller isn't a member of its org, then
// stores it in context so the handler doesn't re-fetch it. Routes that
// target a trashed folder (Restore) can't use this — see handler.go.
func RequireAccess(repo *Repository, members MembershipChecker) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			f, err := repo.Get(r.Context(), r.PathValue("folderId"))
			if err != nil {
				if errors.Is(err, ErrNotFound) {
					httpserver.WriteError(w, r, httpserver.ErrNotFound, "folder not found")
					return
				}
				httpserver.WriteError(w, r, httpserver.ErrInternal, "failed to load folder")
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
			next.ServeHTTP(w, r.WithContext(WithFolder(r.Context(), f)))
		})
	}
}
