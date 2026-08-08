package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// Mailer sends transactional email (password reset). A nil Mailer disables
// sending; the reset link is logged instead so environments without an SMTP
// catcher (kind, bare npm-dev loops) still have a usable flow.
type Mailer interface {
	Send(to, subject, body string) error
}

// OrgCreator is the port auth uses to give every new user a default
// organization at signup, without importing org's data-access layer
// directly (mirrors org's own FolderCreator port — docs/03-hld.md §1).
// Wired in cmd/api from org.Service at startup.
type OrgCreator interface {
	CreateForOwner(ctx context.Context, name, ownerUserID string) error
}

type Service struct {
	repo       *Repository
	redis      *redis.Client
	issuer     *tokenIssuer
	refreshTTL time.Duration
	mailer     Mailer
	webBaseURL string // frontend origin reset links point at
	orgs       OrgCreator
}

func NewService(repo *Repository, redisClient *redis.Client, jwtSecret, jwtSecretPrevious string, accessTTL, refreshTTL time.Duration, mailer Mailer, webBaseURL string, orgs OrgCreator) *Service {
	return &Service{
		repo:       repo,
		redis:      redisClient,
		issuer:     newTokenIssuer(jwtSecret, jwtSecretPrevious, accessTTL),
		refreshTTL: refreshTTL,
		mailer:     mailer,
		webBaseURL: strings.TrimRight(webBaseURL, "/"),
		orgs:       orgs,
	}
}

// Register creates the user, then gives them a default org (named orgName,
// or a generated default if blank) so a fresh signup always has somewhere
// to land instead of an empty, org-less account. Org creation is
// best-effort — the user row already exists by that point, so a failure
// here is logged rather than failing the whole registration (same
// trade-off org.Service.Create makes for its default root folder).
func (s *Service) Register(ctx context.Context, email, password, orgName string) (User, error) {
	hash, err := hashPassword(password)
	if err != nil {
		return User{}, err
	}
	u, err := s.repo.CreateUser(ctx, email, hash)
	if err != nil {
		return User{}, err
	}
	if strings.TrimSpace(orgName) == "" {
		orgName = defaultOrgName(email)
	}
	if err := s.orgs.CreateForOwner(ctx, orgName, u.ID); err != nil {
		slog.Default().Warn("failed to create default org for new user", "user_id", u.ID, "error", err)
	}
	return u, nil
}

func defaultOrgName(email string) string {
	local := email
	if i := strings.Index(email, "@"); i > 0 {
		local = email[:i]
	}
	return local + "'s Organization"
}

func (s *Service) Login(ctx context.Context, email, password string) (LoginResult, error) {
	u, err := s.repo.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return LoginResult{}, ErrInvalidCredentials
		}
		return LoginResult{}, err
	}
	// Same error for "no such user" and "wrong password" — don't leak
	// which one it was (avoids user enumeration).
	if !verifyPassword(u.PasswordHash, password) {
		return LoginResult{}, ErrInvalidCredentials
	}

	// Confirmed TOTP turns login into a two-step flow: hand back a
	// short-lived challenge instead of tokens (backlog #15). The password
	// is not re-checked at step two — possession of the challenge proves
	// step one happened.
	if _, confirmed, err := s.repo.GetTOTP(ctx, u.ID); err == nil && confirmed {
		challenge, challengeHash := newRefreshToken() // same shape: 32 random bytes, only the hash leaves this process
		if err := s.redis.Set(ctx, totpChallengeKey(challengeHash), u.ID, totpChallengeTTL).Err(); err != nil {
			return LoginResult{}, err
		}
		return LoginResult{TOTPRequired: true, ChallengeToken: challenge}, nil
	} else if err != nil && !errors.Is(err, ErrTOTPNotEnrolled) {
		return LoginResult{}, err
	}

	pair, err := s.issueNewSession(ctx, u.ID)
	if err != nil {
		return LoginResult{}, err
	}
	s.recordAuditEvent(ctx, u.ID, AuditEventLogin)
	return LoginResult{Tokens: pair}, nil
}

const (
	resetTokenTTL    = time.Hour
	totpChallengeTTL = 5 * time.Minute
)

// ForgotPassword issues a reset token and emails the reset link. A unknown
// email is deliberately indistinguishable from a known one to the caller
// (no user enumeration) — the work just silently doesn't happen.
func (s *Service) ForgotPassword(ctx context.Context, email string) error {
	u, err := s.repo.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return nil
		}
		return err
	}
	raw, tokenHash := newRefreshToken() // same shape as refresh tokens: 32 random bytes, SHA-256 stored
	if err := s.repo.CreatePasswordResetToken(ctx, u.ID, tokenHash, time.Now().Add(resetTokenTTL)); err != nil {
		return err
	}
	resetURL := s.webBaseURL + "/reset-password?token=" + raw
	if s.mailer == nil {
		slog.Default().Info("password reset requested but no SMTP is configured — link logged instead",
			"email", u.Email, "reset_url", resetURL)
		return nil
	}
	body := fmt.Sprintf(
		"Someone (hopefully you) asked to reset the Nimbus password for %s.\r\n\r\n"+
			"Reset it here within the next hour:\r\n\r\n%s\r\n\r\n"+
			"If this wasn't you, ignore this email — the link expires and your password stays unchanged.\r\n",
		u.Email, resetURL)
	return s.mailer.Send(u.Email, "Reset your Nimbus password", body)
}

