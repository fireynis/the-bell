package postgres

import (
	"context"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/jackc/pgx/v5/pgtype"
)

// RoleCheckerRepo adapts sqlc Queries to the role checker's repository
// interface.
type RoleCheckerRepo struct {
	q *Queries
}

func NewRoleCheckerRepo(q *Queries) *RoleCheckerRepo {
	return &RoleCheckerRepo{q: q}
}

// ListActiveNonBannedUsers returns the promotion/demotion candidates.
//
// It shares userFromRow with UserRepo rather than copying the handful of fields
// the role policy happens to read. The query is SELECT *, so the whole row is
// already in hand: hand-copying a subset into a parallel struct bought nothing
// and meant a column added to users reached the role checker only if someone
// remembered to extend the copy.
func (a *RoleCheckerRepo) ListActiveNonBannedUsers(ctx context.Context) ([]*domain.User, error) {
	rows, err := a.q.ListActiveNonBannedUsers(ctx)
	if err != nil {
		return nil, err
	}

	users := make([]*domain.User, len(rows))
	for i, r := range rows {
		users[i] = userFromRow(r)
	}
	return users, nil
}

func (a *RoleCheckerRepo) CountActiveModeratorVouchesForUser(ctx context.Context, userID string) (int64, error) {
	return a.q.CountActiveModeratorVouchesForUser(ctx, userID)
}

func (a *RoleCheckerRepo) UpdateUserRole(ctx context.Context, id string, role domain.Role) error {
	return a.q.UpdateUserRole(ctx, UpdateUserRoleParams{
		ID:   id,
		Role: string(role),
	})
}

func (a *RoleCheckerRepo) UpdateUserTrustBelowSince(ctx context.Context, id string, since time.Time) error {
	return a.q.UpdateUserTrustBelowSince(ctx, UpdateUserTrustBelowSinceParams{
		ID: id,
		TrustBelowSince: pgtype.Timestamptz{
			Time:  since,
			Valid: true,
		},
	})
}

func (a *RoleCheckerRepo) ClearUserTrustBelowSince(ctx context.Context, id string) error {
	return a.q.ClearUserTrustBelowSince(ctx, id)
}

func (a *RoleCheckerRepo) CreateRoleHistoryEntry(ctx context.Context, entry *domain.RoleHistory) error {
	_, err := a.q.CreateRoleHistoryEntry(ctx, CreateRoleHistoryEntryParams{
		ID:      entry.ID,
		UserID:  entry.UserID,
		OldRole: string(entry.OldRole),
		NewRole: string(entry.NewRole),
		Reason:  entry.Reason,
		CreatedAt: pgtype.Timestamptz{
			Time:  entry.CreatedAt,
			Valid: true,
		},
	})
	return err
}
