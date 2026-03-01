package domain

import "time"

type TrustPenalty struct {
	ID                 string
	UserID             string
	ModerationActionID string
	PenaltyAmount      float64
	HopDepth           int
	CreatedAt          time.Time
	DecaysAt           *time.Time
}
