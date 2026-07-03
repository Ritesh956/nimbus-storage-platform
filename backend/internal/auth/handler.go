package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"nimbus/internal/platform/httpserver"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

type registerRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	if !strings.Contains(req.Email, "@") || len(req.Password) < 8 {
		httpserver.WriteError(w, r, httpserver.ErrInvalid, "email must be valid and password must be at least 8 characters")
		return
	}

	u, err := h.svc.Register(r.Context(), req.Email, req.Password)
	if err != nil {
		if errors.Is(err, ErrEmailTaken) {
			httpserver.WriteError(w, r, httpserver.ErrConflict, "email already registered")
			return
		}
		httpserver.WriteError(w, r, httpserver.ErrInternal, "registration failed")
		return
	}
	httpserver.WriteJSON(w, http.StatusCreated, map[string]string{"user_id": u.ID, "email": u.Email})
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Email = strings.TrimSpace(strings.ToLower(req.Email))

	_, pair, err := h.svc.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		httpserver.WriteError(w, r, httpserver.ErrUnauthorized, "invalid email or password")
		return
	}
	writeTokenPair(w, pair)
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.RefreshToken == "" {
		httpserver.WriteError(w, r, httpserver.ErrInvalid, "refresh_token is required")
		return
	}

	pair, err := h.svc.Refresh(r.Context(), req.RefreshToken)
	if err != nil {
		httpserver.WriteError(w, r, httpserver.ErrUnauthorized, "invalid or expired refresh token")
		return
	}
	writeTokenPair(w, pair)
}

type logoutRequest struct {
	RefreshToken string `json:"refresh_token"`
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	var req logoutRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	accessToken, _ := bearerToken(r)
	if err := h.svc.Logout(r.Context(), req.RefreshToken, accessToken); err != nil {
		httpserver.WriteError(w, r, httpserver.ErrInternal, "logout failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeTokenPair(w http.ResponseWriter, pair TokenPair) {
	httpserver.WriteJSON(w, http.StatusOK, map[string]any{
		"access_token":  pair.AccessToken,
		"refresh_token": pair.RefreshToken,
		"expires_in":    pair.ExpiresIn,
	})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		httpserver.WriteError(w, r, httpserver.ErrInvalid, "malformed JSON body")
		return false
	}
	return true
}
