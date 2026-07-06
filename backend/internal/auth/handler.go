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

	result, err := h.svc.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		httpserver.WriteError(w, r, httpserver.ErrUnauthorized, "invalid email or password")
		return
	}
	if result.TOTPRequired {
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{
			"totp_required":   true,
			"challenge_token": result.ChallengeToken,
		})
		return
	}
	writeTokenPair(w, result.Tokens)
}

type totpLoginRequest struct {
	ChallengeToken string `json:"challenge_token"`
	Code           string `json:"code"`
}

// TOTPLogin is step two of a TOTP-gated login (backlog #15).
func (h *Handler) TOTPLogin(w http.ResponseWriter, r *http.Request) {
	var req totpLoginRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.ChallengeToken == "" || req.Code == "" {
		httpserver.WriteError(w, r, httpserver.ErrInvalid, "challenge_token and code are required")
		return
	}
	pair, err := h.svc.CompleteTOTPLogin(r.Context(), req.ChallengeToken, req.Code)
	if err != nil {
		if errors.Is(err, ErrInvalidTOTPCode) || errors.Is(err, ErrInvalidChallenge) {
			httpserver.WriteError(w, r, httpserver.ErrUnauthorized, err.Error())
			return
		}
		httpserver.WriteError(w, r, httpserver.ErrInternal, "login failed")
		return
	}
	writeTokenPair(w, pair)
}

type forgotPasswordRequest struct {
	Email string `json:"email"`
}

// ForgotPassword always answers 202 whether or not the email is registered
// — the response must not be a user-enumeration oracle (backlog #14).
func (h *Handler) ForgotPassword(w http.ResponseWriter, r *http.Request) {
	var req forgotPasswordRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	if !strings.Contains(req.Email, "@") {
		httpserver.WriteError(w, r, httpserver.ErrInvalid, "email must be valid")
		return
	}
	if err := h.svc.ForgotPassword(r.Context(), req.Email); err != nil {
		httpserver.WriteError(w, r, httpserver.ErrInternal, "could not send reset email")
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

type resetPasswordRequest struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

func (h *Handler) ResetPassword(w http.ResponseWriter, r *http.Request) {
	var req resetPasswordRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Token == "" || len(req.Password) < 8 {
		httpserver.WriteError(w, r, httpserver.ErrInvalid, "token is required and password must be at least 8 characters")
		return
	}
	if err := h.svc.ResetPassword(r.Context(), req.Token, req.Password); err != nil {
		if errors.Is(err, ErrInvalidResetToken) {
			httpserver.WriteError(w, r, httpserver.ErrInvalid, err.Error())
			return
		}
		httpserver.WriteError(w, r, httpserver.ErrInternal, "password reset failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Me returns the caller's own identity — added so the frontend can decide
// what to render (e.g. the Admin nav item) without probing gated routes.
func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	u, err := h.svc.repo.GetUserByID(r.Context(), UserIDFromContext(r.Context()))
	if err != nil {
		httpserver.WriteError(w, r, httpserver.ErrInternal, "could not load profile")
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, map[string]any{
		"user_id":           u.ID,
		"email":             u.Email,
		"is_platform_admin": u.IsPlatformAdmin,
	})
}

// TOTPStatus tells the settings UI whether 2FA is on.
func (h *Handler) TOTPStatus(w http.ResponseWriter, r *http.Request) {
	enabled, err := h.svc.TOTPEnabled(r.Context(), UserIDFromContext(r.Context()))
	if err != nil {
		httpserver.WriteError(w, r, httpserver.ErrInternal, "could not read 2FA status")
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, map[string]bool{"enabled": enabled})
}

func (h *Handler) TOTPSetup(w http.ResponseWriter, r *http.Request) {
	secret, uri, err := h.svc.SetupTOTP(r.Context(), UserIDFromContext(r.Context()))
	if err != nil {
		if errors.Is(err, ErrTOTPAlreadyEnabled) {
			httpserver.WriteError(w, r, httpserver.ErrConflict, err.Error())
			return
		}
		httpserver.WriteError(w, r, httpserver.ErrInternal, "could not start 2FA enrollment")
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, map[string]string{"secret": secret, "otpauth_uri": uri})
}

type totpCodeRequest struct {
	Code string `json:"code"`
}

func (h *Handler) TOTPConfirm(w http.ResponseWriter, r *http.Request) {
	var req totpCodeRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := h.svc.ConfirmTOTP(r.Context(), UserIDFromContext(r.Context()), req.Code); err != nil {
		switch {
		case errors.Is(err, ErrInvalidTOTPCode):
			httpserver.WriteError(w, r, httpserver.ErrInvalid, err.Error())
		case errors.Is(err, ErrTOTPNotEnrolled):
			httpserver.WriteError(w, r, httpserver.ErrConflict, "no pending enrollment — start setup first")
		case errors.Is(err, ErrTOTPAlreadyEnabled):
			httpserver.WriteError(w, r, httpserver.ErrConflict, err.Error())
		default:
			httpserver.WriteError(w, r, httpserver.ErrInternal, "could not confirm 2FA")
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) TOTPDisable(w http.ResponseWriter, r *http.Request) {
	var req totpCodeRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := h.svc.DisableTOTP(r.Context(), UserIDFromContext(r.Context()), req.Code); err != nil {
		switch {
		case errors.Is(err, ErrInvalidTOTPCode):
			httpserver.WriteError(w, r, httpserver.ErrInvalid, err.Error())
		case errors.Is(err, ErrTOTPNotEnrolled):
			httpserver.WriteError(w, r, httpserver.ErrNotFound, "two-factor authentication is not enabled")
		default:
			httpserver.WriteError(w, r, httpserver.ErrInternal, "could not disable 2FA")
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
