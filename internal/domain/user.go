package domain

import "time"

type Role string

const (
	RolePending   Role = "pending"
	RoleMember    Role = "member"
	RoleModerator Role = "moderator"
	RoleCouncil   Role = "council"
	RoleBanned    Role = "banned"
)

const (
	PostingThreshold  = 30.0
	VouchingThreshold = 60.0

	PromotionTrustThreshold = 85.0
	PromotionMinDays        = 90
	PromotionMinModVouches  = 2
	DemotionConsecutiveDays = 30
)

// Demotion thresholds, per role.
//
// There used to be one flat DemotionTrustThreshold of 70.0 applied to every
// role. That was survivable only while trust scores were never recalculated —
// users are created at 50.0 and, before the periodic sweep and the role
// checker's own refresh were fixed, most stayed there or at some stale value.
// Now that scores converge on what CalcCompositeTrust actually computes, a flat
// 70 would demote most healthy members within DemotionConsecutiveDays.
//
// The numbers below are derived from three profiles run through the real
// calculator. Composite weighting is tenure 15%, activity 20%, voucher 35%,
// moderation 30%; activity is (posts/90) and (reactions/270) each worth half,
// over a 90-day window; voucher is min(100, 15*vouches) scaled by the vouchers'
// average trust / 100; moderation is 100 minus undecayed penalty points.
//
//	A. quiet but fine — joined 200d ago, 20 posts, 60 reactions received,
//	   2 vouches from neighbours averaging 70 trust, no penalties:
//	     tenure     200/365*100 = 54.79 -> 8.22
//	     activity   11.11 + 11.11 = 22.22 -> 4.44
//	     voucher    min(100, 30) * 0.70 = 21.00 -> 7.35
//	     moderation 100 -> 30.00
//	                                    total 50.01
//
//	B. engaged — 1 year, 45 posts, 135 reactions, 3 vouches averaging 80:
//	     15.00 + 10.00 + (45*0.80=36 -> 12.60) + 30.00 = 67.60
//
//	C. very strong — 1 year, activity at both caps, 4 vouches averaging 85:
//	     15.00 + 20.00 + (min(100,60)*0.85=51 -> 17.85) + 30.00 = 82.85
//
// Profile A is the floor of "fine", not the middle: a member who reads more
// than they post, has been vouched for twice, and has never been sanctioned.
// Whatever the member threshold is, it has to sit clearly below 50.01.
//
// All three profiles are established accounts, and that is a premise of the
// numbers rather than an accident of the examples: tenure and activity are
// functions of time, so a member who joined last month scores below every one
// of them on components that have nothing to do with conduct. The member bar
// is therefore not applied until DemotionMinimumTenureDays have passed — see
// the constant below for that derivation.
const (
	// MemberDemotionTrustThreshold is the score a member must stay at or above
	// to keep posting rights (below it, sustained, they drop back to pending).
	//
	// 35.0 leaves profile A a 15-point margin, which is what the moderation
	// component is worth in practice. Working backwards from A: with the other
	// three components contributing 20.01, the moderation component has to fall
	// below (35 - 20.01) / 0.30 = 49.97 to cross, i.e. more than ~50 points of
	// live penalty. That means:
	//
	//   - A fresh suspend (severity 4, 40 direct points, 365-day decay) on top
	//     of profile A gives moderation 60 -> 18.00 and a total of 38.01. It
	//     stings — 12 composite points — and stays above the line: serving a
	//     suspension should not also cost a resident their account standing.
	//   - Ban propagation alone does not cross it either. A ban is severity 5,
	//     100 points, decaying 0.75 per hop: three hops out is 42.19 permanent
	//     points, giving moderation 57.81 -> 17.34 and a total of 37.35.
	//   - Collapsed trust does cross it. That same three-hop ban penalty plus
	//     the member's own fresh mute (severity 3, 25 points) totals 67.19,
	//     giving moderation 32.81 -> 9.84 and a total of 29.86.
	//
	// So the member threshold fires on genuinely collapsed trust — someone
	// carrying both a sanction of their own and the fallout of vouching into a
	// banned corner of the graph — and not on a quiet resident or a single
	// served penalty.
	MemberDemotionTrustThreshold = 35.0

	// ModeratorDemotionTrustThreshold is the score a moderator must stay at or
	// above to keep the role. This is the old flat value, kept for moderators
	// and only for them.
	//
	// The promotion bar is PromotionTrustThreshold (85), so 70 is a 15-point
	// buffer below the standing the role was granted for — wide enough that a
	// quiet quarter does not unseat a moderator, narrow enough that the role
	// still means clearly-above-average standing: profile B, an engaged member
	// in good standing, scores 67.60, and profile C, a very strong one, scores
	// 82.85. A moderator below 70 has fallen under the engaged-member norm.
	//
	// Splitting the two thresholds also stops a cascade the flat value created.
	// Under a single 70, a moderator demoted at 65 landed at member — still
	// below 70 — so the next run restarted the clock and DemotionConsecutiveDays
	// later dropped them to pending, losing posting rights entirely for a score
	// that was only ever a statement about moderator standing. At 35 for members
	// the demotion stops where it was meant to.
	ModeratorDemotionTrustThreshold = 70.0

	// DemotionMinimumTenureDays is how old a member's account must be before
	// the member bar is applied to it at all.
	//
	// The bar assumes a score that has had time to mean something, and a new
	// member's has not. Two of the four components are functions of time rather
	// than of conduct: tenure is days/365, and activity is counted over a
	// 90-day window, so a member who joined recently scores low on both no
	// matter how well they behave. Run the review's case through the real
	// calculator — 30 days in, quiet, one vouch from a 60-trust neighbour, never
	// sanctioned:
	//
	//	tenure     30/365*100 = 8.22  -> 1.23
	//	activity   0                  -> 0.00
	//	voucher    15 * 0.60 = 9.00   -> 3.15
	//	moderation 100                -> 30.00
	//	                                34.38
	//
	// That is below 35 on the day they are vouched in, with nothing held
	// against them. Thirty consecutive days later the sweep would demote them
	// back to pending for the crime of being new and quiet — and the demotion
	// clock could start on day one, so the earliest demotion lands around day
	// 31, before the account is old enough to have produced a meaningful score.
	//
	// 90 days is where the structural depression lifts. Tenure alone is then
	// 90/365*100 = 24.66 -> 3.70, and a single vouch from a 55-trust neighbour
	// adds 15 * 0.55 = 8.25 -> 2.89, so a clean member sits at 36.59 with no
	// posts at all — above the bar on the components they cannot hurry. It is
	// also the window activity is measured over, so by then a member has had a
	// full one, and it matches PromotionMinDays: 90 days is already the point
	// at which this codebase considers a member's record long enough to judge.
	//
	// Below it the score is not evidence, so no demotion decision is made
	// either way; see evaluateDemotion. Moderators are deliberately not given
	// the grace — reaching the role takes PromotionMinDays of membership, so
	// every moderator is already past it.
	DemotionMinimumTenureDays = 90
)

