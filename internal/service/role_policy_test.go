package service

import (
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

func TestEvaluateDemotion(t *testing.T) {
	// DemotionTrustThreshold is 70, DemotionConsecutiveDays is 30.
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
			name:        "exactly at threshold counts as healthy",
			user:        &domain.User{Role: domain.RoleMember, TrustScore: domain.DemotionTrustThreshold},
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
			name:        "first drop below threshold starts the timer",
			user:        &domain.User{Role: domain.RoleMember, TrustScore: 69.9},
			wantOutcome: demotionMark,
		},
		{
			name: "below threshold but not long enough",
			user: &domain.User{
				Role: domain.RoleMember, TrustScore: 50,
				TrustBelowSince: ptrTime(daysAgo(29)),
			},
			wantOutcome: demotionWait,
		},
		{
			name: "member demoted to pending after the full window",
			user: &domain.User{
				Role: domain.RoleMember, TrustScore: 50,
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
			wantOutcome: demotionWait,
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

// A user whose trust recovers must not carry the old timer forward, otherwise a
// later dip would demote them immediately instead of restarting the 30-day clock.
func TestEvaluateDemotion_RecoveryResetsTheClock(t *testing.T) {
	longAgo := ptrTime(daysAgo(300))

	recovered := &domain.User{Role: domain.RoleMember, TrustScore: 90, TrustBelowSince: longAgo}
	if got := evaluateDemotion(recovered, policyNow); got.Outcome != demotionClear {
		t.Fatalf("recovered user: Outcome = %v, want demotionClear", got.Outcome)
	}

	// After the clear, TrustBelowSince is nil; a fresh dip only marks.
	dippedAgain := &domain.User{Role: domain.RoleMember, TrustScore: 40}
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