// ResetPassword consumes a reset token, sets the new password, and revokes
// every existing session (see Repository.ResetPassword).
func (s *Service) ResetPassword(ctx context.Context, token, newPassword string) error {
	hash, err := hashPassword(newPassword)
	if err != nil {
		return err
	}
	userID, err := s.repo.ResetPassword(ctx, hashToken(token), hash)
	if err != nil {
		return err
	}
	s.recordAuditEvent(ctx, userID, AuditEventPasswordReset)
	return nil
}

// SetupTOTP starts (or restarts) enrollment: a fresh secret is stored
// unconfirmed and returned for the client to render as a QR code. Login is
// unaffected until ConfirmTOTP proves the authenticator app has it.
func (s *Service) SetupTOTP(ctx context.Context, userID string) (secret, uri string, err error) {
	u, err := s.repo.GetUserByID(ctx, userID)
	if err != nil {
		return "", "", err
	}
	secret = newTOTPSecret()
	if err := s.repo.CreateTOTPEnrollment(ctx, userID, secret); err != nil {
		return "", "", err
	}
	return secret, otpauthURI(u.Email, secret), nil
}

// ConfirmTOTP verifies a code against the pending enrollment and flips 2FA
// on.
func (s *Service) ConfirmTOTP(ctx context.Context, userID, code string) error {
	secret, confirmed, err := s.repo.GetTOTP(ctx, userID)
	if err != nil {
		return err
	}
	if confirmed {
		return ErrTOTPAlreadyEnabled
	}
	if err := s.verifyTOTPOnce(ctx, userID, secret, code); err != nil {
		return err
	}
	if err := s.repo.ConfirmTOTP(ctx, userID); err != nil {
		return err
	}
	s.recordAuditEvent(ctx, userID, AuditEventTOTPEnabled)
	return nil
}

// DisableTOTP removes the enrollment; a valid current code is required so a
// hijacked session alone can't switch 2FA off.
func (s *Service) DisableTOTP(ctx context.Context, userID, code string) error {
	secret, confirmed, err := s.repo.GetTOTP(ctx, userID)
	if err != nil {
		return err
	}
	if confirmed {
		if err := s.verifyTOTPOnce(ctx, userID, secret, code); err != nil {
			return err
		}
	}
	if err := s.repo.DeleteTOTP(ctx, userID); err != nil {
		return err
	}
	if confirmed {
		s.recordAuditEvent(ctx, userID, AuditEventTOTPDisabled)
	}
	return nil
}

// TOTPEnabled reports whether the user has a confirmed enrollment.
func (s *Service) TOTPEnabled(ctx context.Context, userID string) (bool, error) {
	_, confirmed, err := s.repo.GetTOTP(ctx, userID)
	if errors.Is(err, ErrTOTPNotEnrolled) {
		return false, nil
	}
	return confirmed, err
}

// CompleteTOTPLogin finishes the two-step login: challenge + valid code →
// token pair. A wrong code leaves the challenge alive (the user can retype
// within the 5-minute TTL); success consumes it.
func (s *Service) CompleteTOTPLogin(ctx context.Context, challengeToken, code string) (TokenPair, error) {
	key := totpChallengeKey(hashToken(challengeToken))
	userID, err := s.redis.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return TokenPair{}, ErrInvalidChallenge
	}
	if err != nil {
		return TokenPair{}, err
	}
	secret, confirmed, err := s.repo.GetTOTP(ctx, userID)
	if err != nil || !confirmed {
		// Enrollment vanished mid-challenge (disabled concurrently) — the
		// challenge can't be satisfied anymore.
		return TokenPair{}, ErrInvalidChallenge
	}
	if err := s.verifyTOTPOnce(ctx, userID, secret, code); err != nil {
		return TokenPair{}, err
	}
	s.redis.Del(ctx, key)
	pair, err := s.issueNewSession(ctx, userID)
	if err != nil {
		return TokenPair{}, err
	}
	s.recordAuditEvent(ctx, userID, AuditEventLogin)
	return pair, nil
}

