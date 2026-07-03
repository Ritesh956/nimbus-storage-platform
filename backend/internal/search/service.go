package search

import "context"

const (
	defaultLimit = 20
	maxLimit     = 100
)

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) Search(ctx context.Context, orgID string, f Filters) ([]Result, string, error) {
	if f.Limit <= 0 || f.Limit > maxLimit {
		f.Limit = defaultLimit
	}
	return s.repo.Search(ctx, orgID, f)
}
