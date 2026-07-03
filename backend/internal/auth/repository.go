package auth

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"nimbus/internal/platform/idgen"
)

const uniqueViolation = "23505"

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) CreateUser(ctx context.Context, email, passwordHash string) (User, error) {
	var u User
	err := r.pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash) VALUES ($1, $2)
		 RETURNING id, email, password_hash, created_at`,
		email, passwordHash,
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
			return User{}, ErrEmailTaken
		}
		return User{}, err
	}
	return u, nil
}

func (r *Repository) GetUserByEmail(ctx context.Context, email string) (User, error) {
	var u User
	err := r.pool.QueryRow(ctx,
		`SELECT id, email, password_hash, created_at FROM users WHERE email = $1`, email,
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrUserNotFound
	}
	return u, err
}

func (r *Repository) GetUserByID(ctx context.Context, id string) (User, error) {
	var u User
	err := r.pool.QueryRow(ctx,
		`SELECT id, email, password_hash, created_at FROM users WHERE id = $1`, id,
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrUserNotFound
	}
	return u, err
}

// CreateRefreshFamily starts a brand-new rotation family (fresh login), per
// docs/04-lld.md §3.
func (r *Repository) CreateRefreshFamily(ctx context.Context, userID, tokenHash string, expiresAt time.Time) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO refresh_tokens (id, user_id, family_id, token_hash, expires_at)
		 VALUES ($1, $2, $3, $4, $5)`,
		idgen.NewUUID(), userID, idgen.NewUUID(), tokenHash, expiresAt,
	)
	return err
}

// RotateRefreshToken atomically validates oldTokenHash, marks it used, and
// issues a new token in the same family. If oldTokenHash was already used,
// that's reuse of a stolen/replayed token — the whole family is revoked
// (docs/04-lld.md §3).
func (r *Repository) RotateRefreshToken(ctx context.Context, oldTokenHash, newTokenHash string, newExpiresAt time.Time) (userID string, err error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after Commit

	var id, familyID string
	var usedAt *time.Time
	var expiresAt time.Time
	err = tx.QueryRow(ctx,
		`SELECT id, user_id, family_id, used_at, expires_at FROM refresh_tokens
		 WHERE token_hash = $1 FOR UPDATE`,
		oldTokenHash,
	).Scan(&id, &userID, &familyID, &usedAt, &expiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrInvalidRefresh
	}
	if err != nil {
		return "", err
	}

	if usedAt != nil {
		// Reuse of an already-rotated token: treat the family as compromised.
		if _, delErr := tx.Exec(ctx, `DELETE FROM refresh_tokens WHERE family_id = $1`, familyID); delErr != nil {
			return "", delErr
		}
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return "", commitErr
		}
		return "", ErrInvalidRefresh
	}
	if time.Now().After(expiresAt) {
		return "", ErrInvalidRefresh
	}

	if _, err = tx.Exec(ctx, `UPDATE refresh_tokens SET used_at = now() WHERE id = $1`, id); err != nil {
		return "", err
	}
	if _, err = tx.Exec(ctx,
		`INSERT INTO refresh_tokens (id, user_id, family_id, token_hash, expires_at)
		 VALUES ($1, $2, $3, $4, $5)`,
		idgen.NewUUID(), userID, familyID, newTokenHash, newExpiresAt,
	); err != nil {
		return "", err
	}

	if err = tx.Commit(ctx); err != nil {
		return "", err
	}
	return userID, nil
}

// RevokeFamilyByTokenHash revokes every token in the family that hash
// belongs to (logout). A hash that matches nothing is treated as already
// revoked, not an error.
func (r *Repository) RevokeFamilyByTokenHash(ctx context.Context, tokenHash string) error {
	_, err := r.pool.Exec(ctx,
		`DELETE FROM refresh_tokens WHERE family_id = (
			SELECT family_id FROM refresh_tokens WHERE token_hash = $1
		 )`,
		tokenHash,
	)
	return err
}
