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
	DemotionTrustThreshold  = 70.0
	DemotionConsecutiveDays = 30
)

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
}

// IsMuted reports whether a moderator's mute is still in force at now.
//
// An expired mute needs no sweep to clear it: the comparison against now is the
// whole mechanism, so a mute simply stops applying once its time passes.
func (u *User) IsMuted(now time.Time) bool {
	return u.MutedUntil != nil && now.Before(*u.MutedUntil)
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
func (u *User) CanPost(now time.Time) bool {
	return u.IsActive &&
		!u.IsMuted(now) &&
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
