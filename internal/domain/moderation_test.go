package domain

import "testing"

// The four propagation parameters used to be exported map variables. A lookup
// for a severity outside 1-5 answered with the zero value, which reads as a
// legitimate answer at every call site: zero penalty points, a zero decay rate,
// a zero-hop traversal, and — worst — a zero decay window, which
// CalcModerationScore interprets as "permanent". Each function now reports
// whether it recognized the severity so the caller has to decide.
func TestPropagationParameters_KnownSeverities(t *testing.T) {
	tests := []struct {
		severity  int
		depth     int
		decay     float64
		penalty   float64
		decayDays int
	}{
		{severity: 1, depth: 1, decay: 0.50, penalty: 5, decayDays: 90},
		{severity: 2, depth: 1, decay: 0.70, penalty: 10, decayDays: 180},
		{severity: 3, depth: 2, decay: 0.60, penalty: 25, decayDays: 270},
		{severity: 4, depth: 2, decay: 0.70, penalty: 40, decayDays: 365},
		{severity: 5, depth: 3, decay: 0.75, penalty: 100, decayDays: 0},
	}

	for _, tt := range tests {
		depth, ok := PropagationDepth(tt.severity)
		if !ok || depth != tt.depth {
			t.Errorf("PropagationDepth(%d) = %d, %v; want %d, true", tt.severity, depth, ok, tt.depth)
		}

		decay, ok := PropagationDecay(tt.severity)
		if !ok || decay != tt.decay {
			t.Errorf("PropagationDecay(%d) = %v, %v; want %v, true", tt.severity, decay, ok, tt.decay)
		}

		penalty, ok := DirectPenalty(tt.severity)
		if !ok || penalty != tt.penalty {
			t.Errorf("DirectPenalty(%d) = %v, %v; want %v, true", tt.severity, penalty, ok, tt.penalty)
		}

		days, ok := PenaltyDecayDays(tt.severity)
		if !ok || days != tt.decayDays {
			t.Errorf("PenaltyDecayDays(%d) = %d, %v; want %d, true", tt.severity, days, ok, tt.decayDays)
		}
	}
}

// An unrecognized severity must be reported rather than answered. Severity is
// validated at the service boundary, so these values should never arrive — but
// the whole point of the ok signal is that a caller which skips the validation
// gets an error instead of a silently harmless-looking penalty of zero points.
func TestPropagationParameters_UnknownSeverityIsReported(t *testing.T) {
	for _, severity := range []int{-1, 0, 6, 100} {
		if _, ok := PropagationDepth(severity); ok {
			t.Errorf("PropagationDepth(%d) reported a known severity", severity)
		}
		if _, ok := PropagationDecay(severity); ok {
			t.Errorf("PropagationDecay(%d) reported a known severity", severity)
		}
		if _, ok := DirectPenalty(severity); ok {
			t.Errorf("DirectPenalty(%d) reported a known severity", severity)
		}
		if _, ok := PenaltyDecayDays(severity); ok {
			t.Errorf("PenaltyDecayDays(%d) reported a known severity", severity)
		}
	}
}

// Severity 5 is the one severity whose decay window is legitimately 0, meaning
// permanent. It is indistinguishable from the old map's miss, which is why the
// ok signal has to carry that distinction instead of the value.
func TestPenaltyDecayDays_PermanentIsNotTheSameAsUnknown(t *testing.T) {
	days, ok := PenaltyDecayDays(5)
	if !ok {
		t.Fatal("PenaltyDecayDays(5) reported an unknown severity; a ban is known and permanent")
	}
	if days != 0 {
		t.Errorf("PenaltyDecayDays(5) = %d, want 0 (permanent)", days)
	}

	if _, ok := PenaltyDecayDays(6); ok {
		t.Error("PenaltyDecayDays(6) reported a known severity, so 0 would read as permanent")
	}
}

// Penalties must grow with severity, and a penalty must never travel further
// than the graph traversal that finds the people it lands on.
func TestPropagationParameters_ScaleWithSeverity(t *testing.T) {
	var prevPenalty float64
	var prevDepth int
	for severity := 1; severity <= 5; severity++ {
		penalty, _ := DirectPenalty(severity)
		if penalty <= prevPenalty {
			t.Errorf("DirectPenalty(%d) = %v, not greater than severity %d's %v",
				severity, penalty, severity-1, prevPenalty)
		}
		prevPenalty = penalty

		depth, _ := PropagationDepth(severity)
		if depth < prevDepth {
			t.Errorf("PropagationDepth(%d) = %d, less than severity %d's %d",
				severity, depth, severity-1, prevDepth)
		}
		if depth < 1 {
			t.Errorf("PropagationDepth(%d) = %d, want at least one hop", severity, depth)
		}
		prevDepth = depth

		decay, _ := PropagationDecay(severity)
		if decay <= 0 || decay >= 1 {
			t.Errorf("PropagationDecay(%d) = %v, want a fraction that shrinks a penalty per hop",
				severity, decay)
		}
	}
}
