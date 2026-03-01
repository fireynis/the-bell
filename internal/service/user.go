package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/google/uuid"
)

// UserRepository abstracts user persistence using domain types.
type UserRepository interface {
	CreateUser(ctx context.Context, user *domain.User) error
	GetUserByID(ctx context.Context, id string) (*domain.User, error)
	GetUserByKratosID(ctx context.Context, kratosID string) (*domain.User, error)
	UpdateUserProfile(ctx context.Context, id, displayName, bio, avatarURL string) (*domain.User, error)
	ListPendingUsers(ctx context.Context) ([]*domain.User, error)
	CountActiveMembers(ctx context.Context) (int64, error)
	UpdateUserRole(ctx context.Context, id string, role domain.Role) error
}

// UserService orchestrates user business logic.
type UserService struct {
	repo UserRepository
	now  func() time.Time
}

func NewUserService(repo UserRepository, clock func() time.Time) *UserService {
	if clock == nil {
		clock = time.Now
	}
	return &UserService{
		repo: repo,
		now:  clock,
	}
}

// FindOrCreate looks up a user by Kratos identity ID. If no local user exists,
// it auto-provisions one as pending with trust 50.0.
func (s *UserService) FindOrCreate(ctx context.Context, kratosID string) (*domain.User, error) {
	user, err := s.repo.GetUserByKratosID(ctx, kratosID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, fmt.Errorf("looking up user by kratos id: %w", err)
	}
	if user != nil {
		return user, nil
	}

	id, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("generating user id: %w", err)
	}

	now := s.now()
	user = &domain.User{
		ID:               id.String(),
		KratosIdentityID: kratosID,
		TrustScore:       50.0,
		Role:             domain.RolePending,
		IsActive:         true,
		JoinedAt:         now,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	if err := s.repo.CreateUser(ctx, user); err != nil {
		return nil, fmt.Errorf("creating user: %w", err)
	}

	return user, nil
}

// FindByKratosID satisfies the middleware.UserFinder interface by delegating
// to FindOrCreate.
func (s *UserService) FindByKratosID(ctx context.Context, kratosID string) (*domain.User, error) {
	return s.FindOrCreate(ctx, kratosID)
}

// GetByID retrieves a user by their primary ID.
func (s *UserService) GetByID(ctx context.Context, id string) (*domain.User, error) {
	return s.repo.GetUserByID(ctx, id)
}

const (
	maxDisplayNameLength = 100
	maxBioLength         = 500
)

// UpdateProfile updates a user's display name, bio, and avatar URL.
func (s *UserService) UpdateProfile(ctx context.Context, id, displayName, bio, avatarURL string) (*domain.User, error) {
	if strings.TrimSpace(displayName) == "" {
		return nil, fmt.Errorf("%w: display name must not be empty", ErrValidation)
	}
	if len(displayName) > maxDisplayNameLength {
		return nil, fmt.Errorf("%w: display name exceeds %d characters", ErrValidation, maxDisplayNameLength)
	}
	if len(bio) > maxBioLength {
		return nil, fmt.Errorf("%w: bio exceeds %d characters", ErrValidation, maxBioLength)
	}

	return s.repo.UpdateUserProfile(ctx, id, displayName, bio, avatarURL)
}
