package domain

import "time"

type RoleHistory struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	OldRole   Role      `json:"old_role"`
	NewRole   Role      `json:"new_role"`
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"created_at"`
}
