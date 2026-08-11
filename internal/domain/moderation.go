package domain

import "time"

type ActionType string

const (
	ActionWarn    ActionType = "warn"
	ActionMute    ActionType = "mute"
	ActionSuspend ActionType = "suspend"
	ActionBan     ActionType = "ban"
)

type ModerationAction struct {
	ID           string         `json:"id"`
	TargetUserID string         `json:"target_user_id"`
	ModeratorID  string         `json:"moderator_id"`
	Action       ActionType     `json:"action"`
	Severity     int            `json:"severity"`
	Reason       string         `json:"reason"`
	Duration     *time.Duration `json:"duration,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
	ExpiresAt    *time.Time     `json:"expires_at,omitempty"`
}

// ReliefType names a non-punitive moderator act — one that lifts a restriction
// rather than imposing one.
type ReliefType string

const (
	// ReliefMuteLift is a moderator ending a mute before its duration runs out.
	ReliefMuteLift ReliefType = "mute_lift"
)

// ModerationRelief records a moderator undoing a restriction.
//
// It is deliberately not a ModerationAction. Every ModerationAction carries a
// severity between 1 and 5, and each of those names a trust penalty that
// propagates through the vouch graph, so there is no severity meaning "no
// punishment" — filing a release as one would record mercy as a sanction. This
// type has no severity at all, which is what makes it safe to show a member.
type ModerationRelief struct {
	ID           string     `json:"id"`
	TargetUserID string     `json:"target_user_id"`
	ModeratorID  string     `json:"moderator_id"`
	Type         ReliefType `json:"type"`
	// PreviousExpiresAt is the muted_until the lift destroyed. Nil when the
	// target had none, which is not an error: the lift endpoint is idempotent.
	PreviousExpiresAt *time.Time `json:"previous_expires_at,omitempty"`
	// WasInForce distinguishes ending a live restriction from a lift against
	// someone who was not under one. Only the former is worth telling the
	// member about, so the member-facing view filters on it.
	WasInForce bool      `json:"was_in_force"`
	CreatedAt  time.Time `json:"created_at"`
}

type Report struct {
	ID         string    `json:"id"`
	ReporterID string    `json:"reporter_id"`
	PostID     string    `json:"post_id"`
	Reason     string    `json:"reason"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
}

// Trust propagation parameters, keyed by a moderation action's severity (1-5).
//
// These were exported package-level maps, which any package could reassign and
// which answered a lookup for an unrecognized severity with the zero value.
// That zero is indistinguishable from a real answer at every call site — a
// penalty of no points, a traversal of no hops, a decay rate that erases the
// penalty after one hop, and a decay window of 0, which CalcModerationScore
// reads as *permanent*, the strongest outcome rather than the weakest. Each is
// now a function over an explicit switch that reports whether it recognized the
// severity, so an unknown one has to be handled instead of quietly applied.

// PropagationDepth returns how many hops through the vouch graph a penalty of
// this severity travels, and whether the severity is recognized.
func PropagationDepth(severity int) (int, bool) {
	switch severity {
	case 1, 2: // minor, moderate
		return 1, true
	case 3, 4: // serious, severe
		return 2, true
	case 5: // ban
		return 3, true
	default:
		return 0, false
	}
}

// PropagationDecay returns the fraction of a penalty that survives each hop
// outward from the offender, and whether the severity is recognized.
func PropagationDecay(severity int) (float64, bool) {
	switch severity {
	case 1: // minor
		return 0.50, true
	case 2: // moderate
		return 0.70, true
	case 3: // serious
		return 0.60, true
	case 4: // severe
		return 0.70, true
	case 5: // ban
		return 0.75, true
	default:
		return 0, false
	}
}

// DirectPenalty returns the trust points an action of this severity costs the
// offender themselves, and whether the severity is recognized.
func DirectPenalty(severity int) (float64, bool) {
	switch severity {
	case 1: // minor warn
		return 5, true
	case 2: // moderate warn
		return 10, true
	case 3: // mute
		return 25, true
	case 4: // suspend
		return 40, true
	case 5: // ban
		return 100, true
	default:
		return 0, false
	}
}

// PenaltyDecayDays returns how many days a penalty of this severity takes to
// decay away, and whether the severity is recognized. A recognized severity may
// legitimately return 0, which means the penalty is permanent — only the second
// return value distinguishes that from an unknown severity.
func PenaltyDecayDays(severity int) (int, bool) {
	switch severity {
	case 1:
		return 90, true
	case 2:
		return 180, true
	case 3:
		return 270, true
	case 4:
		return 365, true
	case 5:
		return 0, true // a ban never decays
	default:
		return 0, false
	}
}
