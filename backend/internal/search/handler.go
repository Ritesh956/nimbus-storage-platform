package search

import (
	"net/http"
	"strconv"
	"time"

	"nimbus/internal/platform/httpserver"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Search serves GET /v1/orgs/{orgId}/search (docs/06-api-design.md §8).
func (h *Handler) Search(w http.ResponseWriter, r *http.Request) {
	orgID := r.PathValue("orgId")
	q := r.URL.Query()

	f := Filters{
		Query:   q.Get("q"),
		Type:    q.Get("type"),
		OwnerID: q.Get("owner"),
		Cursor:  q.Get("cursor"),
	}
	if v := q.Get("limit"); v != "" {
		f.Limit, _ = strconv.Atoi(v)
	}
	var err error
	if f.DateFrom, err = parseOptionalTime(q.Get("date_from")); err != nil {
		httpserver.WriteError(w, r, httpserver.ErrInvalid, "date_from must be RFC3339")
		return
	}
	if f.DateTo, err = parseOptionalTime(q.Get("date_to")); err != nil {
		httpserver.WriteError(w, r, httpserver.ErrInvalid, "date_to must be RFC3339")
		return
	}
	if f.SizeMin, err = parseOptionalInt64(q.Get("size_min")); err != nil {
		httpserver.WriteError(w, r, httpserver.ErrInvalid, "size_min must be an integer")
		return
	}
	if f.SizeMax, err = parseOptionalInt64(q.Get("size_max")); err != nil {
		httpserver.WriteError(w, r, httpserver.ErrInvalid, "size_max must be an integer")
		return
	}

	results, next, err := h.svc.Search(r.Context(), orgID, f)
	if err != nil {
		httpserver.WriteError(w, r, httpserver.ErrInternal, "search failed")
		return
	}

	resp := make([]map[string]any, 0, len(results))
	for _, res := range results {
		resp = append(resp, map[string]any{
			"file_id": res.FileID, "name": res.Name, "folder_id": res.FolderID, "owner_id": res.OwnerID,
			"created_at": res.CreatedAt, "size_bytes": res.SizeBytes, "mime_type": res.MimeType,
		})
	}
	httpserver.WriteJSON(w, http.StatusOK, map[string]any{"results": resp, "next_cursor": next})
}

func parseOptionalTime(v string) (*time.Time, error) {
	if v == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func parseOptionalInt64(v string) (*int64, error) {
	if v == "" {
		return nil, nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return nil, err
	}
	return &n, nil
}
