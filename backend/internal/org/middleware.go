package org

import (
	"context"
	"net/http"

	"nimbus/internal/auth"
	"nimbus/internal/platform/httpserver"
)

type ctxKey int

const membershipKey ctxKey = iota

func WithMembership(ctx context.Context, m Member) context.Context {
	return context.WithValue(ctx, membershipKey, m)
}

func MembershipFromContext(ctx context.Context) (Member, bool) {
	m, ok := ctx.Value(membershipKey).(Member)
	return m, ok
}

// RequireRole resolves {orgId} from the route and the caller's user ID from
// context (set by auth.Middleware, which must run first), checks
// membership, and enforces a minimum role on the owner > admin > member
// ladder. Any membership at all satisfies RoleMember.
func RequireRole(repo *Repository, minRole Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID := auth.UserIDFromContext(r.Context())
			orgID := r.PathValue("orgId")

			m, err := repo.GetMembership(r.Context(), orgID, userID)
			if err != nil {
				httpserver.WriteError(w, r, httpserver.ErrForbidden, "not a member of this organization")
				return
			}
			if roleRank(m.Role) < roleRank(minRole) {
				httpserver.WriteError(w, r, httpserver.ErrForbidden, string(minRole)+" role required")
				return
			}
			next.ServeHTTP(w, r.WithContext(WithMembership(r.Context(), m)))
		})
	}
}
