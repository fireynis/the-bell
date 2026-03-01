package domain

import "time"

type TrustPenalty struct {
	ID                 string     `json:"id"`
	UserID             string     `json:"user_id"`
	ModerationActionID string     `json:"moderation_action_id"`
	PenaltyAmount      float64    `json:"penalty_amount"`
	HopDepth           int        `json:"hop_depth"`
	CreatedAt          time.Time  `json:"created_at"`
	DecaysAt           *time.Time `json:"decays_at,omitempty"`
}
