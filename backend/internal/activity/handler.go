package activity

import (
	"net/http"
	"strconv"

	"nimbus/internal/platform/httpserver"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// List serves GET /v1/orgs/{orgId}/activity (docs/06-api-design.md §8).
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	orgID := r.PathValue("orgId")
	cursor := r.URL.Query().Get("cursor")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit")) // 0 on parse failure -> service default

	events, next, err := h.svc.List(r.Context(), orgID, cursor, limit)
	if err != nil {
		httpserver.WriteError(w, r, httpserver.ErrInternal, "failed to list activity")
		return
	}

	resp := make([]map[string]any, 0, len(events))
	for _, e := range events {
		resp = append(resp, map[string]any{
			"verb": e.Verb, "target_type": e.TargetType, "target_id": e.TargetID,
			"actor": e.ActorUserID, "created_at": e.CreatedAt,
		})
	}
	httpserver.WriteJSON(w, http.StatusOK, map[string]any{"events": resp, "next_cursor": next})
}
