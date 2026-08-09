package org

import (
	"context"
	"log/slog"
)

// UserLookup is the port org uses to resolve an email to a user ID without
// importing the auth module's Postgres access directly (HLD §1: nothing
// outside a module touches its tables directly). cmd/api wires the real
// implementation from auth.Repository at startup.
type UserLookup interface {
	GetUserByEmail(ctx context.Context, email string) (userID string, err error)
}

// FolderCreator is the port org uses to give every new org a default
// root-level folder, without importing folder's data-access layer
// directly. Added Day 10: without a discoverable entry point, a freshly
// created org had no folder a client could navigate into (every listing
// endpoint needs an already-known folder ID except the new
// GET /v1/orgs/{orgId}/folders, which needs *something* to list).
type FolderCreator interface {
	CreateRoot(ctx context.Context, orgID, name string) error
}

const defaultRootFolderName = "Home"

type Service struct {
	repo       *Repository
	users      UserLookup
	folders    FolderCreator
	usage      UsageSources
	quotaBytes int64 // NIMBUS_ORG_QUOTA_BYTES, reported in the usage view
}

func NewService(repo *Repository, users UserLookup, folders FolderCreator, usage UsageSources, quotaBytes int64) *Service {
	return &Service{repo: repo, users: users, folders: folders, usage: usage, quotaBytes: quotaBytes}
}

// Create makes the org and its owner membership (CreateWithOwner's own
// transaction), then best-effort creates a default root folder. The
// folder step is deliberately outside that transaction and non-fatal on
// failure: the org already exists by that point, so erroring out here
// would misleadingly tell the client "create org" failed when it
// actually partially succeeded — a missing default folder is a
// recoverable UX gap (the owner can still create one manually), not
// worth that confusion.
func (s *Service) Create(ctx context.Context, name, ownerUserID string) (Organization, error) {
	o, err := s.repo.CreateWithOwner(ctx, name, ownerUserID)
	if err != nil {
		return Organization{}, err
	}
	if err := s.folders.CreateRoot(ctx, o.ID, defaultRootFolderName); err != nil {
		slog.Default().Warn("failed to create default root folder", "org_id", o.ID, "error", err)
	}
	return o, nil
}

// CreateForOwner satisfies auth.OrgCreator — the default-org-on-signup
// port (auth.Service.Register) — wrapping Create and discarding the
// Organization, since auth only needs the side effect.
func (s *Service) CreateForOwner(ctx context.Context, name, ownerUserID string) error {
	_, err := s.Create(ctx, name, ownerUserID)
	return err
}

func (s *Service) ListForUser(ctx context.Context, userID string) ([]Organization, error) {
	return s.repo.ListForUser(ctx, userID)
}

func (s *Service) ListMembers(ctx context.Context, orgID string) ([]Member, error) {
	return s.repo.ListMembers(ctx, orgID)
}

// AddMemberByEmail enforces the admin-tier grant bound: only owners hand
// out elevated roles (admin/owner); an admin caller can invite plain
// members and nothing else.
func (s *Service) AddMemberByEmail(ctx context.Context, orgID, email string, role, callerRole Role) (Member, error) {
	if callerRole != RoleOwner && role != RoleMember {
		return Member{}, ErrElevatedRoleNeedsOwner
	}
	userID, err := s.users.GetUserByEmail(ctx, email)
	if err != nil {
		return Member{}, ErrTargetUserNotFound
	}
	if err := s.repo.AddMember(ctx, orgID, userID, role); err != nil {
		return Member{}, err
	}
	return Member{OrgID: orgID, UserID: userID, Email: email, Role: role}, nil
}

// RemoveMember refuses to remove the org's owner — an ownerless org is an
// invalid state this module never allows, matching CreateWithOwner always
// creating the pair together. An admin caller is additionally bounded to
// removing plain members: peers (other admins) and owners are off-limits,
// so a delegated admin can't consolidate control.
func (s *Service) RemoveMember(ctx context.Context, orgID, userID string, callerRole Role) error {
	o, err := s.repo.GetOrg(ctx, orgID)
	if err != nil {
		return err
	}
	if o.OwnerUserID == userID {
		return ErrCannotRemoveOwner
	}
	if callerRole != RoleOwner {
		target, err := s.repo.GetMembership(ctx, orgID, userID)
		if err != nil {
			return err
		}
		if target.Role != RoleMember {
			return ErrAdminRemovesMembersOnly
		}
	}
	return s.repo.RemoveMember(ctx, orgID, userID)
}

// EffectiveQuota resolves orgID's real quota in bytes: its own per-tenant
// override if one is set, else the configured default (s.quotaBytes).
// Shared by the usage view (below) and upload.Service's enforcement path
// (via org.Repository satisfying upload.QuotaReader directly) so both
// report/enforce the exact same number.
func (s *Service) EffectiveQuota(ctx context.Context, orgID string) (int64, error) {
	override, err := s.repo.QuotaOverride(ctx, orgID)
	if err != nil {
		return 0, err
	}
	if override != nil {
		return *override, nil
	}
	return s.quotaBytes, nil
}

// SetQuota sets (quotaBytes != nil) or clears (nil) orgID's per-tenant
// quota override — platform-admin only; see Repository.SetQuotaOverride's
// doc comment for why a cross-tenant limit change isn't an org-admin
// action.
func (s *Service) SetQuota(ctx context.Context, orgID string, quotaBytes *int64) error {
	return s.repo.SetQuotaOverride(ctx, orgID, quotaBytes)
}
