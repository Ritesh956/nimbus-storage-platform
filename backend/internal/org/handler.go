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

// ListMine serves GET /v1/orgs — the orgs the current user belongs to.
func (h *Handler) ListMine(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())
	orgs, err := h.svc.ListForUser(r.Context(), userID)
	if err != nil {
		httpserver.WriteError(w, r, httpserver.ErrInternal, "failed to list organizations")
		return
	}
	resp := make([]map[string]string, 0, len(orgs))
	for _, o := range orgs {
		resp = append(resp, map[string]string{"id": o.ID, "name": o.Name, "owner_user_id": o.OwnerUserID})
	}
	httpserver.WriteJSON(w, http.StatusOK, resp)
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
	if role != RoleOwner && role != RoleAdmin && role != RoleMember {
		role = RoleMember
	}
	caller, _ := MembershipFromContext(r.Context())

	m, err := h.svc.AddMemberByEmail(r.Context(), orgID, req.Email, role, caller.Role)
	if err != nil {
		switch {
		case errors.Is(err, ErrTargetUserNotFound):
			httpserver.WriteError(w, r, httpserver.ErrNotFound, "user not found")
		case errors.Is(err, ErrAlreadyMember):
			httpserver.WriteError(w, r, httpserver.ErrConflict, "user is already a member")
		case errors.Is(err, ErrElevatedRoleNeedsOwner):
			httpserver.WriteError(w, r, httpserver.ErrForbidden, err.Error())
		default:
			httpserver.WriteError(w, r, httpserver.ErrInternal, "failed to add member")
		}
		return
	}
	httpserver.WriteJSON(w, http.StatusCreated, map[string]string{
		"org_id": orgID, "user_id": m.UserID, "role": string(m.Role),
	})
}

// Usage serves GET /v1/orgs/{orgId}/usage — owner-gated (RequireRole
// RoleOwner in main.go) org oversight; see usage.go for the privacy line.
func (h *Handler) Usage(w http.ResponseWriter, r *http.Request) {
	u, err := h.svc.Usage(r.Context(), r.PathValue("orgId"))
	if err != nil {
		httpserver.WriteError(w, r, httpserver.ErrInternal, "failed to compute organization usage")
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, u)
}

type setQuotaRequest struct {
	// nil (omitted or explicit JSON null) clears the override, falling back
	// to the configured default — matches Repository.SetQuotaOverride.
	QuotaBytes *int64 `json:"quota_bytes"`
}

// SetQuota serves PATCH /v1/orgs/{orgId}/quota — platform-admin only
// (RequirePlatformAdmin in main.go, a stricter tier than Usage's org-owner
// gate above): a cross-tenant limit override is a platform operation, not
// something an org's own owner should be able to grant itself (audit §06's
// per-tenant quota gap).
func (h *Handler) SetQuota(w http.ResponseWriter, r *http.Request) {
	orgID := r.PathValue("orgId")
	var req setQuotaRequest
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpserver.WriteError(w, r, httpserver.ErrInvalid, "malformed JSON body")
			return
		}
	}
	if req.QuotaBytes != nil && *req.QuotaBytes <= 0 {
		httpserver.WriteError(w, r, httpserver.ErrInvalid, "quota_bytes must be positive, or omitted/null to clear the override")
		return
	}
	if err := h.svc.SetQuota(r.Context(), orgID, req.QuotaBytes); err != nil {
		if errors.Is(err, ErrNotFound) {
			httpserver.WriteError(w, r, httpserver.ErrNotFound, "organization not found")
			return
		}
		httpserver.WriteError(w, r, httpserver.ErrInternal, "failed to set organization quota")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) RemoveMember(w http.ResponseWriter, r *http.Request) {
	orgID := r.PathValue("orgId")
	userID := r.PathValue("userId")
	caller, _ := MembershipFromContext(r.Context())
	if err := h.svc.RemoveMember(r.Context(), orgID, userID, caller.Role); err != nil {
		switch {
		case errors.Is(err, ErrCannotRemoveOwner):
			httpserver.WriteError(w, r, httpserver.ErrConflict, "cannot remove the organization owner")
		case errors.Is(err, ErrAdminRemovesMembersOnly):
			httpserver.WriteError(w, r, httpserver.ErrForbidden, err.Error())
		default:
			httpserver.WriteError(w, r, httpserver.ErrInternal, "failed to remove member")
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
