package folder

import "context"

// MembershipChecker is the port folder uses to authorize access without
// importing org's data-access layer directly (docs/03-hld.md §1).
type MembershipChecker interface {
	IsMember(ctx context.Context, orgID, userID string) (bool, error)
}

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) Create(ctx context.Context, orgID string, parentID *string, name string) (Folder, error) {
	if parentID != nil {
		parent, err := s.repo.Get(ctx, *parentID)
		if err != nil {
			return Folder{}, ErrInvalidParent
		}
		if parent.OrgID != orgID {
			return Folder{}, ErrInvalidParent
		}
	}
	return s.repo.Create(ctx, orgID, parentID, name)
}

func (s *Service) Get(ctx context.Context, id string) (Folder, error) {
	return s.repo.Get(ctx, id)
}

func (s *Service) ListTrashed(ctx context.Context, orgID string) ([]Folder, error) {
	return s.repo.ListTrashed(ctx, orgID)
}

func (s *Service) GetAny(ctx context.Context, id string) (Folder, error) {
	return s.repo.GetAny(ctx, id)
}

func (s *Service) ListChildren(ctx context.Context, orgID string, parentID *string) ([]Folder, error) {
	return s.repo.ListChildren(ctx, orgID, parentID)
}

func (s *Service) Ancestors(ctx context.Context, folderID string) ([]PathEntry, error) {
	return s.repo.Ancestors(ctx, folderID)
}

// Update renames and/or moves folder. See Repository.Update for the
// pointer-to-pointer semantics of newParentID.
func (s *Service) Update(ctx context.Context, current Folder, name *string, newParentID **string) (Folder, error) {
	if newParentID != nil && *newParentID != nil {
		target := **newParentID
		if target == current.ID {
			return Folder{}, ErrCyclicMove
		}
		parent, err := s.repo.Get(ctx, target)
		if err != nil {
			return Folder{}, ErrInvalidParent
		}
		if parent.OrgID != current.OrgID {
			return Folder{}, ErrInvalidParent
		}
		isDescendant, err := s.repo.IsSelfOrDescendant(ctx, current.ID, target)
		if err != nil {
			return Folder{}, err
		}
		if isDescendant {
			return Folder{}, ErrCyclicMove
		}
	}
	return s.repo.Update(ctx, current.ID, name, newParentID)
}

func (s *Service) Delete(ctx context.Context, folderID string) error {
	return s.repo.SoftDeleteCascade(ctx, folderID)
}

func (s *Service) Restore(ctx context.Context, folderID string) error {
	return s.repo.RestoreCascade(ctx, folderID)
}
