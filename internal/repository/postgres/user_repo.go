package postgres

import (
	"context"
	"errors"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/service"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// UserRepo adapts sqlc queries to the service.UserRepository interface.
type UserRepo struct {
	q *Queries
}

func NewUserRepo(q *Queries) *UserRepo {
	return &UserRepo{q: q}
}

func (r *UserRepo) CreateUser(ctx context.Context, user *domain.User) error {
	_, err := r.q.CreateUser(ctx, CreateUserParams{
		ID:               user.ID,
		KratosIdentityID: user.KratosIdentityID,
		DisplayName:      user.DisplayName,
		Bio:              user.Bio,
		AvatarUrl:        user.AvatarURL,
		TrustScore:       user.TrustScore,
		Role:             string(user.Role),
		IsActive:         user.IsActive,
		JoinedAt:         pgtype.Timestamptz{Time: user.JoinedAt, Valid: true},
		CreatedAt:        pgtype.Timestamptz{Time: user.CreatedAt, Valid: true},
		UpdatedAt:        pgtype.Timestamptz{Time: user.UpdatedAt, Valid: true},
	})
	return err
}

func (r *UserRepo) GetUserByID(ctx context.Context, id string) (*domain.User, error) {
	row, err := r.q.GetUserByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, service.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return userFromRow(row), nil
}

func (r *UserRepo) GetUserByKratosID(ctx context.Context, kratosID string) (*domain.User, error) {
	row, err := r.q.GetUserByKratosID(ctx, kratosID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, service.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return userFromRow(row), nil
}

func (r *UserRepo) UpdateUserProfile(ctx context.Context, id, displayName, bio, avatarURL string) (*domain.User, error) {
	row, err := r.q.UpdateUserProfile(ctx, UpdateUserProfileParams{
		ID:          id,
		DisplayName: displayName,
		Bio:         bio,
		AvatarUrl:   avatarURL,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, service.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return userFromRow(row), nil
}

func (r *UserRepo) ListPendingUsers(ctx context.Context) ([]*domain.User, error) {
	rows, err := r.q.ListPendingUsers(ctx)
	if err != nil {
		return nil, err
	}
	users := make([]*domain.User, len(rows))
	for i, row := range rows {
		users[i] = userFromRow(row)
	}
	return users, nil
}

func (r *UserRepo) CountActiveMembers(ctx context.Context) (int64, error) {
	return r.q.CountUsersByMinRole(ctx)
}

func (r *UserRepo) UpdateUserRole(ctx context.Context, id string, role domain.Role) error {
	return r.q.UpdateUserRole(ctx, UpdateUserRoleParams{
		ID:   id,
		Role: string(role),
	})
}

func userFromRow(row User) *domain.User {
	return &domain.User{
		ID:               row.ID,
		KratosIdentityID: row.KratosIdentityID,
		DisplayName:      row.DisplayName,
		Bio:              row.Bio,
		AvatarURL:        row.AvatarUrl,
		TrustScore:       row.TrustScore,
		Role:             domain.Role(row.Role),
		IsActive:         row.IsActive,
		JoinedAt:         row.JoinedAt.Time,
		CreatedAt:        row.CreatedAt.Time,
		UpdatedAt:        row.UpdatedAt.Time,
	}
}