// DemotionTrustThresholdFor returns the trust score a user of this role must
// stay at or above, and whether the role is subject to automatic demotion at
// all.
//
// The roles it reports false for are exactly the ones nextRoleAfterDemotion
// declines to demote. Pending users have no role left to lose, banned users
// have nowhere further to fall, and council standing is decided by the
// community rather than by a score — CalcCompositeTrust pins council trust to
// 100 for the same reason, so there is no number here that could be crossed.
func DemotionTrustThresholdFor(role Role) (float64, bool) {
	switch role {
	case RoleMember:
		return MemberDemotionTrustThreshold, true
	case RoleModerator:
		return ModeratorDemotionTrustThreshold, true
	default:
		return 0, false
	}
}

type User struct {
	ID               string     `json:"id"`
	KratosIdentityID string     `json:"kratos_identity_id,omitempty"`
	DisplayName      string     `json:"display_name"`
	Bio              string     `json:"bio"`
	AvatarURL        string     `json:"avatar_url"`
	TrustScore       float64    `json:"trust_score"`
	Role             Role       `json:"role"`
	IsActive         bool       `json:"is_active"`
	JoinedAt         time.Time  `json:"joined_at"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	TrustBelowSince  *time.Time `json:"trust_below_since,omitempty"`

	// MutedUntil is when an active mute expires; nil means not muted.
	//
	// It carries no JSON tag on purpose. A mute is between the user and the
	// moderators, and this struct is serialized wholesale by the pending-users
	// list, so a public tag here would publish it to every caller of that
	// endpoint. The response types opt in instead: see handler's own /users/me
	// shape.
	MutedUntil *time.Time `json:"-"`

	// SuspendedUntil is when a suspension expires; nil means not suspended.
	//
	// Untagged for the same reason as MutedUntil. The suspension itself is not
	// a secret — is_active already tells the caller they cannot act — but the
	// date belongs to the moderators' view, not to every reader of a list.
	SuspendedUntil *time.Time `json:"-"`

	// ResidencyClaim is what the resident says about where they live, in their
	// own words. It is an attestation the council reads while deciding whether
	// to approve them, never a verified fact — nothing checks it against
	// anything, and how hard to press on it is each town's own business.
	//
	// Untagged, and this is the strongest case of the three. A resident's home
	// address is the most sensitive thing this struct carries, the struct is
	// serialized wholesale by the council's approval queue, and the claim is
	// for the reviewing council alone: not the directory, not a public profile,
	// not the resident's own neighbours. The queue's response type opts in by
	// naming the field; nothing else may.
	ResidencyClaim string `json:"-"`

	// ResidencyClaimUpdatedAt is when the claim was last written, including
	// when it was cleared; nil means the resident has never set one. Untagged
	// for the same reason as the claim itself.
	ResidencyClaimUpdatedAt *time.Time `json:"-"`
}

// IsMuted reports whether a moderator's mute is still in force at now.
//
// An expired mute needs no sweep to clear it: the comparison against now is the
// whole mechanism, so a mute simply stops applying once its time passes.
func (u *User) IsMuted(now time.Time) bool {
	return u.MutedUntil != nil && now.Before(*u.MutedUntil)
}

// IsSuspended reports whether a moderator's suspension is still in force at now.
//
// Identical in shape to IsMuted, and for the same reason: a suspension ends by
// its own timestamp passing, with nothing to run and nothing to remember. It
// used to be enforced by clearing is_active, which no query ever set back, so
// every timed suspension was in practice permanent.
//
// IsActive is where this is enforced across the codebase — the repository folds
// a suspension in force into it when it hydrates a user, so every existing gate
// (CanPost, CanVouch, CanModerate, the RequireActive middleware) refuses a
// suspended user and admits them again the moment the suspension lapses,
// without each of them growing a clock. This method is for the paths that need
// to name the suspension itself rather than its effect.
func (u *User) IsSuspended(now time.Time) bool {
	return u.SuspendedUntil != nil && now.Before(*u.SuspendedUntil)
}

// CanPost reports whether the user may publish a post at now.
//
// The clock is a parameter rather than a package-level time.Now so that the
// mute boundary is testable without a global, and so that a caller which
// already has a clock — every service does — uses the same one it uses
// everywhere else.
//
// A mute is checked here rather than alongside it because this is the single
// question every caller asks. Splitting the answer across CanPost plus a
// separate mute check would make correctness depend on each caller remembering
// both.
// A suspension is checked here as well as folded into IsActive by the
// repository, so a User assembled in memory — a test, or any future caller that
// does not come through a row — cannot post while serving one either.
func (u *User) CanPost(now time.Time) bool {
	return u.IsActive &&
		!u.IsMuted(now) &&
		!u.IsSuspended(now) &&
		u.TrustScore >= PostingThreshold &&
		u.Role != RolePending && u.Role != RoleBanned
}

func (u *User) CanVouch() bool {
	return u.IsActive && u.TrustScore >= VouchingThreshold && u.Role != RolePending && u.Role != RoleBanned
}

func (u *User) CanModerate() bool {
	return u.IsActive && (u.Role == RoleModerator || u.Role == RoleCouncil)
}

func (u *User) IsCouncil() bool {
	return u.IsActive && u.Role == RoleCouncil
}
