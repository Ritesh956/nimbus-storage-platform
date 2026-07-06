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
		 RETURNING id, email, password_hash, is_platform_admin, created_at`,
		email, passwordHash,
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.IsPlatformAdmin, &u.CreatedAt)
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
		`SELECT id, email, password_hash, is_platform_admin, created_at FROM users WHERE email = $1`, email,
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.IsPlatformAdmin, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrUserNotFound
	}
	return u, err
}

func (r *Repository) GetUserByID(ctx context.Context, id string) (User, error) {
	var u User
	err := r.pool.QueryRow(ctx,
		`SELECT id, email, password_hash, is_platform_admin, created_at FROM users WHERE id = $1`, id,
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.IsPlatformAdmin, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrUserNotFound
	}
	return u, err
}

// EnsureSeededAdmin is the *only* path to is_platform_admin=true: it
// creates the single account named by NIMBUS_ADMIN_EMAIL if it doesn't
// exist yet (with the given plaintext password, hashed here), or leaves
// an existing account's password untouched and just makes sure the flag
// is set. There is deliberately no email-list/self-service mechanism —
// an arbitrary signup can never become an admin by matching a config
// value, only this one seeded account can.
func (r *Repository) EnsureSeededAdmin(ctx context.Context, email, plainPassword string) error {
	hash, err := hashPassword(plainPassword)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx,
		`INSERT INTO users (email, password_hash, is_platform_admin)
		 VALUES ($1, $2, true)
		 ON CONFLICT (email) DO UPDATE SET is_platform_admin = true`,
		email, hash)
	return err
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
// belongs to (logout), and returns the family's user_id for audit-logging
// purposes. A hash that matches nothing is treated as already revoked, not
// an error (userID is returned empty in that case).
func (r *Repository) RevokeFamilyByTokenHash(ctx context.Context, tokenHash string) (userID string, err error) {
	err = r.pool.QueryRow(ctx,
		`DELETE FROM refresh_tokens WHERE family_id = (
			SELECT family_id FROM refresh_tokens WHERE token_hash = $1
		 )
		 RETURNING user_id`,
		tokenHash,
	).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return userID, err
}

// CreatePasswordResetToken stores the hash of a freshly-issued reset token
// (backlog #14). Multiple live tokens per user are allowed — each is
// single-use and expires on its own clock.
func (r *Repository) CreatePasswordResetToken(ctx context.Context, userID, tokenHash string, expiresAt time.Time) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO password_reset_tokens (id, user_id, token_hash, expires_at)
		 VALUES ($1, $2, $3, $4)`,
		idgen.NewUUID(), userID, tokenHash, expiresAt)
	return err
}

// ResetPassword atomically consumes a reset token, rewrites the user's
// password hash, and revokes every refresh-token family the user has — a
// reset means the old credential may be compromised, so no session issued
// under it survives.
func (r *Repository) ResetPassword(ctx context.Context, tokenHash, newPasswordHash string) (userID string, err error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after Commit

	var usedAt *time.Time
	var expiresAt time.Time
	err = tx.QueryRow(ctx,
		`SELECT user_id, used_at, expires_at FROM password_reset_tokens
		 WHERE token_hash = $1 FOR UPDATE`,
		tokenHash,
	).Scan(&userID, &usedAt, &expiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrInvalidResetToken
	}
	if err != nil {
		return "", err
	}
	if usedAt != nil || time.Now().After(expiresAt) {
		return "", ErrInvalidResetToken
	}

	if _, err = tx.Exec(ctx,
		`UPDATE password_reset_tokens SET used_at = now() WHERE token_hash = $1`, tokenHash); err != nil {
		return "", err
	}
	if _, err = tx.Exec(ctx,
		`UPDATE users SET password_hash = $1 WHERE id = $2`, newPasswordHash, userID); err != nil {
		return "", err
	}
	if _, err = tx.Exec(ctx,
		`DELETE FROM refresh_tokens WHERE user_id = $1`, userID); err != nil {
		return "", err
	}
	return userID, tx.Commit(ctx)
}

// CreateTOTPEnrollment starts (or restarts) a pending TOTP enrollment
// (backlog #15). A confirmed enrollment is never overwritten — disabling
// first is the only way back, so a session hijacker can't silently rotate
// the victim's 2FA secret.
func (r *Repository) CreateTOTPEnrollment(ctx context.Context, userID, secret string) error {
	tag, err := r.pool.Exec(ctx,
		`INSERT INTO user_totp (user_id, secret) VALUES ($1, $2)
		 ON CONFLICT (user_id) DO UPDATE SET secret = EXCLUDED.secret, created_at = now()
		 WHERE user_totp.confirmed_at IS NULL`,
		userID, secret)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrTOTPAlreadyEnabled
	}
	return nil
}

// GetTOTP returns the user's TOTP secret and whether enrollment has been
// confirmed (a pending enrollment does not gate login).
func (r *Repository) GetTOTP(ctx context.Context, userID string) (secret string, confirmed bool, err error) {
	var confirmedAt *time.Time
	err = r.pool.QueryRow(ctx,
		`SELECT secret, confirmed_at FROM user_totp WHERE user_id = $1`, userID,
	).Scan(&secret, &confirmedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, ErrTOTPNotEnrolled
	}
	return secret, confirmedAt != nil, err
}

// ConfirmTOTP flips a pending enrollment to confirmed.
func (r *Repository) ConfirmTOTP(ctx context.Context, userID string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE user_totp SET confirmed_at = now() WHERE user_id = $1 AND confirmed_at IS NULL`, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrTOTPNotEnrolled
	}
	return nil
}

// DeleteTOTP removes the user's enrollment (confirmed or pending).
func (r *Repository) DeleteTOTP(ctx context.Context, userID string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM user_totp WHERE user_id = $1`, userID)
	return err
}

// RecordAuditEvent appends a row to auth_audit_log (FR-4).
func (r *Repository) RecordAuditEvent(ctx context.Context, userID, event string) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO auth_audit_log (user_id, event) VALUES ($1, $2)`,
		userID, event)
	return err
}
