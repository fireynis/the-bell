package service

import (
	"context"
	"fmt"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
)

// The member-visible moderation history: what a person may learn about the
// moderation taken against them.
//
// Until now the answer was "almost nothing". A member could see muted_until
// while a mute ran, and the mute or suspension lifts recorded on their own
// profile — but not that they had been warned, not why, not what it cost them,
// and not when that cost fades. Everything else sat behind /v1/moderation,
// which requires the moderator role.
//
// The policy this file implements is three lines, and the types below are how
// each one is enforced rather than merely intended:
//
//  1. A member may see the moderation actions taken AGAINST them: the action
//     type, its severity, the reason the moderator wrote, when it happened,
//     when the restriction it imposed ends, the trust penalty it cost them,
//     and when that penalty finishes decaying — or that it never does.
//
//  2. NO member-facing response names the moderator who acted. That principle
//     already governs the lift records on a member's profile, and it holds here
//     for the same reason: a resident who disagrees with a decision should take
//     it up with the moderation team, not with the neighbour who happened to be
//     on duty. Small towns are the whole premise of this platform, and naming
//     the individual turns a moderation decision into a personal grievance.
//
//  3. A member sees only the penalty they took THEMSELVES. Not the penalties
//     the same action propagated to the people who vouched for them, and not
//     the penalties other people's moderation propagated to the member. Both
//     would name, or let them derive, who else has been moderated. The
//     composite trust score already reflects those silently, which is where
//     that information belongs.

// OwnPenalty is what one moderation action cost the member it landed on — and
// nothing about what the same action cost anybody else.
type OwnPenalty struct {
	// Amount is the trust points the member themselves lost.
	Amount float64 `json:"amount"`
	// DecaysAt is when the penalty has faded away completely. Nil means it
	// never does, which is true of the ban's penalty and of nothing else.
	//
	// That reading is only unambiguous because the enclosing OwnPenalty is
	// present: "no penalty at all" is the absence of the whole struct, so a
	// permanent penalty and a missing one cannot be confused.
	DecaysAt *time.Time `json:"decays_at,omitempty"`
}

// OwnModerationEntry is one moderation action as its subject sees it.
//
// It is a separate type from ActionHistoryEntry rather than a filtered copy of
// one, and that is what enforces the policy above rather than describing it:
// there is no field here for a moderator id, so no later edit to the mapping
// can leak one, and no penalty belonging to somebody else has anywhere to land.
// A test can assert the absence, but the type is what makes the absence hold.
type OwnModerationEntry struct {
	ID string `json:"id"`
	// Action and Severity are the decision itself: "warn" at severity 1 is a
	// different fact from "warn" at severity 2, and the severity is what
	// decided the penalty below.
	Action   domain.ActionType `json:"action"`
	Severity int               `json:"severity"`
	// Reason is the moderator's written reason, exactly as they wrote it. It is
	// mandatory when an action is taken and it is the single most useful thing
	// on this record — a member told only that they were warned has been told
	// nothing they can act on.
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"created_at"`
	// ExpiresAt is when the restriction the action imposed ends. Nil for a warn
	// and for a ban, neither of which expires.
	//
	// It is the action's own expiry as recorded at the time, so a mute a
	// moderator later lifted early still reports the expiry it was given. The
	// lift itself reaches the member through mute_lifts and suspension_lifts on
	// their profile; nothing here is rewritten after the fact, because the
	// record exists to preserve what was actually decided.
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	// Penalty is the direct trust cost to this member. Nil when no direct
	// penalty was recorded against them for this action — which happens when
	// propagation failed after the action was written, a case TakeAction
	// deliberately tolerates rather than losing the action over.
	Penalty *OwnPenalty `json:"penalty,omitempty"`
}

// OwnHistory returns the moderation actions taken against userID, as userID is
// entitled to see them, newest first.
//
// It reuses GetActionHistory rather than reading the audit trail a second way,
// so the member's view and the moderator's view cannot come to disagree about
// what happened; the stripping is a mapping over the same rows. (That path is
// N+1 over the penalty reads, which is a known and separate problem — it is
// bounded by the page limit, as it is for the moderator listing.)
//
// The byModerator direction is not a parameter here and must not become one.
// "Actions I took" is a council audit view of a moderator, and the whole point
// of this method is that its caller is the target.
func (s *ModerationHistoryService) OwnHistory(
	ctx context.Context,
	userID string,
	limit, offset int,
) ([]OwnModerationEntry, error) {
	// Defensive rather than reachable: the handler takes the id from the
	// authenticated session, never from the request. An empty id would
	// otherwise read as a legitimate query returning nothing, which is the
	// wrong answer to give quietly.
	if userID == "" {
		return nil, fmt.Errorf("%w: user id must not be empty", ErrValidation)
	}

	entries, err := s.GetActionHistory(ctx, userID, false, limit, offset)
	if err != nil {
		return nil, err
	}

	own := make([]OwnModerationEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.Action == nil {
			continue
		}
		own = append(own, ownModerationEntry(entry, userID))
	}
	return own, nil
}

// ownModerationEntry strips one audit-trail entry down to what its subject may
// see. Every field is copied across by hand, which is deliberate: a struct
// literal that names each field is what makes an added field on
// ModerationAction an explicit decision rather than something that arrives on a
// member's screen because a repository query grew a column.
func ownModerationEntry(entry ActionHistoryEntry, userID string) OwnModerationEntry {
	return OwnModerationEntry{
		ID:        entry.Action.ID,
		Action:    entry.Action.Action,
		Severity:  entry.Action.Severity,
		Reason:    entry.Action.Reason,
		CreatedAt: entry.Action.CreatedAt,
		ExpiresAt: copyTime(entry.Action.ExpiresAt),
		Penalty:   ownPenalty(entry.Penalties, userID),
	}
}

// ownPenalty picks the member's own direct penalty out of everything one action
// caused, and returns nil when there is none.
//
// Both conditions are checked, though either alone would do today: depth 0 is
// the offender's own penalty by construction in planDirectPenalty, and only the
// offender is at depth 0. Requiring both means a future change to either — a
// propagated penalty that lands at depth 0, or a direct penalty recorded
// against somebody else — cannot quietly turn this into a window onto another
// member's moderation.
func ownPenalty(penalties []domain.TrustPenalty, userID string) *OwnPenalty {
	for _, p := range penalties {
		if p.UserID != userID || p.HopDepth != 0 {
			continue
		}
		return &OwnPenalty{Amount: p.PenaltyAmount, DecaysAt: copyTime(p.DecaysAt)}
	}
	return nil
}

// copyTime copies the value behind an optional timestamp rather than the
// pointer, so nothing a caller does to the result can reach back into the
// domain rows the repository handed us.
func copyTime(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	c := *t
	return &c
}

// OwnModerationHistory delegates to ModerationHistoryService, on the same terms
// as GetActionHistory above it: the moderation handler reaches the history
// service through *ModerationActionService, and this exists so the member-facing
// route does too rather than acquiring a second path into the same data.
func (s *ModerationActionService) OwnModerationHistory(
	ctx context.Context,
	userID string,
	limit, offset int,
) ([]OwnModerationEntry, error) {
	return s.history.OwnHistory(ctx, userID, limit, offset)
}
