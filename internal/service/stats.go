package service

import "context"

// TownStats holds aggregate statistics for the admin dashboard.
type TownStats struct {
	TotalUsers       int64 `json:"total_users"`
	PostsToday       int64 `json:"posts_today"`
	ActiveModerators int64 `json:"active_moderators"`
	PendingUsers     int64 `json:"pending_users"`
}

// StatsRepository abstracts the queries needed for town statistics.
type StatsRepository interface {
	CountAllUsers(ctx context.Context) (int64, error)
	CountPostsToday(ctx context.Context) (int64, error)
	CountModerators(ctx context.Context) (int64, error)
	CountPendingUsers(ctx context.Context) (int64, error)
}

// StatsService aggregates town statistics.
type StatsService struct {
	repo StatsRepository
}

// NewStatsService creates a StatsService.
func NewStatsService(repo StatsRepository) *StatsService {
	return &StatsService{repo: repo}
}

// GetStats returns aggregated town statistics.
func (s *StatsService) GetStats(ctx context.Context) (*TownStats, error) {
	totalUsers, err := s.repo.CountAllUsers(ctx)
	if err != nil {
		return nil, err
	}

	postsToday, err := s.repo.CountPostsToday(ctx)
	if err != nil {
		return nil, err
	}

	moderators, err := s.repo.CountModerators(ctx)
	if err != nil {
		return nil, err
	}

	pending, err := s.repo.CountPendingUsers(ctx)
	if err != nil {
		return nil, err
	}

	return &TownStats{
		TotalUsers:       totalUsers,
		PostsToday:       postsToday,
		ActiveModerators: moderators,
		PendingUsers:     pending,
	}, nil
}
