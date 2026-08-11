package service

import (
	"errors"
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
)

// policyFor resolves the propagation policy for a severity the domain is
// expected to know; a failure here is a broken test, not a broken planner.
func policyFor(t *testing.T, severity int) penaltyPolicy {
	t.Helper()
	policy, err := resolvePenaltyPolicy(severity)
	if err != nil {
		t.Fatalf("resolvePenaltyPolicy(%d): %v", severity, err)
	}
	return policy
}

func TestPlanDirectPenalty(t *testing.T) {
	for severity := 1; severity <= 5; severity++ {
		want, ok := domain.DirectPenalty(severity)
		if !ok {
			t.Fatalf("severity %d is not a known severity", severity)
		}

		got := planDirectPenalty("offender", policyFor(t, severity))
		if got.UserID != "offender" {
			t.Errorf("severity %d: UserID = %q, want %q", severity, got.UserID, "offender")
		}
		if got.Amount != want {
			t.Errorf("severity %d: Amount = %v, want %v", severity, got.Amount, want)
		}
		if got.Depth != 0 {
			t.Errorf("severity %d: Depth = %d, want 0 for the direct penalty", severity, got.Depth)
		}
	}
}

// A severity outside 1-5 used to index straight off the end of four maps and
// plan a penalty of zero points travelling zero hops, which persists as a real
// row that costs the offender nothing. Resolving the policy is the one place
// that can now refuse, and every planner is downstream of it.
func TestResolvePenaltyPolicy_RejectsUnknownSeverity(t *testing.T) {
	for _, severity := range []int{-1, 0, 6, 100} {
		policy, err := resolvePenaltyPolicy(severity)
		if !errors.Is(err, ErrValidation) {
			t.Errorf("resolvePenaltyPolicy(%d) error = %v, want ErrValidation", severity, err)
		}
		if policy != (penaltyPolicy{}) {
			t.Errorf("resolvePenaltyPolicy(%d) = %+v, want the zero policy alongside the error",
				severity, policy)
		}
	}
}

// The policy has to carry all four parameters, because a planner that reached
// for a missing one would be back to reading a zero as an answer.
func TestResolvePenaltyPolicy_CarriesEveryParameter(t *testing.T) {
	for severity := 1; severity <= 5; severity++ {
		policy := policyFor(t, severity)

		if want, _ := domain.DirectPenalty(severity); policy.directPenalty != want {
			t.Errorf("severity %d: directPenalty = %v, want %v", severity, policy.directPenalty, want)
		}
		if want, _ := domain.PropagationDecay(severity); policy.decayRate != want {
			t.Errorf("severity %d: decayRate = %v, want %v", severity, policy.decayRate, want)
		}
		if want, _ := domain.PropagationDepth(severity); policy.maxDepth != want {
			t.Errorf("severity %d: maxDepth = %d, want %d", severity, policy.maxDepth, want)
		}
		if want, _ := domain.PenaltyDecayDays(severity); policy.decayDays != want {
			t.Errorf("severity %d: decayDays = %d, want %d", severity, policy.decayDays, want)
		}
	}
}

func TestPlanPropagatedPenalties_DecaysByDepth(t *testing.T) {
	// Severity 5: base 100, decay 0.75 per hop.
	vouchers := map[string]int{"a": 1, "b": 2, "c": 3}

	got := planPropagatedPenalties("offender", policyFor(t, 5), vouchers)
	if len(got) != 3 {
		t.Fatalf("got %d specs, want 3", len(got))
	}

	want := []PenaltySpec{
		{UserID: "a", Amount: 75, Depth: 1},
		{UserID: "b", Amount: 56.25, Depth: 2},
		{UserID: "c", Amount: 42.1875, Depth: 3},
	}
	for i, w := range want {
		if got[i].UserID != w.UserID || got[i].Depth != w.Depth {
			t.Errorf("spec %d = %+v, want user %q at depth %d", i, got[i], w.UserID, w.Depth)
		}
		if !approxEqual(got[i].Amount, w.Amount) {
			t.Errorf("spec %d amount = %v, want %v", i, got[i].Amount, w.Amount)
		}
	}
}

