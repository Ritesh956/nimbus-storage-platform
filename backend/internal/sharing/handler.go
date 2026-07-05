package sharing

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"nimbus/internal/auth"
	"nimbus/internal/file"
	"nimbus/internal/folder"
	"nimbus/internal/platform/httpserver"
)

// MembershipChecker authorizes DELETE /v1/shares/{token} — that route has
// no {fileId}/{folderId}/{orgId} in its path, so it can't go through the
// usual access middleware; the link's own org_id is checked inline.
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
	ExpiresAt *string  `json:"expires_at"`
	FileIDs   []string `json:"file_ids"` // bundle creation only (POST /v1/orgs/{orgId}/shares)
}

func parseCreateShare(w http.ResponseWriter, r *http.Request) (createShareRequest, *time.Time, bool) {
	var req createShareRequest
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpserver.WriteError(w, r, httpserver.ErrInvalid, "malformed JSON body")
			return req, nil, false
		}
	}
	var expiresAt *time.Time
	if req.ExpiresAt != nil {
		t, err := time.Parse(time.RFC3339, *req.ExpiresAt)
		if err != nil {
			httpserver.WriteError(w, r, httpserver.ErrInvalid, "expires_at must be RFC3339")
			return req, nil, false
		}
		expiresAt = &t
	}
	return req, expiresAt, true
}

// Create serves POST /v1/files/{fileId}/share — authorized by
// file.RequireAccess (org membership on the file already checked, file
// loaded into context) before this handler runs.
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	f, _ := file.FileFromContext(r.Context())
	_, expiresAt, ok := parseCreateShare(w, r)
	if !ok {
		return
	}

	userID := auth.UserIDFromContext(r.Context())
	link, err := h.svc.CreateShare(r.Context(), f.OrgID, f.ID, userID, expiresAt)
	if err != nil {
		httpserver.WriteError(w, r, httpserver.ErrInternal, "failed to create share link")
		return
	}
	writeCreatedLink(w, r, link)
}

// CreateFolder serves POST /v1/folders/{folderId}/share — authorized by
// folder.RequireAccess, mirroring Create above.
func (h *Handler) CreateFolder(w http.ResponseWriter, r *http.Request) {
	f, _ := folder.FolderFromContext(r.Context())
	_, expiresAt, ok := parseCreateShare(w, r)
	if !ok {
		return
	}

	userID := auth.UserIDFromContext(r.Context())
	link, err := h.svc.CreateFolderShare(r.Context(), f.OrgID, f.ID, userID, expiresAt)
	if err != nil {
		httpserver.WriteError(w, r, httpserver.ErrInternal, "failed to create share link")
		return
	}
	writeCreatedLink(w, r, link)
}

// CreateBundle serves POST /v1/orgs/{orgId}/shares — one link covering a
// hand-picked set of files (org membership checked by the route's
// requireMember middleware).
func (h *Handler) CreateBundle(w http.ResponseWriter, r *http.Request) {
	orgID := r.PathValue("orgId")
	req, expiresAt, ok := parseCreateShare(w, r)
	if !ok {
		return
	}

	userID := auth.UserIDFromContext(r.Context())
	link, err := h.svc.CreateBundleShare(r.Context(), orgID, req.FileIDs, userID, expiresAt)
	if err != nil {
		switch {
		case errors.Is(err, ErrEmptyBundle):
			httpserver.WriteError(w, r, httpserver.ErrInvalid, "file_ids must name at least one file")
		case errors.Is(err, ErrFileNotShareable):
			httpserver.WriteError(w, r, httpserver.ErrInvalid, "every file must exist, be out of trash, and belong to this organization")
		default:
			httpserver.WriteError(w, r, httpserver.ErrInternal, "failed to create share link")
		}
		return
	}
	writeCreatedLink(w, r, link)
}

