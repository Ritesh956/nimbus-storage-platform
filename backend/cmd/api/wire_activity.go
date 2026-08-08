package main

// Activity feed + live (SSE) wiring — small enough to be its own file
// rather than folded into a bigger concern, since both routes are thin
// reads over one Redis pub/sub relay (internal/live) that upload wiring
// also needs a producer handle to (activitySvc, returned below).

import (
	"net/http"

	"github.com/redis/go-redis/v9"

	"nimbus/internal/activity"
	"nimbus/internal/live"
)

// wireActivity registers the org activity feed and SSE event stream routes,
// returning activity.Service — upload wiring passes it in as the
// ActivityRecorder for synchronous "uploaded" event writes.
func wireActivity(
	mux *http.ServeMux,
	rdb *redis.Client,
	requireAuth, requireMember func(http.Handler) http.Handler,
	activityRepo *activity.Repository,
) (activitySvc *activity.Service) {
	activitySvc = activity.NewService(activityRepo, live.NewPublisher(rdb))
	activityHandler := activity.NewHandler(activitySvc)
	liveHandler := live.NewHandler(rdb)

	mux.Handle("GET /v1/orgs/{orgId}/activity", requireAuth(requireMember(http.HandlerFunc(activityHandler.List))))
	mux.Handle("GET /v1/orgs/{orgId}/events", requireAuth(requireMember(http.HandlerFunc(liveHandler.Stream))))

	return activitySvc
}