// Penalties must shrink as they travel outward, so someone three hops away is
// always hurt less than a direct voucher.
func TestPlanPropagatedPenalties_MonotonicallyDecreasing(t *testing.T) {
	for severity := 1; severity <= 5; severity++ {
		specs := planPropagatedPenalties("offender", policyFor(t, severity), map[string]int{"a": 1, "b": 2, "c": 3})
		for i := 1; i < len(specs); i++ {
			if specs[i].Amount >= specs[i-1].Amount {
				t.Errorf("severity %d: depth %d amount %v is not less than depth %d amount %v",
					severity, specs[i].Depth, specs[i].Amount, specs[i-1].Depth, specs[i-1].Amount)
			}
		}
	}
}

// Map iteration order is randomized in Go, so the planner must sort its output
// or the persisted order (and any assertion over it) would vary run to run.
func TestPlanPropagatedPenalties_DeterministicOrder(t *testing.T) {
	vouchers := map[string]int{"zeta": 1, "alpha": 1, "mid": 2, "beta": 1, "omega": 2}

	policy := policyFor(t, 3)

	first := planPropagatedPenalties("offender", policy, vouchers)
	for i := 0; i < 50; i++ {
		again := planPropagatedPenalties("offender", policy, vouchers)
		for j := range first {
			if again[j] != first[j] {
				t.Fatalf("iteration %d differs at index %d: %+v vs %+v", i, j, again[j], first[j])
			}
		}
	}

	// Sorted by depth, then user ID.
	wantOrder := []string{"alpha", "beta", "zeta", "mid", "omega"}
	for i, want := range wantOrder {
		if first[i].UserID != want {
			t.Errorf("index %d = %q, want %q (order: %v)", i, first[i].UserID, want, wantOrder)
		}
	}
}

// The offender already receives the direct penalty; a cycle in the vouch graph
// that returns them as their own voucher must not penalize them a second time.
func TestPlanPropagatedPenalties_SkipsTargetAndDepthZero(t *testing.T) {
	vouchers := map[string]int{
		"offender": 2, // cycle back to the offender
		"selfhop":  0, // depth 0 duplicates the direct penalty
		"real":     1,
	}

	got := planPropagatedPenalties("offender", policyFor(t, 4), vouchers)
	if len(got) != 1 {
		t.Fatalf("got %d specs (%+v), want only the genuine voucher", len(got), got)
	}
	if got[0].UserID != "real" {
		t.Errorf("UserID = %q, want %q", got[0].UserID, "real")
	}
}

func TestPlanPropagatedPenalties_NoVouchers(t *testing.T) {
	policy := policyFor(t, 3)

	if got := planPropagatedPenalties("offender", policy, nil); len(got) != 0 {
		t.Errorf("got %d specs, want none for a user with no vouchers", len(got))
	}
	if got := planPropagatedPenalties("offender", policy, map[string]int{}); len(got) != 0 {
		t.Errorf("got %d specs, want none for an empty voucher map", len(got))
	}
}

func TestPenaltyDecayTime(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

	for severity := 1; severity <= 5; severity++ {
		days, known := domain.PenaltyDecayDays(severity)
		if !known {
			t.Fatalf("severity %d is not a known severity", severity)
		}

		got := penaltyDecayTime(policyFor(t, severity), now)

		if days == 0 {
			if got != nil {
				t.Errorf("severity %d: got %v, want nil for a permanent penalty", severity, got)
			}
			continue
		}

		if got == nil {
			t.Errorf("severity %d: got nil, want a decay time %d days out", severity, days)
			continue
		}
		if want := now.AddDate(0, 0, days); !got.Equal(want) {
			t.Errorf("severity %d: got %v, want %v", severity, got, want)
		}
	}
}

// A ban is the one penalty that never decays; letting it expire would silently
// restore a banned user's trust score.
func TestPenaltyDecayTime_BanIsPermanent(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	got := penaltyDecayTime(policyFor(t, 5), now)
	if got != nil {
		t.Errorf("severity 5 (ban) decays at %v, want a permanent penalty (nil)", got)
	}
}
