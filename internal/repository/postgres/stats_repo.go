package postgres

import "context"

// StatsRepo adapts sqlc queries to the service.StatsRepository interface.
type StatsRepo struct {
	q *Queries
}

// NewStatsRepo creates a StatsRepo.
func NewStatsRepo(q *Queries) *StatsRepo {
	return &StatsRepo{q: q}
}

func (r *StatsRepo) CountAllUsers(ctx context.Context) (int64, error) {
	return r.q.CountAllUsers(ctx)
}

func (r *StatsRepo) CountPostsToday(ctx context.Context) (int64, error) {
	return r.q.CountPostsToday(ctx)
}

func (r *StatsRepo) CountModerators(ctx context.Context) (int64, error) {
	return r.q.CountModerators(ctx)
}

func (r *StatsRepo) CountPendingUsers(ctx context.Context) (int64, error) {
	return r.q.CountPendingUsers(ctx)
}
