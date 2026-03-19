package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/google/uuid"
)

// RoleCheckerRepository abstracts the database queries needed by the role checker.
type RoleCheckerRepository interface {
	ListActiveNonBannedUsers(ctx context.Context) ([]RoleCheckerUser, error)
	CountActiveModeratorVouchesForUser(ctx context.Context, userID string) (int64, error)
	UpdateUserRole(ctx context.Context, id string, role domain.Role) error
	UpdateUserTrustBelowSince(ctx context.Context, id string, since time.Time) error
	ClearUserTrustBelowSince(ctx context.Context, id string) error
	CreateRoleHistoryEntry(ctx context.Context, entry *domain.RoleHistory) error
}

// RoleCheckerUser is the user data needed by the role checker.
type RoleCheckerUser struct {
	ID              string
	DisplayName     string
	TrustScore      float64
	Role            domain.Role
	JoinedAt        time.Time
	TrustBelowSince *time.Time
}

// RoleChange records a single promotion or demotion.
type RoleChange struct {
	UserID      string
	DisplayName string
	OldRole     domain.Role
	NewRole     domain.Role
	Reason      string
}

// RoleCheckResult summarizes the outcome of a role check run.
type RoleCheckResult struct {
	UsersChecked int
	Promotions   []RoleChange
	Demotions    []RoleChange
	Cleared      int // number of users whose TrustBelowSince was cleared
	Marked       int // number of users whose TrustBelowSince was set
}

// RoleChecker evaluates all active users for automatic promotion and demotion.
type RoleChecker struct {
	repo   RoleCheckerRepository
	logger *slog.Logger
	now    func() time.Time
}

// NewRoleChecker creates a new RoleChecker service.
func NewRoleChecker(repo RoleCheckerRepository, logger *slog.Logger, clock func() time.Time) *RoleChecker {
	if clock == nil {
		clock = time.Now
	}
	return &RoleChecker{
		repo:   repo,
		logger: logger,
		now:    clock,
	}
}

// Run iterates all active non-banned users and evaluates promotion/demotion criteria.
func (rc *RoleChecker) Run(ctx context.Context) (*RoleCheckResult, error) {
	now := rc.now()

	users, err := rc.repo.ListActiveNonBannedUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing active users: %w", err)
	}

	result := &RoleCheckResult{
		UsersChecked: len(users),
	}

	for _, u := range users {
		// Council members are never auto-promoted or demoted.
		if u.Role == domain.RoleCouncil {
			continue
		}

		if err := rc.checkDemotion(ctx, u, now, result); err != nil {
			rc.logger.Error("demotion check failed", "user_id", u.ID, "error", err)
			continue
		}

		if err := rc.checkPromotion(ctx, u, now, result); err != nil {
			rc.logger.Error("promotion check failed", "user_id", u.ID, "error", err)
			continue
		}
	}

	return result, nil
}

// checkPromotion evaluates whether a member should be promoted to moderator.
func (rc *RoleChecker) checkPromotion(ctx context.Context, u RoleCheckerUser, now time.Time, result *RoleCheckResult) error {
	// Only members can be promoted.
	if u.Role != domain.RoleMember {
		return nil
	}

	// Trust must meet the promotion threshold.
	if u.TrustScore < domain.PromotionTrustThreshold {
		return nil
	}

	// Must have been a member for at least PromotionMinDays.
	daysSinceJoin := now.Sub(u.JoinedAt).Hours() / 24
	if daysSinceJoin < float64(domain.PromotionMinDays) {
		return nil
	}

	// Must be vouched by at least PromotionMinModVouches moderators/council.
	modVouches, err := rc.repo.CountActiveModeratorVouchesForUser(ctx, u.ID)
	if err != nil {
		return fmt.Errorf("counting mod vouches: %w", err)
	}
	if modVouches < int64(domain.PromotionMinModVouches) {
		return nil
	}

	// All criteria met -- promote.
	reason := fmt.Sprintf("auto-promotion: trust %.1f >= %.1f, member for %d days, %d moderator vouches",
		u.TrustScore, domain.PromotionTrustThreshold, int(daysSinceJoin), modVouches)

	if err := rc.changeRole(ctx, u, domain.RoleModerator, reason, now); err != nil {
		return err
	}

	change := RoleChange{
		UserID:      u.ID,
		DisplayName: u.DisplayName,
		OldRole:     u.Role,
		NewRole:     domain.RoleModerator,
		Reason:      reason,
	}
	result.Promotions = append(result.Promotions, change)
	rc.logger.Info("user promoted", "user_id", u.ID, "display_name", u.DisplayName, "old_role", u.Role, "new_role", domain.RoleModerator)

	return nil
}

