package storage

import (
	"context"
	"net/http"

	"nimbus/internal/platform/httpserver"
)

// ChunkRef is one chunk of a file's latest version, as the ring view needs
// it (sequence for labeling, hash for ring position + location lookups).
type ChunkRef struct {
	Sequence int
	Hash     string
}

// FileChunkResolver is the port the ring view uses to resolve a file's
// latest version's chunk list without storage importing file's data-access
// layer (docs/03-hld.md §1) — satisfied by an adapter over file.Repository
// in cmd/api/main.go.
type FileChunkResolver interface {
	LatestVersionChunks(ctx context.Context, fileID string) ([]ChunkRef, error)
}

type Handler struct {
	repo              *Repository
	router            *Router
	fileChunks        FileChunkResolver
	replicationFactor int
}

func NewHandler(repo *Repository, router *Router, fileChunks FileChunkResolver, replicationFactor int) *Handler {
	return &Handler{repo: repo, router: router, fileChunks: fileChunks, replicationFactor: replicationFactor}
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

// Ring serves GET /v1/admin/ring[?file_id=] (backlog #13): the consistent-
// hash ring's vnode table, and — when file_id is given — where that file's
// latest version's chunks sit on the ring, both as the ring would place
// them today (preference order, health-ignoring) and where they were
// actually committed (recorded chunk_locations). The two can legitimately
// differ after a failover write, which is exactly what makes them worth
// showing side by side.
func (h *Handler) Ring(w http.ResponseWriter, r *http.Request) {
	vnodes := h.router.RingSnapshot()
	vnodesResp := make([]map[string]any, len(vnodes))
	for i, vn := range vnodes {
		vnodesResp[i] = map[string]any{"position": vn.Position, "node": string(vn.Node)}
	}
	resp := map[string]any{
		"vnodes":             vnodesResp,
		"replication_factor": h.replicationFactor,
	}

	if fileID := r.URL.Query().Get("file_id"); fileID != "" {
		chunks, err := h.fileChunks.LatestVersionChunks(r.Context(), fileID)
		if err != nil {
			httpserver.WriteError(w, r, httpserver.ErrNotFound, "file not found")
			return
		}
		chunksResp := make([]map[string]any, len(chunks))
		for i, c := range chunks {
			locations, err := h.repo.LocationsForChunk(r.Context(), c.Hash)
			if err != nil {
				httpserver.WriteError(w, r, httpserver.ErrInternal, "failed to resolve chunk locations")
				return
			}
			preference, _ := h.router.Candidates(c.Hash) // health-ignoring full ring order; error only when no nodes are configured
			// Non-nil even when empty — nil slices marshal to JSON null,
			// not [] (the FindMissingChunks lesson, CLAUDE.md).
			if locations == nil {
				locations = []NodeID{}
			}
			if preference == nil {
				preference = []NodeID{}
			}
			chunksResp[i] = map[string]any{
				"sequence":   c.Sequence,
				"hash":       c.Hash,
				"position":   PositionOf(c.Hash),
				"preference": preference,
				"locations":  locations,
			}
		}
		resp["chunks"] = chunksResp
	}

	httpserver.WriteJSON(w, http.StatusOK, resp)
}

// Repair serves POST /v1/admin/nodes/{nodeId}/repair (audit §02 — "recovery
// restores routing but not necessarily the replica count"): re-verifies
// every chunk this node is recorded as holding, restoring any physically
// missing from a surviving replica. Manual-trigger, synchronous — the
// response is the pass's own result, not a job handle, since there's no
// background queue for this (see repair.go's doc comment for why automatic
// isn't done here).
func (h *Handler) Repair(w http.ResponseWriter, r *http.Request) {
	nodeID := NodeID(r.PathValue("nodeId"))
	result, err := h.router.RepairNode(r.Context(), nodeID)
	if err != nil {
		httpserver.WriteError(w, r, httpserver.ErrNotFound, err.Error())
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, result)
}
