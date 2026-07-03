// Package auth owns users, sessions, and JWT/refresh-token issuance —
// see docs/03-hld.md §1 and docs/04-lld.md §3.
package auth

import (
	"errors"
	"time"
)

type User struct {
	ID           string
	Email        string
	PasswordHash string
	CreatedAt    time.Time
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
)
