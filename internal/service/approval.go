package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/google/uuid"
)

const bootstrapExitThreshold = 20

// ApprovalUserRepository is the subset of user persistence needed by ApprovalService.
type ApprovalUserRepository interface {
	GetUserByID(ctx context.Context, id string) (*domain.User, error)
	// ListPendingUsersPage and CountPendingUsersMatching page and size the same
	// population, so an implementation must apply the same filter to both. query
	// is a plain substring — escaping whatever the storage engine reads as
	// syntax is the implementation's job. The page is oldest applicant first.
	ListPendingUsersPage(ctx context.Context, query string, limit, offset int) ([]*domain.User, error)
	CountPendingUsersMatching(ctx context.Context, query string) (int64, error)
	CountActiveMembers(ctx context.Context) (int64, error)
	UpdateUserRole(ctx context.Context, id string, role domain.Role) error
}

// ApprovalService handles council approval of pending users during bootstrap.
type ApprovalService struct {
	users       ApprovalUserRepository
	config      ConfigRepository
	roleHistory RoleHistoryWriter
	now         func() time.Time
	logger      *slog.Logger
}

func NewApprovalService(users ApprovalUserRepository, config ConfigRepository) *ApprovalService {
	return &ApprovalService{
		users:  users,
		config: config,
		now:    time.Now,
		logger: slog.Default(),
	}
}

// SetRoleHistory attaches the audit trail, on the same terms as
// VouchService.SetRoleHistory: a setter for wiring convenience, but a council
// approval is a role change and belongs in role_history like every other. Until
// this was wired, the two ways a resident actually becomes a member — a vouch
// and a bootstrap approval — were the only role changes the trail did not
// record. Approving with no writer attached logs a warning.
func (s *ApprovalService) SetRoleHistory(w RoleHistoryWriter) {
	s.roleHistory = w
}

// SetClock overrides the clock used to stamp role history entries. Tests use
// it; nothing else does.
func (s *ApprovalService) SetClock(clock func() time.Time) {
	if clock != nil {
		s.now = clock
	}
}

// ListPending returns one page of pending users, oldest applicant first, and
// the total number of them matching the search. Only available during bootstrap
// mode.
//
// It is paged rather than whole because the queue is exactly the thing that
// grows without warning: a town launch or a registration flood puts fifty
// strangers in front of the council at once, and the page they work through is
// no place to render all of them. The bounds are boundUserListing's, shared
// with the member directory.
//
// The count is a second query for the same reason ListDirectory's is — an
// offset past the end has no row to carry a total on — and it counts everyone
// matching the search rather than everyone waiting, so the number above a
// filtered queue describes the queue on screen.
func (s *ApprovalService) ListPending(ctx context.Context, query string, limit, offset int) ([]*domain.User, int, error) {
	if err := s.requireBootstrap(ctx); err != nil {
		return nil, 0, err
	}

	query, limit, offset, err := boundUserListing(query, limit, offset)
	if err != nil {
		return nil, 0, err
	}

	users, err := s.users.ListPendingUsersPage(ctx, query, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("listing pending users: %w", err)
	}

	total, err := s.users.CountPendingUsersMatching(ctx, query)
	if err != nil {
		return nil, 0, fmt.Errorf("counting pending users: %w", err)
	}

	return users, int(total), nil
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

	// Captured before the write. old_role is a fact about the moment before the
	// update, and reading it off user afterwards would depend on the repository
	// not having touched the value in hand — an in-memory one does, and the
	// entry silently becomes member -> member.
	oldRole := user.Role
	if err := s.users.UpdateUserRole(ctx, userID, domain.RoleMember); err != nil {
		return nil, fmt.Errorf("updating user role: %w", err)
	}
	// A failure here is post-commit and reported for the same reason the
	// bootstrap exit check below is: the promotion stands, and the operator
	// should hear that its audit entry did not.
	if err := s.recordApproval(ctx, userID, oldRole); err != nil {
		return nil, err
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

// recordApproval writes the role_history entry for a council approval that has
// already been applied.
//
// The reason names the mechanism rather than the approver. The council acts as
// a body here — approval during bootstrap takes one council member and no
// vote — and the endpoint does not carry which one of them clicked, so naming
// an individual would mean inventing an attribution. "council approval" is what
// the row can honestly say, and it is enough to tell this promotion apart from
// a vouch or an automatic one at a glance.
func (s *ApprovalService) recordApproval(ctx context.Context, userID string, oldRole domain.Role) error {
	if s.roleHistory == nil {
		s.logger.Warn("approval not recorded in role history: no writer wired", "user_id", userID)
		return nil
	}

	id, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("user approved but generating role history id: %v", err)
	}
	entry := &domain.RoleHistory{
		ID:        id.String(),
		UserID:    userID,
		OldRole:   oldRole,
		NewRole:   domain.RoleMember,
		Reason:    "council approval during bootstrap",
		CreatedAt: s.now(),
	}
	if err := s.roleHistory.CreateRoleHistoryEntry(ctx, entry); err != nil {
		// Unwrapped, like the promotion errors in VouchService: a post-commit
		// failure is not the council member's mistake, and a sentinel leaking
		// through here would render as a 404 or a 400 about a request that in
		// fact succeeded.
		return fmt.Errorf("user approved but recording role history: %v", err)
	}
	return nil
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
