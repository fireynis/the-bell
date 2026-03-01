package service

import (
	"context"
	"fmt"

	"github.com/fireynis/the-bell/internal/domain"
)

const bootstrapExitThreshold = 20

// ApprovalService handles council approval of pending users during bootstrap.
type ApprovalService struct {
	users  UserRepository
	config ConfigRepository
}

func NewApprovalService(users UserRepository, config ConfigRepository) *ApprovalService {
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

	count, err := s.users.CountActiveMembers(ctx)
	if err != nil {
		return user, nil // approval succeeded; count check is best-effort
	}
	if count >= bootstrapExitThreshold {
		_ = s.config.SetTownConfig(ctx, "bootstrap_mode", "false")
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
