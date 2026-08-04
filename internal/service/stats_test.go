package service

import (
	"context"
	"errors"
	"testing"
)

// mockStatsRepo is an in-memory StatsRepository for testing. Each count is
// distinct so a test can tell which query a value came from.
type mockStatsRepo struct {
	allUsers   int64
	postsToday int64
	moderators int64
	pending    int64

	allUsersErr   error
	postsTodayErr error
	moderatorsErr error
	pendingErr    error
}

func (m *mockStatsRepo) CountAllUsers(_ context.Context) (int64, error) {
	return m.allUsers, m.allUsersErr
}

func (m *mockStatsRepo) CountPostsToday(_ context.Context) (int64, error) {
	return m.postsToday, m.postsTodayErr
}

func (m *mockStatsRepo) CountModerators(_ context.Context) (int64, error) {
	return m.moderators, m.moderatorsErr
}

func (m *mockStatsRepo) CountPendingUsers(_ context.Context) (int64, error) {
	return m.pending, m.pendingErr
}

// Every count must land in its own field. The four queries all return a bare
// int64, so a swapped assignment would be invisible without distinct values.
func TestStatsService_GetStats(t *testing.T) {
	repo := &mockStatsRepo{allUsers: 142, postsToday: 17, moderators: 5, pending: 23}

	stats, err := NewStatsService(repo).GetStats(context.Background())
	if err != nil {
		t.Fatalf("GetStats() unexpected error: %v", err)
	}
	if stats.TotalUsers != 142 {
		t.Errorf("TotalUsers = %d, want 142", stats.TotalUsers)
	}
	if stats.PostsToday != 17 {
		t.Errorf("PostsToday = %d, want 17", stats.PostsToday)
	}
	if stats.ActiveModerators != 5 {
		t.Errorf("ActiveModerators = %d, want 5", stats.ActiveModerators)
	}
	if stats.PendingUsers != 23 {
		t.Errorf("PendingUsers = %d, want 23", stats.PendingUsers)
	}
}

// A brand-new town has nothing to count; zeroes are a valid answer, not an
// error.
func TestStatsService_GetStats_EmptyTown(t *testing.T) {
	stats, err := NewStatsService(&mockStatsRepo{}).GetStats(context.Background())
	if err != nil {
		t.Fatalf("GetStats() unexpected error: %v", err)
	}
	if *stats != (TownStats{}) {
		t.Errorf("GetStats() = %+v, want all zeroes", *stats)
	}
}

// The dashboard must never render a partial picture: if any one count fails,
// GetStats reports the failure instead of returning the counts it did get.
func TestStatsService_GetStats_AnyQueryFailureAbortsTheWholeReport(t *testing.T) {
	dbDown := errors.New("db connection lost")

	tests := []struct {
		name  string
		setup func(*mockStatsRepo)
	}{
		{"total users query fails", func(m *mockStatsRepo) { m.allUsersErr = dbDown }},
		{"posts today query fails", func(m *mockStatsRepo) { m.postsTodayErr = dbDown }},
		{"moderator count query fails", func(m *mockStatsRepo) { m.moderatorsErr = dbDown }},
		{"pending user query fails", func(m *mockStatsRepo) { m.pendingErr = dbDown }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &mockStatsRepo{allUsers: 142, postsToday: 17, moderators: 5, pending: 23}
			tt.setup(repo)

			stats, err := NewStatsService(repo).GetStats(context.Background())
			if !errors.Is(err, dbDown) {
				t.Fatalf("GetStats() error = %v, want %v", err, dbDown)
			}
			if stats != nil {
				t.Errorf("GetStats() = %+v, want nil alongside the error", stats)
			}
		})
	}
}
