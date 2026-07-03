package folder

import (
	"encoding/json"
	"errors"
	"net/http"

	"nimbus/internal/auth"
	"nimbus/internal/platform/httpserver"
)

type Handler struct {
	svc     *Service
	files   FileLister
	members MembershipChecker
}

func NewHandler(svc *Service, files FileLister, members MembershipChecker) *Handler {
	return &Handler{svc: svc, files: files, members: members}
}

type createFolderRequest struct {
	ParentID *string `json:"parent_id"`
	Name     string  `json:"name"`
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	orgID := r.PathValue("orgId")
	var req createFolderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		httpserver.WriteError(w, r, httpserver.ErrInvalid, "name is required")
		return
	}
	f, err := h.svc.Create(r.Context(), orgID, req.ParentID, req.Name)
	if err != nil {
		writeFolderError(w, r, err)
		return
	}
	httpserver.WriteJSON(w, http.StatusCreated, toResponse(f))
}

func (h *Handler) ListChildren(w http.ResponseWriter, r *http.Request) {
	f, _ := FolderFromContext(r.Context())

	folders, err := h.svc.ListChildren(r.Context(), f.OrgID, &f.ID)
	if err != nil {
		httpserver.WriteError(w, r, httpserver.ErrInternal, "failed to list child folders")
		return
	}
	files, err := h.files.ListInFolder(r.Context(), f.ID)
	if err != nil {
		httpserver.WriteError(w, r, httpserver.ErrInternal, "failed to list files")
		return
	}

	folderResp := make([]map[string]any, 0, len(folders))
	for _, c := range folders {
		folderResp = append(folderResp, toResponse(c))
	}
	fileResp := make([]map[string]any, 0, len(files))
	for _, fl := range files {
		fileResp = append(fileResp, map[string]any{"id": fl.ID, "name": fl.Name})
	}
	httpserver.WriteJSON(w, http.StatusOK, map[string]any{"folders": folderResp, "files": fileResp})
}

// Update distinguishes "field omitted" from "field explicitly null" (for
// parent_id, null means "move to root") by decoding into a raw map first —
// plain struct decoding can't tell those two cases apart.
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	f, _ := FolderFromContext(r.Context())

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

	var parentPtr **string
	if v, ok := raw["parent_id"]; ok {
		var parentVal *string
		if err := json.Unmarshal(v, &parentVal); err != nil {
			httpserver.WriteError(w, r, httpserver.ErrInvalid, "parent_id must be a string or null")
			return
		}
		parentPtr = &parentVal
	}

	updated, err := h.svc.Update(r.Context(), f, namePtr, parentPtr)
	if err != nil {
		writeFolderError(w, r, err)
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, toResponse(updated))
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	f, _ := FolderFromContext(r.Context())
	if err := h.svc.Delete(r.Context(), f.ID); err != nil {
		httpserver.WriteError(w, r, httpserver.ErrInternal, "failed to trash folder")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Restore targets an already-trashed folder, so it can't go through
// RequireAccess (which only loads non-deleted folders) — it loads via
// GetAny and checks membership inline instead.
func (h *Handler) Restore(w http.ResponseWriter, r *http.Request) {
	folderID := r.PathValue("folderId")
	f, err := h.svc.GetAny(r.Context(), folderID)
	if err != nil {
		httpserver.WriteError(w, r, httpserver.ErrNotFound, "folder not found")
		return
	}
	userID := auth.UserIDFromContext(r.Context())
	ok, err := h.members.IsMember(r.Context(), f.OrgID, userID)
	if err != nil {
		httpserver.WriteError(w, r, httpserver.ErrInternal, "failed to check membership")
		return
	}
	if !ok {
		httpserver.WriteError(w, r, httpserver.ErrForbidden, "not a member of this organization")
		return
	}
	if err := h.svc.Restore(r.Context(), f.ID); err != nil {
		httpserver.WriteError(w, r, httpserver.ErrInternal, "failed to restore folder")
		return
	}
	restored, err := h.svc.GetAny(r.Context(), folderID)
	if err != nil {
		httpserver.WriteError(w, r, httpserver.ErrInternal, "restored but failed to reload")
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, toResponse(restored))
}

func writeFolderError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		httpserver.WriteError(w, r, httpserver.ErrNotFound, "folder not found")
	case errors.Is(err, ErrNameConflict):
		httpserver.WriteError(w, r, httpserver.ErrConflict, "a folder with this name already exists here")
	case errors.Is(err, ErrInvalidParent):
		httpserver.WriteError(w, r, httpserver.ErrInvalid, "invalid parent folder")
	case errors.Is(err, ErrCyclicMove):
		httpserver.WriteError(w, r, httpserver.ErrInvalid, "cannot move a folder into itself or a descendant")
	default:
		httpserver.WriteError(w, r, httpserver.ErrInternal, "folder operation failed")
	}
}

func toResponse(f Folder) map[string]any {
	return map[string]any{
		"id": f.ID, "org_id": f.OrgID, "parent_id": f.ParentID, "name": f.Name,
		"created_at": f.CreatedAt, "updated_at": f.UpdatedAt,
	}
}
