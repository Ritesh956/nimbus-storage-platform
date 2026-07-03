package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
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
	return u, pair, err
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
	return TokenPair{
		AccessToken:  access,
		RefreshToken: newRaw,
		ExpiresIn:    int64(s.issuer.ttl.Seconds()),
	}, nil
}

// Logout revokes the refresh family and, if a still-valid access token was
// presented, blacklists its jti until natural expiry (docs/04-lld.md §3).
func (s *Service) Logout(ctx context.Context, refreshToken, accessToken string) error {
	if refreshToken != "" {
		if err := s.repo.RevokeFamilyByTokenHash(ctx, hashToken(refreshToken)); err != nil {
			return err
		}
	}
	if accessToken != "" {
		if _, jti, expiresAt, err := s.issuer.verify(accessToken); err == nil {
			if ttl := time.Until(expiresAt); ttl > 0 {
				s.redis.Set(ctx, blacklistKey(jti), "1", ttl)
			}
		}
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