// checkDemotion evaluates whether a user's trust has fallen below the demotion threshold.
func (rc *RoleChecker) checkDemotion(ctx context.Context, u RoleCheckerUser, now time.Time, result *RoleCheckResult) error {
	if u.TrustScore >= domain.DemotionTrustThreshold {
		// Trust is above threshold. If TrustBelowSince was set, clear it.
		if u.TrustBelowSince != nil {
			if err := rc.repo.ClearUserTrustBelowSince(ctx, u.ID); err != nil {
				return fmt.Errorf("clearing trust_below_since: %w", err)
			}
			result.Cleared++
			rc.logger.Info("trust recovered, cleared trust_below_since", "user_id", u.ID, "trust", u.TrustScore)
		}
		return nil
	}

	// Trust is below the demotion threshold.
	if u.TrustBelowSince == nil {
		// First time below threshold -- mark the timestamp.
		if err := rc.repo.UpdateUserTrustBelowSince(ctx, u.ID, now); err != nil {
			return fmt.Errorf("setting trust_below_since: %w", err)
		}
		result.Marked++
		rc.logger.Info("trust below threshold, marked trust_below_since", "user_id", u.ID, "trust", u.TrustScore)
		return nil
	}

	// Trust has been below threshold for some time -- check if it's been long enough.
	daysBelowThreshold := now.Sub(*u.TrustBelowSince).Hours() / 24
	if daysBelowThreshold < float64(domain.DemotionConsecutiveDays) {
		return nil
	}

	// Demotion criteria met.
	var newRole domain.Role
	switch u.Role {
	case domain.RoleModerator:
		newRole = domain.RoleMember
	case domain.RoleMember:
		newRole = domain.RolePending
	default:
		return nil
	}

	reason := fmt.Sprintf("auto-demotion: trust %.1f < %.1f for %d consecutive days",
		u.TrustScore, domain.DemotionTrustThreshold, int(daysBelowThreshold))

	if err := rc.changeRole(ctx, u, newRole, reason, now); err != nil {
		return err
	}

	// Clear trust_below_since after demotion so the clock resets.
	if err := rc.repo.ClearUserTrustBelowSince(ctx, u.ID); err != nil {
		rc.logger.Error("failed to clear trust_below_since after demotion", "user_id", u.ID, "error", err)
	}

	change := RoleChange{
		UserID:      u.ID,
		DisplayName: u.DisplayName,
		OldRole:     u.Role,
		NewRole:     newRole,
		Reason:      reason,
	}
	result.Demotions = append(result.Demotions, change)
	rc.logger.Info("user demoted", "user_id", u.ID, "display_name", u.DisplayName, "old_role", u.Role, "new_role", newRole)

	return nil
}

// changeRole updates the user's role and records the change in role_history.
func (rc *RoleChecker) changeRole(ctx context.Context, u RoleCheckerUser, newRole domain.Role, reason string, now time.Time) error {
	if err := rc.repo.UpdateUserRole(ctx, u.ID, newRole); err != nil {
		return fmt.Errorf("updating role: %w", err)
	}

	id, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("generating role history id: %w", err)
	}

	entry := &domain.RoleHistory{
		ID:        id.String(),
		UserID:    u.ID,
		OldRole:   u.Role,
		NewRole:   newRole,
		Reason:    reason,
		CreatedAt: now,
	}
	if err := rc.repo.CreateRoleHistoryEntry(ctx, entry); err != nil {
		return fmt.Errorf("recording role history: %w", err)
	}

	return nil
}
