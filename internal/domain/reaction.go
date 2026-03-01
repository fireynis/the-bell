package domain

import "time"

type ReactionType string

const (
	ReactionBell      ReactionType = "bell"
	ReactionHeart     ReactionType = "heart"
	ReactionCelebrate ReactionType = "celebrate"
)

type Reaction struct {
	ID        string
	UserID    string
	PostID    string
	Type      ReactionType
	CreatedAt time.Time
}
