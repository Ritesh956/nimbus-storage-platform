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

func (s *Service) ListForUser(ctx context.Context, userID string) ([]Organization, error) {
	return s.repo.ListForUser(ctx, userID)
}

func (s *Service) ListMembers(ctx context.Context, orgID string) ([]Member, error) {
	return s.repo.ListMembers(ctx, orgID)
}

func (s *Service) AddMemberByEmail(ctx context.Context, orgID, email string, role Role) (Member, error) {
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
// creating the pair together.
func (s *Service) RemoveMember(ctx context.Context, orgID, userID string) error {
	o, err := s.repo.GetOrg(ctx, orgID)
	if err != nil {
		return err
	}
	if o.OwnerUserID == userID {
		return ErrCannotRemoveOwner
	}
	return s.repo.RemoveMember(ctx, orgID, userID)
}
