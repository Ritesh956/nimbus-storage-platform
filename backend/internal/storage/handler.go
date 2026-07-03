package storage

import (
	"net/http"

	"nimbus/internal/platform/httpserver"
)

type Handler struct {
	repo *Repository
}

func NewHandler(repo *Repository) *Handler {
	return &Handler{repo: repo}
}

// ListNodes serves GET /v1/admin/nodes (docs/06-api-design.md §9) — this is
// what's on screen during the chaos demo.
func (h *Handler) ListNodes(w http.ResponseWriter, r *http.Request) {
	nodes, err := h.repo.ListNodes(r.Context())
	if err != nil {
		httpserver.WriteError(w, r, httpserver.ErrInternal, "failed to list storage nodes")
		return
	}
	resp := make([]map[string]any, 0, len(nodes))
	for _, n := range nodes {
		resp = append(resp, map[string]any{
			"id":                n.ID,
			"endpoint":          n.Endpoint,
			"status":            n.Status,
			"last_heartbeat_at": n.LastHeartbeatAt,
		})
	}
	httpserver.WriteJSON(w, http.StatusOK, resp)
}
