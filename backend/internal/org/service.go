package org

import "context"

// UserLookup is the port org uses to resolve an email to a user ID without
// importing the auth module's Postgres access directly (HLD §1: nothing
// outside a module touches its tables directly). cmd/api wires the real
// implementation from auth.Repository at startup.
type UserLookup interface {
	GetUserByEmail(ctx context.Context, email string) (userID string, err error)
}

type Service struct {
	repo  *Repository
	users UserLookup
}

func NewService(repo *Repository, users UserLookup) *Service {
	return &Service{repo: repo, users: users}
}

func (s *Service) Create(ctx context.Context, name, ownerUserID string) (Organization, error) {
	return s.repo.CreateWithOwner(ctx, name, ownerUserID)
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