// verifyTOTPOnce checks the code and burns its time step in Redis so the
// same code can't be replayed within its validity window (RFC 6238 §5.2) —
// one successful use per step, across login/confirm/disable alike.
func (s *Service) verifyTOTPOnce(ctx context.Context, userID, secret, code string) error {
	step, ok := verifyTOTP(secret, code, time.Now())
	if !ok {
		return ErrInvalidTOTPCode
	}
	fresh, err := s.redis.SetNX(ctx, fmt.Sprintf("nimbus:totp:used:%s:%d", userID, step),
		"1", totpPeriod*(totpSkew+2)).Result()
	if err != nil {
		return err
	}
	if !fresh {
		return ErrInvalidTOTPCode
	}
	return nil
}

func totpChallengeKey(challengeHash string) string { return "nimbus:totp:challenge:" + challengeHash }

// recordAuditEvent is a best-effort side effect (FR-4): a logging hiccup
// must never fail an otherwise-successful auth operation, matching the
// upload module's ActivityRecorder convention (upload.Service.CompleteUpload).
func (s *Service) recordAuditEvent(ctx context.Context, userID, event string) {
	if err := s.repo.RecordAuditEvent(ctx, userID, event); err != nil {
		slog.Default().Warn("failed to record auth audit event", "error", err, "event", event)
	}
}

func (s *Service) issueNewSession(ctx context.Context, userID string) (TokenPair, error) {
	access, _, _, err := s.issuer.issue(userID)
	if err != nil {
		return TokenPair{}, err
	}
	refreshRaw, refreshHash := newRefreshToken()
	expiresAt := time.Now().Add(s.refreshTTL)
	if err := s.repo.CreateRefreshFamily(ctx, userID, refreshHash, expiresAt); err != nil {
		return TokenPair{}, err
	}
	return TokenPair{
		AccessToken:  access,
		RefreshToken: refreshRaw,
		ExpiresIn:    int64(s.issuer.ttl.Seconds()),
	}, nil
}

// Refresh rotates the refresh token and issues a new access token. Reuse
// of an already-rotated token revokes the whole family — see
// Repository.RotateRefreshToken and docs/04-lld.md §3.
func (s *Service) Refresh(ctx context.Context, refreshToken string) (TokenPair, error) {
	newRaw, newHash := newRefreshToken()
	userID, err := s.repo.RotateRefreshToken(ctx, hashToken(refreshToken), newHash, time.Now().Add(s.refreshTTL))
	if err != nil {
		return TokenPair{}, err
	}
	access, _, _, err := s.issuer.issue(userID)
	if err != nil {
		return TokenPair{}, err
	}
	s.recordAuditEvent(ctx, userID, AuditEventRefresh)
	return TokenPair{
		AccessToken:  access,
		RefreshToken: newRaw,
		ExpiresIn:    int64(s.issuer.ttl.Seconds()),
	}, nil
}

// Logout revokes the refresh family and, if a still-valid access token was
// presented, blacklists its jti until natural expiry (docs/04-lld.md §3).
func (s *Service) Logout(ctx context.Context, refreshToken, accessToken string) error {
	var userID string
	if refreshToken != "" {
		uid, err := s.repo.RevokeFamilyByTokenHash(ctx, hashToken(refreshToken))
		if err != nil {
			return err
		}
		userID = uid
	}
	if accessToken != "" {
		if uid, jti, expiresAt, err := s.issuer.verify(accessToken); err == nil {
			if userID == "" {
				userID = uid
			}
			if ttl := time.Until(expiresAt); ttl > 0 {
				s.redis.Set(ctx, blacklistKey(jti), "1", ttl)
			}
		}
	}
	if userID != "" {
		s.recordAuditEvent(ctx, userID, AuditEventLogout)
	}
	return nil
}

// VerifyAccessToken is used by the auth middleware on every authenticated
// request.
func (s *Service) VerifyAccessToken(ctx context.Context, token string) (userID string, err error) {
	userID, jti, _, err := s.issuer.verify(token)
	if err != nil {
		return "", err
	}
	blacklisted, err := s.redis.Exists(ctx, blacklistKey(jti)).Result()
	if err != nil {
		return "", err
	}
	if blacklisted > 0 {
		return "", ErrInvalidToken
	}
	return userID, nil
}

// PeekUserID extracts the user ID from a signature-valid access token
// without the blacklist round-trip VerifyAccessToken does — for callers
// that only need a stable caller identity (the rate limiter's bucket key),
// not an authorization decision. Empty string on any failure; a request
// carrying a bad token just gets keyed some other way (per-IP), and real
// authorization still happens in Middleware.
func (s *Service) PeekUserID(token string) string {
	userID, _, _, err := s.issuer.verify(token)
	if err != nil {
		return ""
	}
	return userID
}

func blacklistKey(jti string) string { return "nimbus:blacklist:" + jti }

func newRefreshToken() (raw, hash string) {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	raw = base64.RawURLEncoding.EncodeToString(b)
	return raw, hashToken(raw)
}

func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
