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

type User struct {
	ID               string
	KratosIdentityID string
	DisplayName      string
	Bio              string
	AvatarURL        string
	TrustScore       float64
	Role             Role
	IsActive         bool
	JoinedAt         time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

func (u *User) CanPost() bool {
	return u.IsActive && u.TrustScore >= 30.0 && u.Role != RolePending && u.Role != RoleBanned
}

func (u *User) CanVouch() bool {
	return u.IsActive && u.TrustScore >= 60.0 && u.Role != RolePending && u.Role != RoleBanned
}

func (u *User) CanModerate() bool {
	return u.IsActive && (u.Role == RoleModerator || u.Role == RoleCouncil)
}

func (u *User) IsCouncil() bool {
	return u.IsActive && u.Role == RoleCouncil
}