// Resolve serves GET /v1/shares/{token} — deliberately not behind
// auth.Middleware; that's the entire point of a share link
// (docs/06-api-design.md §7). The response shape is discriminated by
// "kind"; the single-file shape keeps its original fields.
func (h *Handler) Resolve(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	resolved, err := h.svc.Resolve(r.Context(), token)
	if err != nil {
		writeShareError(w, r, err)
		return
	}

	switch resolved.Kind {
	case KindFile:
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{
			"kind":          KindFile,
			"file":          fileJSON(resolved.File),
			"download_plan": map[string]any{"chunks": chunksJSON(resolved.DownloadPlan)},
		})
	case KindFolder:
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{
			"kind":    KindFolder,
			"folder":  map[string]any{"id": resolved.Folder.ID, "name": resolved.Folder.Name},
			"folders": foldersJSON(resolved.Subfolders),
			"files":   filesJSON(resolved.Files),
		})
	default:
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{
			"kind":  KindBundle,
			"files": filesJSON(resolved.Files),
		})
	}
}

// Children serves GET /v1/shares/{token}/folders/{folderId} (public) —
// navigation inside a folder share.
func (h *Handler) Children(w http.ResponseWriter, r *http.Request) {
	f, subfolders, files, err := h.svc.ResolveFolderChildren(r.Context(), r.PathValue("token"), r.PathValue("folderId"))
	if err != nil {
		writeShareError(w, r, err)
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, map[string]any{
		"folder":  map[string]any{"id": f.ID, "name": f.Name},
		"folders": foldersJSON(subfolders),
		"files":   filesJSON(files),
	})
}

// DownloadPlan serves GET /v1/shares/{token}/files/{fileId}/download-plan
// (public) — per-file download inside a folder or bundle share.
func (h *Handler) DownloadPlan(w http.ResponseWriter, r *http.Request) {
	info, plan, err := h.svc.ResolveDownloadPlan(r.Context(), r.PathValue("token"), r.PathValue("fileId"))
	if err != nil {
		writeShareError(w, r, err)
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, map[string]any{
		"file":          fileJSON(info),
		"download_plan": map[string]any{"chunks": chunksJSON(plan)},
	})
}

// Delete serves DELETE /v1/shares/{token}, authorized against the link's
// own org (see MembershipChecker).
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

	userID := auth.UserIDFromContext(r.Context())
	ok, err := h.members.IsMember(r.Context(), link.OrgID, userID)
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

func writeCreatedLink(w http.ResponseWriter, r *http.Request, link ShareLink) {
	httpserver.WriteJSON(w, http.StatusCreated, map[string]string{
		"token": link.Token,
		"url":   shareURL(r, link.Token),
	})
}

func fileJSON(f FileInfo) map[string]any {
	return map[string]any{
		"id": f.ID, "name": f.Name,
		"size_bytes": f.SizeBytes, "mime_type": f.MimeType,
		"checksum_sha256": f.ChecksumSHA256,
	}
}

func filesJSON(files []FileInfo) []map[string]any {
	out := make([]map[string]any, len(files))
	for i, f := range files {
		out[i] = fileJSON(f)
	}
	return out
}

func foldersJSON(folders []FolderInfo) []map[string]any {
	out := make([]map[string]any, len(folders))
	for i, f := range folders {
		out[i] = map[string]any{"id": f.ID, "name": f.Name}
	}
	return out
}

func chunksJSON(plan []file.DownloadPlanChunk) []map[string]any {
	out := make([]map[string]any, len(plan))
	for i, c := range plan {
		out[i] = map[string]any{"sequence": c.Sequence, "hash": c.Hash, "targets": c.Targets}
	}
	return out
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
	case errors.Is(err, ErrNotInShare):
		// Same 404 whether the item exists outside the share or not at all —
		// a token holder can't probe org contents.
		httpserver.WriteError(w, r, httpserver.ErrNotFound, "no such item in this share")
	case errors.Is(err, ErrExpired):
		httpserver.WriteError(w, r, httpserver.ErrForbidden, "share link has expired")
	case errors.Is(err, ErrFileHasNoVersion):
		httpserver.WriteError(w, r, httpserver.ErrConflict, "shared file has no version to serve")
	default:
		httpserver.WriteError(w, r, httpserver.ErrInternal, "failed to resolve share link")
	}
}
