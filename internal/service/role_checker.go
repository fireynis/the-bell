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
	ListActiveNonBannedUsers(ctx context.Context) ([]*domain.User, error)
	CountActiveModeratorVouchesForUser(ctx context.Context, userID string) (int64, error)
	UpdateUserRole(ctx context.Context, id string, role domain.Role) error
	UpdateUserTrustBelowSince(ctx context.Context, id string, since time.Time) error
	ClearUserTrustBelowSince(ctx context.Context, id string) error
	CreateRoleHistoryEntry(ctx context.Context, entry *domain.RoleHistory) error
}

// The role checker works directly on *domain.User rather than a narrowed
// projection of it. There used to be a RoleCheckerUser struct here holding the
// six fields the policy reads, which the Postgres adapter filled in by hand —
// which meant internal/repository/postgres had to import internal/service to
// name a plain data type, inverting the dependency between persistence and
// business logic. Every field of it was already a field of domain.User, and
// ListActiveNonBannedUsers is a SELECT *, so the projection narrowed nothing
// that had not already been fetched.

// TrustScoreWriter persists a recalculated trust score.
//
// It is the same one method cache.TrustWorker writes through, declared again
// here so the role checker depends on the write rather than on the worker.
type TrustScoreWriter interface {
	UpdateUserTrustScore(ctx context.Context, id string, score float64) error
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

	// inputs and scores are the trust refresher; see SetTrustRefresher. Both
	// are nil until it is called.
	inputs TrustInputs
	scores TrustScoreWriter
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

// SetTrustRefresher gives the checker what it needs to recompute a user's
// trust score before judging them by it.
//
// It is optional in the type but not in practice: internal/app always supplies
// it. Without it the checker falls back to the stored score, which is what made
// a town's first check-roles run destructive — see refreshTrust.
func (rc *RoleChecker) SetTrustRefresher(inputs TrustInputs, scores TrustScoreWriter) {
	rc.inputs = inputs
	rc.scores = scores
}

// Run iterates all active non-banned users and evaluates promotion/demotion criteria.
func (rc *RoleChecker) Run(ctx context.Context) (*RoleCheckResult, error) {
	now := rc.now()

	users, err := rc.repo.ListActiveNonBannedUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing active users: %w", err)
	}

	if rc.inputs == nil {
		rc.logger.Warn("role checker has no trust refresher: roles will be decided by stored scores, " +
			"which may never have been recalculated")
	}

	result := &RoleCheckResult{
		UsersChecked: len(users),
	}

	for _, u := range users {
		// Ahead of the council skip on purpose. Council never changes role
		// here, but on a deployment without Redis this run is the only thing
		// that ever recalculates anybody, and a council member left on a stale
		// score is one that cannot vouch.
		if err := rc.refreshTrust(ctx, u, now); err != nil {
			rc.logger.Error("trust refresh failed", "user_id", u.ID, "error", err)
			continue
		}

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

// refreshTrust recomputes u.TrustScore before the policy reads it, and persists
// the result.
//
// Without this, the policy judged a number that in a great many towns had never
// been computed at all. Users are created at 50.0 and only the Redis-backed
// trust worker ever recalculates; a deployment running without Redis — a
// documented mode — leaves every user at that default forever. The demotion
// threshold is a flat 70.0, so thirty days after such a town opened, this run
// demoted its entire membership to pending on the strength of a placeholder.
//
// Recomputing here also makes `bell check-roles` the recalculation sweep for
// Redis-less deployments: penalties decay and tenure accrues on the schedule
// the operator runs it on. Where Redis is present the trust worker's own sweep
// has usually already written the same number, and recomputing it twice is
// cheap next to demoting someone on a stale one.
//
// A user whose score will not compute is skipped by the caller rather than
// judged on the old value. Persisting is best-effort by contrast: the run holds
// the right number either way, so a failed write costs a stale row, not a role.
func (rc *RoleChecker) refreshTrust(ctx context.Context, u *domain.User, now time.Time) error {
	if rc.inputs == nil {
		return nil
	}

	score, err := CalcCompositeTrust(ctx, rc.inputs, u.ID, now)
	if err != nil {
		return fmt.Errorf("recalculating trust: %w", err)
	}
	u.TrustScore = score

	if rc.scores == nil {
		return nil
	}
	if err := rc.scores.UpdateUserTrustScore(ctx, u.ID, score); err != nil {
		rc.logger.Error("persisting refreshed trust score failed", "user_id", u.ID, "error", err)
	}
	return nil
}

// checkPromotion evaluates whether a member should be promoted to moderator.
// The policy itself lives in evaluatePromotionGate/evaluatePromotion; this
// method only supplies the vouch count and applies the outcome.
func (rc *RoleChecker) checkPromotion(ctx context.Context, u *domain.User, now time.Time, result *RoleCheckResult) error {
	gate := evaluatePromotionGate(u, now)
	if !gate.Eligible {
		return nil
	}

	modVouches, err := rc.repo.CountActiveModeratorVouchesForUser(ctx, u.ID)
	if err != nil {
		return fmt.Errorf("counting mod vouches: %w", err)
	}

	promote, reason := evaluatePromotion(u, gate, modVouches)
	if !promote {
		return nil
	}

	if err := rc.changeRole(ctx, u, domain.RoleModerator, reason, now); err != nil {
		return err
	}

	result.Promotions = append(result.Promotions, RoleChange{
		UserID:      u.ID,
		DisplayName: u.DisplayName,
		OldRole:     u.Role,
		NewRole:     domain.RoleModerator,
		Reason:      reason,
	})
	rc.logger.Info("user promoted", "user_id", u.ID, "display_name", u.DisplayName, "old_role", u.Role, "new_role", domain.RoleModerator)

	return nil
}

// checkDemotion applies the sustained-low-trust policy decided by
// evaluateDemotion.
func (rc *RoleChecker) checkDemotion(ctx context.Context, u *domain.User, now time.Time, result *RoleCheckResult) error {
	return rc.applyDemotion(ctx, u, evaluateDemotion(u, now), now, result)
}

// applyDemotion writes whatever evaluateDemotion decided. Every outcome is
// handled explicitly and an unrecognized one is an error, so a demotionOutcome
// added to the policy later cannot silently fall through into taking a role
// away from someone.
func (rc *RoleChecker) applyDemotion(ctx context.Context, u *domain.User, decision demotionDecision, now time.Time, result *RoleCheckResult) error {
	switch decision.Outcome {
	case demotionNone, demotionWait:
		return nil

	case demotionClear:
		if err := rc.repo.ClearUserTrustBelowSince(ctx, u.ID); err != nil {
			return fmt.Errorf("clearing trust_below_since: %w", err)
		}
		result.Cleared++
		rc.logger.Info("trust recovered, cleared trust_below_since", "user_id", u.ID, "trust", u.TrustScore)
		return nil

	case demotionMark:
		if err := rc.repo.UpdateUserTrustBelowSince(ctx, u.ID, now); err != nil {
			return fmt.Errorf("setting trust_below_since: %w", err)
		}
		result.Marked++
		rc.logger.Info("trust below threshold, marked trust_below_since", "user_id", u.ID, "trust", u.TrustScore)
		return nil

	case demotionDemote:
		// Handled below, where the role change and its bookkeeping live.

	default:
		return fmt.Errorf("unhandled demotion outcome %d", decision.Outcome)
	}

	if err := rc.changeRole(ctx, u, decision.NewRole, decision.Reason, now); err != nil {
		return err
	}

	// Clear trust_below_since after demotion so the clock resets at the new role.
	if err := rc.repo.ClearUserTrustBelowSince(ctx, u.ID); err != nil {
		rc.logger.Error("failed to clear trust_below_since after demotion", "user_id", u.ID, "error", err)
	}

	result.Demotions = append(result.Demotions, RoleChange{
		UserID:      u.ID,
		DisplayName: u.DisplayName,
		OldRole:     u.Role,
		NewRole:     decision.NewRole,
		Reason:      decision.Reason,
	})
	rc.logger.Info("user demoted", "user_id", u.ID, "display_name", u.DisplayName, "old_role", u.Role, "new_role", decision.NewRole)

	return nil
}

// changeRole updates the user's role and records the change in role_history.
func (rc *RoleChecker) changeRole(ctx context.Context, u *domain.User, newRole domain.Role, reason string, now time.Time) error {
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
