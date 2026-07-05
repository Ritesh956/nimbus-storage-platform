package events

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"nimbus/internal/platform/httpserver"
)

// DLQHandler serves the admin dead-letter endpoints (backlog #9). Same
// authorization posture as GET /v1/admin/nodes: any authenticated user —
// there is no admin role in v1 (docs/01-srs.md non-goals: no real RBAC).
type DLQHandler struct {
	repo      *Repository
	publisher *Publisher
}

func NewDLQHandler(repo *Repository, publisher *Publisher) *DLQHandler {
	return &DLQHandler{repo: repo, publisher: publisher}
}

// List serves GET /v1/admin/dlq — newest first, capped at 100.
func (h *DLQHandler) List(w http.ResponseWriter, r *http.Request) {
	events, err := h.repo.ListDeadEvents(r.Context())
	if err != nil {
		httpserver.WriteError(w, r, httpserver.ErrInternal, "failed to list dead events")
		return
	}
	resp := make([]map[string]any, 0, len(events))
	for _, e := range events {
		entry := map[string]any{
			"id": e.ID, "subject": e.Subject, "payload": e.Payload, "error": e.Error,
			"deliveries": e.Deliveries, "status": e.Status, "created_at": e.CreatedAt,
		}
		if e.RetriedAt != nil {
			entry["retried_at"] = e.RetriedAt.Format(time.RFC3339)
		}
		resp = append(resp, entry)
	}
	httpserver.WriteJSON(w, http.StatusOK, map[string]any{"events": resp})
}

// Retry serves POST /v1/admin/dlq/{id}/retry — republishes the stored
// payload to its original subject and marks the row retried. The republish
// happens only after the dead→retried flip succeeds, so two concurrent
// retries can't both publish; if it then fails again downstream, the
// worker dead-letters it as a fresh row.
func (h *DLQHandler) Retry(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	evt, err := h.repo.GetDeadEvent(r.Context(), id)
	if err != nil {
		if errors.Is(err, ErrDeadEventNotFound) {
			httpserver.WriteError(w, r, httpserver.ErrNotFound, "dead event not found")
			return
		}
		httpserver.WriteError(w, r, httpserver.ErrInternal, "failed to load dead event")
		return
	}

	flipped, err := h.repo.MarkRetried(r.Context(), id)
	if err != nil {
		httpserver.WriteError(w, r, httpserver.ErrInternal, "failed to mark dead event retried")
		return
	}
	if !flipped {
		httpserver.WriteError(w, r, httpserver.ErrConflict, "dead event was already retried")
		return
	}

	if err := h.publisher.PublishRaw(r.Context(), evt.Subject, evt.Payload); err != nil {
		// Undo the flip so the row stays retryable — otherwise a NATS blip
		// here would strand it in 'retried' with nothing ever published.
		if revertErr := h.repo.RevertRetry(r.Context(), id); revertErr != nil {
			slog.Default().Error("republish failed and revert to dead failed — row stuck as retried", "id", id, "error", revertErr)
		}
		httpserver.WriteError(w, r, httpserver.ErrInternal, "republish failed — event left in the queue, retry later")
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, map[string]string{"status": "retried"})
}
