package postgres

import (
	"context"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/service"
	"github.com/jackc/pgx/v5/pgtype"
)

// RoleCheckerRepo adapts sqlc Queries to the service.RoleCheckerRepository interface.
type RoleCheckerRepo struct {
	q *Queries
}

func NewRoleCheckerRepo(q *Queries) *RoleCheckerRepo {
	return &RoleCheckerRepo{q: q}
}

func (a *RoleCheckerRepo) ListActiveNonBannedUsers(ctx context.Context) ([]service.RoleCheckerUser, error) {
	rows, err := a.q.ListActiveNonBannedUsers(ctx)
	if err != nil {
		return nil, err
	}

	users := make([]service.RoleCheckerUser, len(rows))
	for i, r := range rows {
		users[i] = service.RoleCheckerUser{
			ID:          r.ID,
			DisplayName: r.DisplayName,
			TrustScore:  r.TrustScore,
			Role:        domain.Role(r.Role),
			JoinedAt:    r.JoinedAt.Time,
		}
		if r.TrustBelowSince.Valid {
			t := r.TrustBelowSince.Time
			users[i].TrustBelowSince = &t
		}
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
