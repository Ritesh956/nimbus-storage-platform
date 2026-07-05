package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

type Service struct {
	repo       *Repository
	redis      *redis.Client
	issuer     *tokenIssuer
	refreshTTL time.Duration
}

func NewService(repo *Repository, redisClient *redis.Client, jwtSecret string, accessTTL, refreshTTL time.Duration) *Service {
	return &Service{
		repo:       repo,
		redis:      redisClient,
		issuer:     newTokenIssuer(jwtSecret, accessTTL),
		refreshTTL: refreshTTL,
	}
}

func (s *Service) Register(ctx context.Context, email, password string) (User, error) {
	hash, err := hashPassword(password)
	if err != nil {
		return User{}, err
	}
	return s.repo.CreateUser(ctx, email, hash)
}

func (s *Service) Login(ctx context.Context, email, password string) (User, TokenPair, error) {
	u, err := s.repo.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return User{}, TokenPair{}, ErrInvalidCredentials
		}
		return User{}, TokenPair{}, err
	}
	// Same error for "no such user" and "wrong password" — don't leak
	// which one it was (avoids user enumeration).
	if !verifyPassword(u.PasswordHash, password) {
		return User{}, TokenPair{}, ErrInvalidCredentials
	}
	pair, err := s.issueNewSession(ctx, u.ID)
	if err != nil {
		return User{}, TokenPair{}, err
	}
	s.recordAuditEvent(ctx, u.ID, AuditEventLogin)
	return u, pair, nil
}

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
