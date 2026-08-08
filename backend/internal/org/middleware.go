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
//
// The membership lookup is fronted by a short-lived in-process cache
// (membership_cache.go, audit §05) shared across every route this
// particular middleware instance gates — one RequireRole call per
// (repo, minRole) pair at wiring time (see wire_auth.go), not per-request.
func RequireRole(repo *Repository, minRole Role) func(http.Handler) http.Handler {
	cache := newMembershipCache()
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID := auth.UserIDFromContext(r.Context())
			orgID := r.PathValue("orgId")

			m, ok := cache.get(orgID, userID)
			if !ok {
				var err error
				m, err = repo.GetMembership(r.Context(), orgID, userID)
				if err != nil {
					httpserver.WriteError(w, r, httpserver.ErrForbidden, "not a member of this organization")
					return
				}
				cache.set(orgID, userID, m)
			}
			if roleRank(m.Role) < roleRank(minRole) {
				httpserver.WriteError(w, r, httpserver.ErrForbidden, string(minRole)+" role required")
				return
			}
			next.ServeHTTP(w, r.WithContext(WithMembership(r.Context(), m)))
		})
	}
}
