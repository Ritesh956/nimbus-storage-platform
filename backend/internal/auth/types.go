// Package auth owns users, sessions, and JWT/refresh-token issuance —
// see docs/03-hld.md §1 and docs/04-lld.md §3.
package auth

import (
	"errors"
	"time"
)

type User struct {
	ID              string
	Email           string
	PasswordHash    string
	IsPlatformAdmin bool
	CreatedAt       time.Time
}

type TokenPair struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int64 // seconds, matches docs/06-api-design.md §2
}

var (
	ErrEmailTaken         = errors.New("email already registered")
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrInvalidRefresh     = errors.New("invalid or expired refresh token")
	ErrUserNotFound       = errors.New("user not found")
	ErrInvalidResetToken  = errors.New("invalid or expired reset token")
	ErrTOTPAlreadyEnabled = errors.New("two-factor authentication is already enabled")
	ErrTOTPNotEnrolled    = errors.New("no two-factor enrollment found")
	ErrInvalidTOTPCode    = errors.New("invalid authentication code")
	ErrInvalidChallenge   = errors.New("invalid or expired login challenge")
)

// LoginResult is what a successful password check yields: either a full
// token pair, or — when the user has confirmed TOTP — a short-lived
// challenge the client must complete via CompleteTOTPLogin (backlog #15).
type LoginResult struct {
	TOTPRequired   bool
	ChallengeToken string
	Tokens         TokenPair
}

// Audit event verbs for auth_audit_log (FR-4: basic audit log of auth
// events — login, token refresh, logout, and the Tier 4 additions).
const (
	AuditEventLogin         = "login"
	AuditEventRefresh       = "refresh"
	AuditEventLogout        = "logout"
	AuditEventPasswordReset = "password_reset"
	AuditEventTOTPEnabled   = "totp_enabled"
	AuditEventTOTPDisabled  = "totp_disabled"
)
