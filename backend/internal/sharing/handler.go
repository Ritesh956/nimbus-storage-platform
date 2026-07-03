package sharing

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"nimbus/internal/auth"
	"nimbus/internal/file"
	"nimbus/internal/platform/httpserver"
)

// MembershipChecker authorizes DELETE /v1/shares/{token} — see
// FileOrgLookup's doc comment for why this can't go through
// file.RequireAccess.
type MembershipChecker interface {
	IsMember(ctx context.Context, orgID, userID string) (bool, error)
}

type Handler struct {
	svc     *Service
	members MembershipChecker
}

func NewHandler(svc *Service, members MembershipChecker) *Handler {
	return &Handler{svc: svc, members: members}
}

type createShareRequest struct {
	ExpiresAt *string `json:"expires_at"`
}

// Create serves POST /v1/files/{fileId}/share — authorized by
// file.RequireAccess (org membership on the file already checked, file
// loaded into context) before this handler runs.
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	f, _ := file.FileFromContext(r.Context())

	var req createShareRequest
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpserver.WriteError(w, r, httpserver.ErrInvalid, "malformed JSON body")
			return
		}
	}
	var expiresAt *time.Time
	if req.ExpiresAt != nil {
		t, err := time.Parse(time.RFC3339, *req.ExpiresAt)
		if err != nil {
			httpserver.WriteError(w, r, httpserver.ErrInvalid, "expires_at must be RFC3339")
			return
		}
		expiresAt = &t
	}

	userID := auth.UserIDFromContext(r.Context())
	link, err := h.svc.CreateShare(r.Context(), f.ID, userID, expiresAt)
	if err != nil {
		httpserver.WriteError(w, r, httpserver.ErrInternal, "failed to create share link")
		return
	}

	httpserver.WriteJSON(w, http.StatusCreated, map[string]string{
		"token": link.Token,
		"url":   shareURL(r, link.Token),
	})
}

// Resolve serves GET /v1/shares/{token} — deliberately not behind
// auth.Middleware; that's the entire point of a share link
// (docs/06-api-design.md §7).
func (h *Handler) Resolve(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	resolved, err := h.svc.Resolve(r.Context(), token)
	if err != nil {
		writeShareError(w, r, err)
		return
	}

	chunks := make([]map[string]any, len(resolved.DownloadPlan))
	for i, c := range resolved.DownloadPlan {
		chunks[i] = map[string]any{"sequence": c.Sequence, "hash": c.Hash, "targets": c.Targets}
	}
	httpserver.WriteJSON(w, http.StatusOK, map[string]any{
		"file": map[string]any{
			"id": resolved.File.ID, "name": resolved.File.Name,
			"size_bytes": resolved.File.SizeBytes, "mime_type": resolved.File.MimeType,
			"checksum_sha256": resolved.File.ChecksumSHA256,
		},
		"download_plan": map[string]any{"chunks": chunks},
	})
}

// Delete serves DELETE /v1/shares/{token}. There's no {fileId} in this
// route, so authorization is done inline here (load the link, resolve its
// file's org, check membership) rather than via file.RequireAccess.
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")

	link, err := h.svc.GetLinkByToken(r.Context(), token)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			w.WriteHeader(http.StatusNoContent) // already gone — deleting a nonexistent link isn't an error
			return
		}
		httpserver.WriteError(w, r, httpserver.ErrInternal, "failed to load share link")
		return
	}

	orgID, err := h.svc.FileOrgID(r.Context(), link.FileID)
	if err != nil {
		httpserver.WriteError(w, r, httpserver.ErrInternal, "failed to resolve file organization")
		return
	}
	userID := auth.UserIDFromContext(r.Context())
	ok, err := h.members.IsMember(r.Context(), orgID, userID)
	if err != nil {
		httpserver.WriteError(w, r, httpserver.ErrInternal, "failed to check membership")
		return
	}
	if !ok {
		httpserver.WriteError(w, r, httpserver.ErrForbidden, "not a member of this organization")
		return
	}

	if err := h.svc.Revoke(r.Context(), token); err != nil {
		httpserver.WriteError(w, r, httpserver.ErrInternal, "failed to revoke share link")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func shareURL(r *http.Request, token string) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	}
	return scheme + "://" + r.Host + "/v1/shares/" + token
}

func writeShareError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		httpserver.WriteError(w, r, httpserver.ErrNotFound, "share link not found")
	case errors.Is(err, ErrExpired):
		httpserver.WriteError(w, r, httpserver.ErrForbidden, "share link has expired")
	case errors.Is(err, ErrFileHasNoVersion):
		httpserver.WriteError(w, r, httpserver.ErrConflict, "shared file has no version to serve")
	default:
		httpserver.WriteError(w, r, httpserver.ErrInternal, "failed to resolve share link")
	}
}
