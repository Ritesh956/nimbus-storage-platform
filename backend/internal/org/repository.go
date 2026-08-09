package org

import (
	"context"
	"errors"

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

// CreateWithOwner creates the org and its owner membership in one
// transaction — an org with no owner (or an owner with no membership row)
// should never be observable.
func (r *Repository) CreateWithOwner(ctx context.Context, name, ownerUserID string) (Organization, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Organization{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var o Organization
	id := idgen.NewUUID()
	err = tx.QueryRow(ctx,
		`INSERT INTO organizations (id, name, owner_user_id) VALUES ($1, $2, $3)
		 RETURNING id, name, owner_user_id, created_at`,
		id, name, ownerUserID,
	).Scan(&o.ID, &o.Name, &o.OwnerUserID, &o.CreatedAt)
	if err != nil {
		return Organization{}, err
	}

	if _, err = tx.Exec(ctx,
		`INSERT INTO memberships (org_id, user_id, role) VALUES ($1, $2, $3)`,
		o.ID, ownerUserID, RoleOwner,
	); err != nil {
		return Organization{}, err
	}

	if err = tx.Commit(ctx); err != nil {
		return Organization{}, err
	}
	return o, nil
}

func (r *Repository) GetOrg(ctx context.Context, orgID string) (Organization, error) {
	var o Organization
	err := r.pool.QueryRow(ctx,
		`SELECT id, name, owner_user_id, created_at FROM organizations WHERE id = $1`, orgID,
	).Scan(&o.ID, &o.Name, &o.OwnerUserID, &o.CreatedAt)
	return o, err
}

// ListForUser returns every org userID is a member of — the read a client
// needs right after login to know what to show; there was no way to
// discover this before Day 10, a real gap closed here (only
// POST /v1/orgs existed, no listing).
func (r *Repository) ListForUser(ctx context.Context, userID string) ([]Organization, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT o.id, o.name, o.owner_user_id, o.created_at
		 FROM organizations o JOIN memberships m ON m.org_id = o.id
		 WHERE m.user_id = $1 ORDER BY o.created_at`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var orgs []Organization
	for rows.Next() {
		var o Organization
		if err := rows.Scan(&o.ID, &o.Name, &o.OwnerUserID, &o.CreatedAt); err != nil {
			return nil, err
		}
		orgs = append(orgs, o)
	}
	return orgs, rows.Err()
}

func (r *Repository) GetMembership(ctx context.Context, orgID, userID string) (Member, error) {
	var m Member
	err := r.pool.QueryRow(ctx,
		`SELECT org_id, user_id, role, created_at FROM memberships WHERE org_id = $1 AND user_id = $2`,
		orgID, userID,
	).Scan(&m.OrgID, &m.UserID, &m.Role, &m.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Member{}, ErrNotMember
	}
	return m, err
}

func (r *Repository) ListMembers(ctx context.Context, orgID string) ([]Member, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT m.org_id, m.user_id, u.email, m.role, m.created_at
		 FROM memberships m JOIN users u ON u.id = m.user_id
		 WHERE m.org_id = $1 ORDER BY m.created_at`,
		orgID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var members []Member
	for rows.Next() {
		var m Member
		if err := rows.Scan(&m.OrgID, &m.UserID, &m.Email, &m.Role, &m.CreatedAt); err != nil {
			return nil, err
		}
		members = append(members, m)
	}
	return members, rows.Err()
}

func (r *Repository) AddMember(ctx context.Context, orgID, userID string, role Role) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO memberships (org_id, user_id, role) VALUES ($1, $2, $3)`,
		orgID, userID, role,
	)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return ErrAlreadyMember
	}
	return err
}

func (r *Repository) RemoveMember(ctx context.Context, orgID, userID string) error {
	_, err := r.pool.Exec(ctx,
		`DELETE FROM memberships WHERE org_id = $1 AND user_id = $2`, orgID, userID,
	)
	return err
}

// QuotaOverride returns this org's per-tenant quota override in bytes, or
// nil if it has none (the common case — falls back to the configured
// default, resolved by the caller). Audit §06: quota used to be a single
// config value applied identically to every org, with no per-tenant
// differentiation possible in the data model at all.
func (r *Repository) QuotaOverride(ctx context.Context, orgID string) (*int64, error) {
	var quotaBytes *int64
	err := r.pool.QueryRow(ctx,
		`SELECT quota_bytes_override FROM organizations WHERE id = $1`, orgID).Scan(&quotaBytes)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return quotaBytes, err
}

// SetQuotaOverride sets or clears (quotaBytes == nil) an org's per-tenant
// quota override — platform-admin only (see auth.RequirePlatformAdmin on
// the route), a cross-tenant limit change is not something an org's own
// admin should be able to grant themselves.
func (r *Repository) SetQuotaOverride(ctx context.Context, orgID string, quotaBytes *int64) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE organizations SET quota_bytes_override = $1 WHERE id = $2`, quotaBytes, orgID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
