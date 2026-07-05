// Package live pushes server-side changes to connected browsers over SSE
// (post-v1 backlog #12, docs/06-api-design.md §10). One endpoint,
// GET /v1/orgs/{orgId}/events, relays two Redis pub/sub feeds as SSE frames:
//
//	activity     org-scoped domain events, published by activity.Service
//	             right after the Postgres insert (Publisher below). Because
//	             both nimbus-api ("uploaded") and nimbus-worker
//	             ("thumbnail_generated") funnel through activity.Service,
//	             hooking there covers cross-process events with no new
//	             coordination — Redis pub/sub is already the cross-process
//	             visibility layer (docs/07-distributed-architecture.md §2).
//	node_health  the storage router's existing health-transition channel
//	             (storage.HealthChangesChannel) — published on state *changes*
//	             only, which is exactly the "flip red live" signal the admin
//	             page wants.
//
// Frames are deliberately thin (a verb and a target, not full rows): the
// frontend treats them as revalidation signals for the SWR caches it
// already maintains, rather than a second source of truth to reconcile.
// Fan-out is one Redis subscription per connected client — fine at demo
// scale, and the first thing to revisit (shared in-process hub) if
// connection counts ever grew.
package live

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"nimbus/internal/storage"
)

const activityChannelPrefix = "nimbus:events:activity:"

// keepaliveInterval keeps intermediaries (and the client's reconnect logic)
// convinced the stream is alive between real events.
const keepaliveInterval = 25 * time.Second

// Publisher satisfies activity.LiveNotifier: it announces a just-recorded
// activity event on the org's Redis channel. Best-effort by design — a
// pub/sub hiccup must never fail the domain operation that triggered it.
type Publisher struct {
	rdb *redis.Client
}

func NewPublisher(rdb *redis.Client) *Publisher {
	return &Publisher{rdb: rdb}
}

func (p *Publisher) NotifyActivity(ctx context.Context, orgID, verb, targetType, targetID string) {
	payload, err := json.Marshal(map[string]string{"verb": verb, "target_type": targetType, "target_id": targetID})
	if err != nil {
		return
	}
	if err := p.rdb.Publish(ctx, activityChannelPrefix+orgID, payload).Err(); err != nil {
		slog.Default().Warn("failed to publish live activity event", "error", err, "org_id", orgID)
	}
}

type Handler struct {
	rdb *redis.Client
}

func NewHandler(rdb *redis.Client) *Handler {
	return &Handler{rdb: rdb}
}

// Stream is GET /v1/orgs/{orgId}/events. Auth/membership are enforced by
// the surrounding middleware chain, same as every other org-scoped route.
// The client is a fetch()-based reader, not EventSource — EventSource can't
// send an Authorization header and putting the token in the query string
// would leak it into request logs.
func (h *Handler) Stream(w http.ResponseWriter, r *http.Request) {
	orgID := r.PathValue("orgId")

	// The server's blanket read/write timeouts (httpserver.New) exist for
	// request/response traffic and would sever a long-lived stream; clear
	// both for this connection only.
	rc := http.NewResponseController(w)
	_ = rc.SetReadDeadline(time.Time{})
	_ = rc.SetWriteDeadline(time.Time{})

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)

	sub := h.rdb.Subscribe(r.Context(), activityChannelPrefix+orgID, storage.HealthChangesChannel)
	defer sub.Close()
	msgs := sub.Channel()

	// An immediate hello lets the client distinguish "connected" from
	// "request still pending" without waiting for a real event.
	fmt.Fprint(w, "event: hello\ndata: {}\n\n")
	if err := rc.Flush(); err != nil {
		return
	}

	ticker := time.NewTicker(keepaliveInterval)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			fmt.Fprint(w, ": keepalive\n\n")
			if err := rc.Flush(); err != nil {
				return // client gone
			}
		case msg, ok := <-msgs:
			if !ok {
				return
			}
			if msg.Channel == storage.HealthChangesChannel {
				node, status, _ := strings.Cut(msg.Payload, ":")
				data, _ := json.Marshal(map[string]string{"node": node, "status": status})
				fmt.Fprintf(w, "event: node_health\ndata: %s\n\n", data)
			} else {
				fmt.Fprintf(w, "event: activity\ndata: %s\n\n", msg.Payload)
			}
			if err := rc.Flush(); err != nil {
				return
			}
		}
	}
}
