package service

import (
	"fmt"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
)

// This file holds the automatic role policy as pure functions. Keeping the
// decisions separate from the repository writes in role_checker.go means every
// promotion and demotion rule can be exercised in a table test without a
// database, a clock, or a logger.

// demotionOutcome is what the demotion policy decided for one user.
type demotionOutcome int

const (
	// demotionNone means the user is above the threshold and was already in
	// good standing; nothing to record.
	demotionNone demotionOutcome = iota
	// demotionClear means trust recovered and the below-threshold timer
	// should be cleared.
	demotionClear
	// demotionMark means trust just dropped below the threshold and the timer
	// should start.
	demotionMark
	// demotionWait means the timer is running but has not yet elapsed.
	demotionWait
	// demotionDemote means the user has been below the threshold long enough
	// and should drop a role.
	demotionDemote
)

// demotionDecision is the full result of evaluating the demotion policy.
type demotionDecision struct {
	Outcome demotionOutcome
	// NewRole is meaningful only when Outcome is demotionDemote.
	NewRole domain.Role
	// Reason is meaningful only when Outcome is demotionDemote.
	Reason string
}

// nextRoleAfterDemotion returns the role a user drops to, and whether a
// demotion applies at all. Council members are governed by the community
// rather than by trust score, and banned users have nowhere further to fall,
// so neither is demotable.
func nextRoleAfterDemotion(current domain.Role) (domain.Role, bool) {
	switch current {
	case domain.RoleModerator:
		return domain.RoleMember, true
	case domain.RoleMember:
		return domain.RolePending, true
	default:
		return "", false
	}
}

// evaluateDemotion applies the sustained-low-trust policy: a user must sit
// below DemotionTrustThreshold continuously for DemotionConsecutiveDays before
// losing a role. Recovering above the threshold at any point resets the clock,
// so a single bad week cannot demote someone.
func evaluateDemotion(u RoleCheckerUser, now time.Time) demotionDecision {
	if u.TrustScore >= domain.DemotionTrustThreshold {
		if u.TrustBelowSince != nil {
			return demotionDecision{Outcome: demotionClear}
		}
		return demotionDecision{Outcome: demotionNone}
	}

	if u.TrustBelowSince == nil {
		return demotionDecision{Outcome: demotionMark}
	}

	daysBelow := now.Sub(*u.TrustBelowSince).Hours() / 24
	if daysBelow < float64(domain.DemotionConsecutiveDays) {
		return demotionDecision{Outcome: demotionWait}
	}

	newRole, ok := nextRoleAfterDemotion(u.Role)
	if !ok {
		return demotionDecision{Outcome: demotionWait}
	}

	return demotionDecision{
		Outcome: demotionDemote,
		NewRole: newRole,
		Reason: fmt.Sprintf("auto-demotion: trust %.1f < %.1f for %d consecutive days",
			u.TrustScore, domain.DemotionTrustThreshold, int(daysBelow)),
	}
}

// promotionGate reports which promotion criteria a user already satisfies
// without consulting the vouch count, which is the expensive part to look up.
type promotionGate struct {
	// Eligible is true when role, trust, and tenure all pass, meaning the
	// moderator vouch count is worth querying.
	Eligible bool
	// DaysSinceJoin is carried through so the reason string can report it.
	DaysSinceJoin float64
}

// evaluatePromotionGate checks the criteria that need no database access.
// Only members are promoted: pending users have not been vouched in yet,
// moderators are already there, and council is not an automatic role.
func evaluatePromotionGate(u RoleCheckerUser, now time.Time) promotionGate {
	daysSinceJoin := now.Sub(u.JoinedAt).Hours() / 24

	eligible := u.Role == domain.RoleMember &&
		u.TrustScore >= domain.PromotionTrustThreshold &&
		daysSinceJoin >= float64(domain.PromotionMinDays)

	return promotionGate{Eligible: eligible, DaysSinceJoin: daysSinceJoin}
}

// evaluatePromotion completes the promotion decision once the moderator vouch
// count is known. Requiring endorsement by existing moderators is what keeps
// promotion a community judgement rather than a pure function of activity.
func evaluatePromotion(u RoleCheckerUser, gate promotionGate, modVouches int64) (promote bool, reason string) {
	if !gate.Eligible || modVouches < int64(domain.PromotionMinModVouches) {
		return false, ""
	}

	return true, fmt.Sprintf(
		"auto-promotion: trust %.1f >= %.1f, member for %d days, %d moderator vouches",
		u.TrustScore, domain.PromotionTrustThreshold, int(gate.DaysSinceJoin), modVouches)
}
