package service

import (
	"context"
	"fmt"

	"github.com/fireynis/the-bell/internal/domain"
)

const bootstrapExitThreshold = 20

// ApprovalUserRepository is the subset of user persistence needed by ApprovalService.
type ApprovalUserRepository interface {
	GetUserByID(ctx context.Context, id string) (*domain.User, error)
	ListPendingUsers(ctx context.Context) ([]*domain.User, error)
	CountActiveMembers(ctx context.Context) (int64, error)
	UpdateUserRole(ctx context.Context, id string, role domain.Role) error
}

// ApprovalService handles council approval of pending users during bootstrap.
type ApprovalService struct {
	users  ApprovalUserRepository
	config ConfigRepository
}

func NewApprovalService(users ApprovalUserRepository, config ConfigRepository) *ApprovalService {
	return &ApprovalService{
		users:  users,
		config: config,
	}
}

// ListPending returns all pending users. Only available during bootstrap mode.
func (s *ApprovalService) ListPending(ctx context.Context) ([]*domain.User, error) {
	if err := s.requireBootstrap(ctx); err != nil {
		return nil, err
	}
	return s.users.ListPendingUsers(ctx)
}

// Approve promotes a pending user to member. Only available during bootstrap mode.
// When the active member count reaches the threshold, bootstrap mode is auto-disabled.
func (s *ApprovalService) Approve(ctx context.Context, userID string) (*domain.User, error) {
	if err := s.requireBootstrap(ctx); err != nil {
		return nil, err
	}

	user, err := s.users.GetUserByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("looking up user: %w", err)
	}

	if user.Role != domain.RolePending {
		return nil, fmt.Errorf("%w: user is not pending", ErrValidation)
	}
	if !user.IsActive {
		return nil, fmt.Errorf("%w: user is not active", ErrValidation)
	}

	if err := s.users.UpdateUserRole(ctx, userID, domain.RoleMember); err != nil {
		return nil, fmt.Errorf("updating user role: %w", err)
	}
	user.Role = domain.RoleMember

	// Leaving bootstrap mode is a one-way transition that stops council
	// approval being the way people join, so a failure here must reach the
	// caller rather than leaving the town in bootstrap mode indefinitely. The
	// role change above has already committed; the error says the exit check
	// did not run, not that the approval was rolled back.
	count, err := s.users.CountActiveMembers(ctx)
	if err != nil {
		return nil, fmt.Errorf("counting active members: %w", err)
	}
	if count >= bootstrapExitThreshold {
		if err := s.config.SetTownConfig(ctx, "bootstrap_mode", "false"); err != nil {
			return nil, fmt.Errorf("disabling bootstrap mode: %w", err)
		}
	}

	return user, nil
}

func (s *ApprovalService) requireBootstrap(ctx context.Context) error {
	val, err := s.config.GetTownConfig(ctx, "bootstrap_mode")
	if err != nil {
		return fmt.Errorf("%w: bootstrap mode not available", ErrForbidden)
	}
	if val != "true" {
		return fmt.Errorf("%w: not in bootstrap mode", ErrForbidden)
	}
	return nil
}
