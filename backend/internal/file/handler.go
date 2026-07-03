package file

import (
	"encoding/json"
	"errors"
	"net/http"

	"nimbus/internal/auth"
	"nimbus/internal/platform/httpserver"
)

type Handler struct {
	svc     *Service
	members MembershipChecker
}

func NewHandler(svc *Service, members MembershipChecker) *Handler {
	return &Handler{svc: svc, members: members}
}

// ListTrashed serves GET /v1/orgs/{orgId}/trash/files — the trash UI's
// file half (docs/09-roadmap.md Day 10).
func (h *Handler) ListTrashed(w http.ResponseWriter, r *http.Request) {
	orgID := r.PathValue("orgId")
	files, err := h.svc.ListTrashed(r.Context(), orgID)
	if err != nil {
		httpserver.WriteError(w, r, httpserver.ErrInternal, "failed to list trashed files")
		return
	}
	resp := make([]map[string]any, 0, len(files))
	for _, f := range files {
		resp = append(resp, toResponse(f))
	}
	httpserver.WriteJSON(w, http.StatusOK, resp)
}

// Update distinguishes "field omitted" from "field explicitly present" the
// same way folder.Handler.Update does — see that comment.
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	f, _ := FileFromContext(r.Context())

	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		httpserver.WriteError(w, r, httpserver.ErrInvalid, "malformed JSON body")
		return
	}

	var namePtr *string
	if v, ok := raw["name"]; ok {
		var name string
		if err := json.Unmarshal(v, &name); err != nil || name == "" {
			httpserver.WriteError(w, r, httpserver.ErrInvalid, "name must be a non-empty string")
			return
		}
		namePtr = &name
	}

	var folderPtr *string
	if v, ok := raw["folder_id"]; ok {
		var folderID string
		if err := json.Unmarshal(v, &folderID); err != nil || folderID == "" {
			httpserver.WriteError(w, r, httpserver.ErrInvalid, "folder_id must be a non-empty string")
			return
		}
		folderPtr = &folderID
	}

	updated, err := h.svc.Update(r.Context(), f, namePtr, folderPtr)
	if err != nil {
		writeFileError(w, r, err)
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, toResponse(updated))
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	f, _ := FileFromContext(r.Context())
	if err := h.svc.Delete(r.Context(), f.ID); err != nil {
		httpserver.WriteError(w, r, httpserver.ErrInternal, "failed to trash file")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) Restore(w http.ResponseWriter, r *http.Request) {
	f, ok := h.loadAndAuthorize(w, r)
	if !ok {
		return
	}
	if err := h.svc.Restore(r.Context(), f.ID); err != nil {
		httpserver.WriteError(w, r, httpserver.ErrInternal, "failed to restore file")
		return
	}
	restored, err := h.svc.GetAny(r.Context(), f.ID)
	if err != nil {
		httpserver.WriteError(w, r, httpserver.ErrInternal, "restored but failed to reload")
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, toResponse(restored))
}

func (h *Handler) Purge(w http.ResponseWriter, r *http.Request) {
	f, ok := h.loadAndAuthorize(w, r)
	if !ok {
		return
	}
	if err := h.svc.Purge(r.Context(), f); err != nil {
		writeFileError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// loadAndAuthorize is Restore/Purge's stand-in for RequireAccess: both
// target a possibly-trashed file, so they load via GetAny and check
// membership inline instead of going through the middleware.
func (h *Handler) loadAndAuthorize(w http.ResponseWriter, r *http.Request) (File, bool) {
	f, err := h.svc.GetAny(r.Context(), r.PathValue("fileId"))
	if err != nil {
		httpserver.WriteError(w, r, httpserver.ErrNotFound, "file not found")
		return File{}, false
	}
	userID := auth.UserIDFromContext(r.Context())
	ok, err := h.members.IsMember(r.Context(), f.OrgID, userID)
	if err != nil {
		httpserver.WriteError(w, r, httpserver.ErrInternal, "failed to check membership")
		return File{}, false
	}
	if !ok {
		httpserver.WriteError(w, r, httpserver.ErrForbidden, "not a member of this organization")
		return File{}, false
	}
	return f, true
}

func (h *Handler) ListVersions(w http.ResponseWriter, r *http.Request) {
	f, _ := FileFromContext(r.Context())
	versions, err := h.svc.ListVersions(r.Context(), f.ID)
	if err != nil {
		httpserver.WriteError(w, r, httpserver.ErrInternal, "failed to list versions")
		return
	}
	resp := make([]map[string]any, 0, len(versions))
	for _, v := range versions {
		resp = append(resp, map[string]any{
			"id": v.ID, "size_bytes": v.SizeBytes, "checksum_sha256": v.ChecksumSHA256,
			"mime_type": v.MimeType, "created_at": v.CreatedAt,
		})
	}
	httpserver.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) DownloadPlan(w http.ResponseWriter, r *http.Request) {
	f, _ := FileFromContext(r.Context())
	versionID := r.PathValue("versionId")

	plan, err := h.svc.DownloadPlan(r.Context(), f.ID, versionID)
	if err != nil {
		writeFileError(w, r, err)
		return
	}
	chunks := make([]map[string]any, len(plan))
	for i, c := range plan {
		chunks[i] = map[string]any{"sequence": c.Sequence, "hash": c.Hash, "targets": c.Targets}
	}
	httpserver.WriteJSON(w, http.StatusOK, map[string]any{"chunks": chunks})
}

func (h *Handler) RestoreVersion(w http.ResponseWriter, r *http.Request) {
	f, _ := FileFromContext(r.Context())
	versionID := r.PathValue("versionId")

	restored, err := h.svc.RestoreVersion(r.Context(), f, versionID)
	if err != nil {
		writeFileError(w, r, err)
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, toResponse(restored))
}

func writeFileError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		httpserver.WriteError(w, r, httpserver.ErrNotFound, "file not found")
	case errors.Is(err, ErrInvalidFolder):
		httpserver.WriteError(w, r, httpserver.ErrInvalid, "invalid target folder")
	case errors.Is(err, ErrNotTrashed):
		httpserver.WriteError(w, r, httpserver.ErrInvalid, "file must be trashed before it can be purged")
	case errors.Is(err, ErrVersionNotFound):
		httpserver.WriteError(w, r, httpserver.ErrNotFound, "version not found for this file")
	default:
		httpserver.WriteError(w, r, httpserver.ErrInternal, "file operation failed")
	}
}

func toResponse(f File) map[string]any {
	return map[string]any{
		"id": f.ID, "org_id": f.OrgID, "folder_id": f.FolderID, "name": f.Name,
		"latest_version_id": f.LatestVersionID, "created_at": f.CreatedAt, "updated_at": f.UpdatedAt,
	}
}
