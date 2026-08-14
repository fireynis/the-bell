package service

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
)

var policyNow = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

// daysAgo returns a timestamp the given number of days before policyNow.
func daysAgo(d float64) time.Time {
	return policyNow.Add(-time.Duration(d * float64(24*time.Hour)))
}

func ptrTime(t time.Time) *time.Time { return &t }

func TestNextRoleAfterDemotion(t *testing.T) {
	tests := []struct {
		name    string
		current domain.Role
		want    domain.Role
		wantOK  bool
	}{
		{"moderator drops to member", domain.RoleModerator, domain.RoleMember, true},
		{"member drops to pending", domain.RoleMember, domain.RolePending, true},
		{"pending has nowhere to fall", domain.RolePending, "", false},
		{"council is not auto-demoted", domain.RoleCouncil, "", false},
		{"banned is not demoted further", domain.RoleBanned, "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := nextRoleAfterDemotion(tt.current)
			if ok != tt.wantOK || got != tt.want {
				t.Errorf("nextRoleAfterDemotion(%q) = (%q, %v), want (%q, %v)",
					tt.current, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

// evaluateDemotion reads the demotable set from domain and the destination role
// from nextRoleAfterDemotion. If those two ever disagree, one of them wins
// silently: a role with a threshold but no destination stalls on demotionWait
// forever, and a role with a destination but no threshold is never judged at
// all. Neither shows up as a failure anywhere else.
func TestDemotableRolesAgreeWithTheirDestinations(t *testing.T) {
	for _, role := range []domain.Role{
		domain.RolePending, domain.RoleMember, domain.RoleModerator,
		domain.RoleCouncil, domain.RoleBanned,
	} {
		t.Run(string(role), func(t *testing.T) {
			_, hasThreshold := domain.DemotionTrustThresholdFor(role)
			_, hasDestination := nextRoleAfterDemotion(role)
			if hasThreshold != hasDestination {
				t.Errorf("%q: has a demotion threshold = %v, has a role to drop to = %v; "+
					"they must agree", role, hasThreshold, hasDestination)
			}
		})
	}
}

func TestEvaluateDemotion(t *testing.T) {
	// Members are judged against 35, moderators against 70, and both need
	// DemotionConsecutiveDays (30) below their own threshold.
	tests := []struct {
		name        string
		user        *domain.User
		wantOutcome demotionOutcome
		wantRole    domain.Role
	}{
		{
			name:        "healthy trust, never flagged",
			user:        &domain.User{Role: domain.RoleMember, TrustScore: 80},
			wantOutcome: demotionNone,
		},
		{
			name:        "member exactly at their threshold counts as healthy",
			user:        &domain.User{Role: domain.RoleMember, TrustScore: domain.MemberDemotionTrustThreshold},
			wantOutcome: demotionNone,
		},
		{
			name:        "moderator exactly at their threshold counts as healthy",
			user:        &domain.User{Role: domain.RoleModerator, TrustScore: domain.ModeratorDemotionTrustThreshold},
			wantOutcome: demotionNone,
		},
		{
			name: "recovered above threshold clears the timer",
			user: &domain.User{
				Role: domain.RoleMember, TrustScore: 75,
				TrustBelowSince: ptrTime(daysAgo(10)),
			},
			wantOutcome: demotionClear,
		},
		{
			name:        "member's first drop below 35 starts the timer",
			user:        &domain.User{Role: domain.RoleMember, TrustScore: 34.9},
			wantOutcome: demotionMark,
		},
		{
			name:        "moderator's first drop below 70 starts the timer",
			user:        &domain.User{Role: domain.RoleModerator, TrustScore: 69.9},
			wantOutcome: demotionMark,
		},
		{
			name: "below threshold but not long enough",
			user: &domain.User{
				Role: domain.RoleMember, TrustScore: 20,
				TrustBelowSince: ptrTime(daysAgo(29)),
			},
			wantOutcome: demotionWait,
		},
		{
			name: "member demoted to pending after the full window",
			user: &domain.User{
				Role: domain.RoleMember, TrustScore: 20,
				TrustBelowSince: ptrTime(daysAgo(30)),
			},
			wantOutcome: demotionDemote,
			wantRole:    domain.RolePending,
		},
		{
			name: "moderator demoted to member after the full window",
			user: &domain.User{
				Role: domain.RoleModerator, TrustScore: 12,
				TrustBelowSince: ptrTime(daysAgo(45)),
			},
			wantOutcome: demotionDemote,
			wantRole:    domain.RoleMember,
		},
		{
			name: "pending user is never demoted further",
			user: &domain.User{
				Role: domain.RolePending, TrustScore: 5,
				TrustBelowSince: ptrTime(daysAgo(365)),
			},
			// Not demotionWait: pending is on no clock at all, so the timer it
			// inherited from being a member is cleared rather than left to be
			// spent the instant a vouch restores the role.
			wantOutcome: demotionClear,
		},
		{
			name:        "pending user with no timer needs nothing recorded",
			user:        &domain.User{Role: domain.RolePending, TrustScore: 5},
			wantOutcome: demotionNone,
		},
		{
			name: "council is exempt however low the score",
			user: &domain.User{
				Role: domain.RoleCouncil, TrustScore: 0,
				TrustBelowSince: ptrTime(daysAgo(365)),
			},
			wantOutcome: demotionClear,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := evaluateDemotion(tt.user, policyNow)
			if got.Outcome != tt.wantOutcome {
				t.Errorf("Outcome = %v, want %v", got.Outcome, tt.wantOutcome)
			}
			if got.NewRole != tt.wantRole {
				t.Errorf("NewRole = %q, want %q", got.NewRole, tt.wantRole)
			}
			if tt.wantOutcome == demotionDemote && got.Reason == "" {
				t.Error("expected a non-empty reason for a demotion")
			}
		})
	}
}

// --- Threshold calibration, measured against the real trust calculator ---
//
// The tests below exist because the demotion thresholds are only defensible in
// terms of scores CalcCompositeTrust actually produces. They build the profiles
// named in the derivation above domain.MemberDemotionTrustThreshold, run the
// real calculator over them, assert the score the derivation claims, and then
// assert what the policy does with it. A change to any weight, cap or penalty
// constant that moves a real member across a threshold fails here rather than
// in production thirty days later.

// trustProfile is one member's calculator inputs, in the terms the derivation
// comment uses.
type trustProfile struct {
	tenureDays int
	posts      int64
	reactions  int64
	vouches    int64
	avgTrust   float64
	penalties  []domain.TrustPenalty
}

// score runs the real CalcCompositeTrust over the profile at policyNow.
func (p trustProfile) score(t *testing.T) float64 {
	t.Helper()

	inputs := &fakeTrustInputs{
		user: &domain.User{
			ID:       "u",
			Role:     domain.RoleMember,
			JoinedAt: policyNow.AddDate(0, 0, -p.tenureDays),
		},
		posts:     p.posts,
		reactions: p.reactions,
		vouches:   p.vouches,
		avgTrust:  p.avgTrust,
		penalties: p.penalties,
	}

	score, err := CalcCompositeTrust(context.Background(), inputs, "u", policyNow)
	if err != nil {
		t.Fatalf("CalcCompositeTrust: %v", err)
	}
	return score
}

// withPenalties returns a copy of the profile carrying the given penalties, so
// the clean profiles stay readable as the baseline they are.
func (p trustProfile) withPenalties(penalties ...domain.TrustPenalty) trustProfile {
	p.penalties = penalties
	return p
}

// The three profiles the thresholds were derived from.
var (
	// quietMember reads more than they post and has never been sanctioned.
	// This is the floor of "fine", and the number the member threshold has to
	// stay clearly below.
	quietMember = trustProfile{tenureDays: 200, posts: 20, reactions: 60, vouches: 2, avgTrust: 70}
	// engagedMember posts regularly and is well vouched for.
	engagedMember = trustProfile{tenureDays: 365, posts: 45, reactions: 135, vouches: 3, avgTrust: 80}
	// strongMember has both activity caps and four good vouches — still short
	// of the 85 promotion bar.
	strongMember = trustProfile{tenureDays: 365, posts: 90, reactions: 270, vouches: 4, avgTrust: 85}
)

// freshPenalty builds a penalty applied at policyNow, so no decay has run yet.
// decayDays of 0 means permanent, matching domain.PenaltyDecayDays.
func freshPenalty(points float64, decayDays int) domain.TrustPenalty {
	p := domain.TrustPenalty{PenaltyAmount: points, CreatedAt: policyNow}
	if decayDays > 0 {
		p.DecaysAt = ptrTime(policyNow.AddDate(0, 0, decayDays))
	}
	return p
}

// The sanctions the derivation reasons about, in the calculator's terms.
// domain.DirectPenalty and domain.PenaltyDecayDays supply the points and
// windows; a propagated ban penalty is directPenalty * decayRate^hops and
// inherits the ban's nil DecaysAt, i.e. it is permanent.
func freshSuspendPenalty() domain.TrustPenalty { return freshPenalty(40, 365) } // severity 4
func freshMutePenalty() domain.TrustPenalty    { return freshPenalty(25, 270) } // severity 3
func banPenaltyAtThreeHops() domain.TrustPenalty {
	return freshPenalty(100*0.75*0.75*0.75, 0) // 42.1875, permanent
}

// demotedAsMember reports whether a member holding this score, having been
// below their threshold for the full window, loses the role.
func demotedAsMember(t *testing.T, score float64) bool {
	t.Helper()
	u := &domain.User{Role: domain.RoleMember, TrustScore: score, TrustBelowSince: ptrTime(daysAgo(30))}
	return evaluateDemotion(u, policyNow).Outcome == demotionDemote
}

func TestDemotionThresholds_AgainstRealTrustScores(t *testing.T) {
	tests := []struct {
		name string
		// derivation is the arithmetic from the comment above
		// domain.MemberDemotionTrustThreshold, restated so a failure says which
		// step moved.
		derivation  string
		profile     trustProfile
		wantScore   float64
		wantDemoted bool
	}{
		{
			name:        "quiet but fine member is safe",
			derivation:  "8.22 tenure + 4.44 activity + 7.35 voucher + 30.00 moderation",
			profile:     quietMember,
			wantScore:   50.01,
			wantDemoted: false,
		},
		{
			name:        "engaged member is safe",
			derivation:  "15.00 + 10.00 + 12.60 + 30.00",
			profile:     engagedMember,
			wantScore:   67.60,
			wantDemoted: false,
		},
		{
			name:        "very strong member is safe",
			derivation:  "15.00 + 20.00 + 17.85 + 30.00",
			profile:     strongMember,
			wantScore:   82.85,
			wantDemoted: false,
		},
		{
			// The case the flat 70 got wrong in the other direction: a member
			// who served a suspension should feel it — 12 composite points —
			// without also losing their standing in the town.
			name:        "quiet member serving a fresh suspension is still safe",
			derivation:  "20.01 from the other three components + 60 moderation * 0.30 = 18.00",
			profile:     quietMember.withPenalties(freshSuspendPenalty()),
			wantScore:   38.01,
			wantDemoted: false,
		},
		{
			// Being three hops from a ban is not, on its own, evidence about
			// this member. It costs them 12.66 composite points and leaves them
			// above the line.
			name:        "ban propagated three hops does not demote on its own",
			derivation:  "20.01 + 57.81 moderation * 0.30 = 17.34",
			profile:     quietMember.withPenalties(banPenaltyAtThreeHops()),
			wantScore:   37.36,
			wantDemoted: false,
		},
		{
			// Collapsed trust: the fallout of vouching into a banned corner of
			// the graph *and* a sanction of their own. This is what the member
			// threshold is for.
			name:        "propagated ban plus the member's own mute does demote",
			derivation:  "20.01 + 32.81 moderation * 0.30 = 9.84",
			profile:     quietMember.withPenalties(banPenaltyAtThreeHops(), freshMutePenalty()),
			wantScore:   29.86,
			wantDemoted: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := tt.profile.score(t)
			if math.Abs(score-tt.wantScore) > 0.005 {
				t.Errorf("CalcCompositeTrust = %.4f, want %.2f (%s)", score, tt.wantScore, tt.derivation)
			}
			if got := demotedAsMember(t, score); got != tt.wantDemoted {
				t.Errorf("demoted at trust %.2f = %v, want %v (member threshold is %.1f)",
					score, got, tt.wantDemoted, domain.MemberDemotionTrustThreshold)
			}
		})
	}
}

// The flat 70.0 is what a member scoring in the fifties and sixties would have
// been judged against. Both the quiet and the engaged profile sit below it —
// only the very strong one clears it — which is the whole reason the member
// threshold moved: with periodic recalculation working, the old value demoted
// most of the town.
func TestFlatSeventyWouldHaveDemotedHealthyMembers(t *testing.T) {
	for name, profile := range map[string]trustProfile{
		"quiet":   quietMember,
		"engaged": engagedMember,
	} {
		t.Run(name, func(t *testing.T) {
			if score := profile.score(t); score >= domain.ModeratorDemotionTrustThreshold {
				t.Errorf("score %.2f is at or above the old flat threshold %.1f; "+
					"this test no longer demonstrates anything and the derivation needs revisiting",
					score, domain.ModeratorDemotionTrustThreshold)
			}
		})
	}
}

// The same score means different things at different ranks. 71 is comfortable
// for a member and marginal for a moderator; 69 unseats a moderator and leaves
// a member alone.
func TestDemotionThreshold_SameScoreDiffersByRole(t *testing.T) {
	tests := []struct {
		name        string
		role        domain.Role
		trust       float64
		wantOutcome demotionOutcome
		wantRole    domain.Role
	}{
		// Safe users below expect demotionClear rather than demotionNone: they
		// are set up mid-clock, and being above your threshold is what stops
		// that clock.
		{"member at 71 is well clear", domain.RoleMember, 71, demotionClear, ""},
		{"moderator at 71 is clear too", domain.RoleModerator, 71, demotionClear, ""},
		{"member at 69 is still clear", domain.RoleMember, 69, demotionClear, ""},
		{"moderator at 69 drops to member", domain.RoleModerator, 69, demotionDemote, domain.RoleMember},
		{"member at 34 drops to pending", domain.RoleMember, 34, demotionDemote, domain.RolePending},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Already below for the full window, so the only question left is
			// which threshold applies.
			u := &domain.User{Role: tt.role, TrustScore: tt.trust, TrustBelowSince: ptrTime(daysAgo(30))}

			got := evaluateDemotion(u, policyNow)
			if got.Outcome != tt.wantOutcome {
				t.Errorf("Outcome = %v, want %v", got.Outcome, tt.wantOutcome)
			}
			if got.NewRole != tt.wantRole {
				t.Errorf("NewRole = %q, want %q", got.NewRole, tt.wantRole)
			}
		})
	}
}

// A moderator who loses the role must not keep falling. Under the flat 70 a
// moderator demoted at 65 was still below the threshold as a member, so the
// next run restarted the clock and dropped them to pending thirty days later —
// costing them posting rights over a judgement that was only ever about
// moderator standing.
func TestDemotionDoesNotCascadePastMember(t *testing.T) {
	const trust = 65.0

	moderator := &domain.User{Role: domain.RoleModerator, TrustScore: trust, TrustBelowSince: ptrTime(daysAgo(30))}
	first := evaluateDemotion(moderator, policyNow)
	if first.Outcome != demotionDemote || first.NewRole != domain.RoleMember {
		t.Fatalf("moderator at %.0f: got (%v, %q), want (demotionDemote, member)", trust, first.Outcome, first.NewRole)
	}

	// The checker clears trust_below_since after a demotion, so the demoted
	// moderator faces the next run as a member with a fresh clock.
	demoted := &domain.User{Role: first.NewRole, TrustScore: trust}
	if second := evaluateDemotion(demoted, policyNow); second.Outcome != demotionNone {
		t.Errorf("demoted moderator as a member at %.0f: Outcome = %v, want demotionNone", trust, second.Outcome)
	}
}

// A user whose trust recovers must not carry the old timer forward, otherwise a
// later dip would demote them immediately instead of restarting the 30-day clock.
func TestEvaluateDemotion_RecoveryResetsTheClock(t *testing.T) {
	longAgo := ptrTime(daysAgo(300))

	recovered := &domain.User{Role: domain.RoleMember, TrustScore: 90, TrustBelowSince: longAgo}
	if got := evaluateDemotion(recovered, policyNow); got.Outcome != demotionClear {
		t.Fatalf("recovered user: Outcome = %v, want demotionClear", got.Outcome)
	}

	// After the clear, TrustBelowSince is nil; a fresh dip only marks.
	dippedAgain := &domain.User{Role: domain.RoleMember, TrustScore: 20}
	if got := evaluateDemotion(dippedAgain, policyNow); got.Outcome != demotionMark {
		t.Fatalf("re-dipped user: Outcome = %v, want demotionMark", got.Outcome)
	}
}

func TestEvaluatePromotionGate(t *testing.T) {
	// PromotionTrustThreshold is 85, PromotionMinDays is 90.
	tests := []struct {
		name         string
		user         *domain.User
		wantEligible bool
	}{
		{
			name:         "qualified member",
			user:         &domain.User{Role: domain.RoleMember, TrustScore: 90, JoinedAt: daysAgo(120)},
			wantEligible: true,
		},
		{
			name:         "exactly at both thresholds qualifies",
			user:         &domain.User{Role: domain.RoleMember, TrustScore: 85, JoinedAt: daysAgo(90)},
			wantEligible: true,
		},
		{
			name:         "trust just below threshold",
			user:         &domain.User{Role: domain.RoleMember, TrustScore: 84.9, JoinedAt: daysAgo(120)},
			wantEligible: false,
		},
		{
			name:         "tenure just below minimum",
			user:         &domain.User{Role: domain.RoleMember, TrustScore: 95, JoinedAt: daysAgo(89)},
			wantEligible: false,
		},
		{
			name:         "pending users are not promoted to moderator",
			user:         &domain.User{Role: domain.RolePending, TrustScore: 99, JoinedAt: daysAgo(500)},
			wantEligible: false,
		},
		{
			name:         "existing moderator is not re-promoted",
			user:         &domain.User{Role: domain.RoleModerator, TrustScore: 99, JoinedAt: daysAgo(500)},
			wantEligible: false,
		},
		{
			name:         "council is not an automatic role",
			user:         &domain.User{Role: domain.RoleCouncil, TrustScore: 99, JoinedAt: daysAgo(500)},
			wantEligible: false,
		},
		{
			name:         "banned user is never eligible",
			user:         &domain.User{Role: domain.RoleBanned, TrustScore: 99, JoinedAt: daysAgo(500)},
			wantEligible: false,
		},
		{
			name:         "future join date yields negative tenure",
			user:         &domain.User{Role: domain.RoleMember, TrustScore: 99, JoinedAt: policyNow.AddDate(0, 0, 5)},
			wantEligible: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := evaluatePromotionGate(tt.user, policyNow); got.Eligible != tt.wantEligible {
				t.Errorf("Eligible = %v, want %v", got.Eligible, tt.wantEligible)
			}
		})
	}
}

