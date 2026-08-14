package service

import (
	"context"
	"fmt"
	"log/slog"

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
	logger *slog.Logger
}

func NewApprovalService(users ApprovalUserRepository, config ConfigRepository) *ApprovalService {
	return &ApprovalService{
		users:  users,
		config: config,
		logger: slog.Default(),
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
	//
	// requireBootstrap re-checks the same condition on every later call down
	// this path, so a failure here is no longer permanent — but it is still
	// reported, because the operator should hear about it now rather than
	// discover it when the town has been stuck for a week.
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

// requireBootstrap admits the caller only while the town is genuinely still in
// bootstrap mode, re-evaluating the exit before answering.
//
// That re-evaluation is a second chance at the flip Approve makes after
// promoting someone. Approve's runs post-commit: if it fails, the caller gets an
// error but the new member is already counted, and re-approving them fails the
// pending guard — so nothing would ever look at the membership count again and
// the town could sit in bootstrap mode forever. Checking here means any later
// call down the approval path repairs it.
//
// It is a second chance rather than a replacement: Approve keeps its own check
// and keeps propagating that check's errors. This one deliberately does not
// propagate. It runs on calls that are not about the exit at all, and a town
// whose membership count cannot be read should still be able to approve the
// next resident — refusing would turn a repair attempt into an outage. Approve's
// own check, one step later, reports the same failure to the same caller.
func (s *ApprovalService) requireBootstrap(ctx context.Context) error {
	val, err := s.config.GetTownConfig(ctx, "bootstrap_mode")
	if err != nil {
		return fmt.Errorf("%w: bootstrap mode not available", ErrForbidden)
	}
	if val != "true" {
		return fmt.Errorf("%w: not in bootstrap mode", ErrForbidden)
	}
	if s.exitBootstrapIfEarned(ctx) {
		return fmt.Errorf("%w: not in bootstrap mode", ErrForbidden)
	}
	return nil
}

// exitBootstrapIfEarned turns bootstrap mode off when the town already has
// enough active members to have left it, reporting whether it did.
//
// A false return means bootstrap mode is still on — either the town has not
// earned the exit, or the repair could not be made. The two are logged apart
// but answered the same, because in both cases the caller is still in bootstrap
// mode and should be treated as such.
func (s *ApprovalService) exitBootstrapIfEarned(ctx context.Context) bool {
	count, err := s.users.CountActiveMembers(ctx)
	if err != nil {
		s.logger.Warn("re-checking the bootstrap exit failed", "error", err)
		return false
	}
	if count < bootstrapExitThreshold {
		return false
	}

	if err := s.config.SetTownConfig(ctx, "bootstrap_mode", "false"); err != nil {
		s.logger.Warn("re-disabling bootstrap mode failed",
			"active_members", count, "threshold", bootstrapExitThreshold, "error", err)
		return false
	}

	s.logger.Info("bootstrap mode disabled on a later approval-path call; "+
		"the exit check after an earlier approval must have failed",
		"active_members", count, "threshold", bootstrapExitThreshold)
	return true
}
