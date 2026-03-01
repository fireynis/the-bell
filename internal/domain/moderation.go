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
	ID           string
	TargetUserID string
	ModeratorID  string
	Action       ActionType
	Severity     int // 1-5
	Reason       string
	Duration     *time.Duration
	CreatedAt    time.Time
	ExpiresAt    *time.Time
}

type Report struct {
	ID         string    `json:"id"`
	ReporterID string    `json:"reporter_id"`
	PostID     string    `json:"post_id"`
	Reason     string    `json:"reason"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
}

// Trust propagation constants
var PropagationDepth = map[int]int{
	1: 1, // minor: 1 hop
	2: 1, // moderate: 1 hop
	3: 2, // serious: 2 hops
	4: 2, // severe: 2 hops
	5: 3, // ban: 3 hops
}

var PropagationDecay = map[int]float64{
	1: 0.50, // minor
	2: 0.70, // moderate
	3: 0.60, // serious
	4: 0.70, // severe
	5: 0.75, // ban
}

var DirectPenalty = map[int]float64{
	1: 5,   // minor warn
	2: 10,  // moderate warn
	3: 25,  // mute
	4: 40,  // suspend
	5: 100, // ban
}

var PenaltyDecayDays = map[int]int{
	1: 90,
	2: 180,
	3: 270,
	4: 365,
	5: 0, // permanent
}