func TestEvaluatePromotion(t *testing.T) {
	qualified := &domain.User{Role: domain.RoleMember, TrustScore: 90, JoinedAt: daysAgo(120)}
	gate := evaluatePromotionGate(qualified, policyNow)
	if !gate.Eligible {
		t.Fatal("test setup: expected the user to pass the gate")
	}

	tests := []struct {
		name        string
		gate        promotionGate
		modVouches  int64
		wantPromote bool
	}{
		{"enough moderator vouches", gate, domain.PromotionMinModVouches, true},
		{"more than enough vouches", gate, 10, true},
		{"one vouch short", gate, domain.PromotionMinModVouches - 1, false},
		{"no moderator vouches", gate, 0, false},
		{"ineligible user is never promoted", promotionGate{Eligible: false}, 99, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			promote, reason := evaluatePromotion(qualified, tt.gate, tt.modVouches)
			if promote != tt.wantPromote {
				t.Fatalf("promote = %v, want %v", promote, tt.wantPromote)
			}
			if promote && !strings.Contains(reason, "auto-promotion") {
				t.Errorf("reason = %q, want it to describe the auto-promotion", reason)
			}
			if !promote && reason != "" {
				t.Errorf("reason = %q, want empty when not promoting", reason)
			}
		})
	}
}
