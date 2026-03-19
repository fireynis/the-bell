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
}

func (u *User) CanPost() bool {
	return u.IsActive && u.TrustScore >= PostingThreshold && u.Role != RolePending && u.Role != RoleBanned
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
