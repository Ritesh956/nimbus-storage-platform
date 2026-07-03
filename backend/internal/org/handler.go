package org

import (
	"encoding/json"
	"errors"
	"net/http"

	"nimbus/internal/auth"
	"nimbus/internal/platform/httpserver"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

type createOrgRequest struct {
	Name string `json:"name"`
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var req createOrgRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		httpserver.WriteError(w, r, httpserver.ErrInvalid, "name is required")
		return
	}
	userID := auth.UserIDFromContext(r.Context())
	o, err := h.svc.Create(r.Context(), req.Name, userID)
	if err != nil {
		httpserver.WriteError(w, r, httpserver.ErrInternal, "failed to create organization")
		return
	}
	httpserver.WriteJSON(w, http.StatusCreated, map[string]string{
		"id": o.ID, "name": o.Name, "owner_user_id": o.OwnerUserID,
	})
}

func (h *Handler) ListMembers(w http.ResponseWriter, r *http.Request) {
	orgID := r.PathValue("orgId")
	members, err := h.svc.ListMembers(r.Context(), orgID)
	if err != nil {
		httpserver.WriteError(w, r, httpserver.ErrInternal, "failed to list members")
		return
	}
	resp := make([]map[string]string, 0, len(members))
	for _, m := range members {
		resp = append(resp, map[string]string{"user_id": m.UserID, "email": m.Email, "role": string(m.Role)})
	}
	httpserver.WriteJSON(w, http.StatusOK, resp)
}

type addMemberRequest struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

func (h *Handler) AddMember(w http.ResponseWriter, r *http.Request) {
	orgID := r.PathValue("orgId")
	var req addMemberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Email == "" {
		httpserver.WriteError(w, r, httpserver.ErrInvalid, "email is required")
		return
	}
	role := Role(req.Role)
	if role != RoleOwner && role != RoleMember {
		role = RoleMember
	}

	m, err := h.svc.AddMemberByEmail(r.Context(), orgID, req.Email, role)
	if err != nil {
		switch {
		case errors.Is(err, ErrTargetUserNotFound):
			httpserver.WriteError(w, r, httpserver.ErrNotFound, "user not found")
		case errors.Is(err, ErrAlreadyMember):
			httpserver.WriteError(w, r, httpserver.ErrConflict, "user is already a member")
		default:
			httpserver.WriteError(w, r, httpserver.ErrInternal, "failed to add member")
		}
		return
	}
	httpserver.WriteJSON(w, http.StatusCreated, map[string]string{
		"org_id": orgID, "user_id": m.UserID, "role": string(m.Role),
	})
}

func (h *Handler) RemoveMember(w http.ResponseWriter, r *http.Request) {
	orgID := r.PathValue("orgId")
	userID := r.PathValue("userId")
	if err := h.svc.RemoveMember(r.Context(), orgID, userID); err != nil {
		if errors.Is(err, ErrCannotRemoveOwner) {
			httpserver.WriteError(w, r, httpserver.ErrConflict, "cannot remove the organization owner")
			return
		}
		httpserver.WriteError(w, r, httpserver.ErrInternal, "failed to remove member")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
