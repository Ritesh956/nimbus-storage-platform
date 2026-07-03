package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"nimbus/internal/platform/idgen"
)

// HS256 (shared secret), not RS256/JWKS — matches "modular monolith today"
// (docs/04-lld.md §3/§6): nothing outside this process verifies tokens yet,
// so there's no case for asymmetric keys until auth is ever split out.

var ErrInvalidToken = errors.New("invalid access token")

type tokenIssuer struct {
	secret []byte
	ttl    time.Duration
}

func newTokenIssuer(secret string, ttl time.Duration) *tokenIssuer {
	return &tokenIssuer{secret: []byte(secret), ttl: ttl}
}

// issue mints a signed access token and returns its jti (needed by the
// caller to persist a logout blacklist entry, docs/04-lld.md §3).
func (t *tokenIssuer) issue(userID string) (token, jti string, expiresAt time.Time, err error) {
	jti = idgen.NewUUID()
	expiresAt = time.Now().Add(t.ttl)
	claims := jwt.RegisteredClaims{
		Subject:   userID,
		ExpiresAt: jwt.NewNumericDate(expiresAt),
		IssuedAt:  jwt.NewNumericDate(time.Now()),
		ID:        jti,
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(t.secret)
	return signed, jti, expiresAt, err
}

func (t *tokenIssuer) verify(tokenStr string) (userID, jti string, expiresAt time.Time, err error) {
	claims := &jwt.RegisteredClaims{}
	parsed, err := jwt.ParseWithClaims(tokenStr, claims, func(tok *jwt.Token) (any, error) {
		if _, ok := tok.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidToken
		}
		return t.secret, nil
	})
	if err != nil || !parsed.Valid || claims.ExpiresAt == nil {
		return "", "", time.Time{}, ErrInvalidToken
	}
	return claims.Subject, claims.ID, claims.ExpiresAt.Time, nil
}
