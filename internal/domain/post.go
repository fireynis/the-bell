package domain

import "time"

type PostStatus string

const (
	PostVisible         PostStatus = "visible"
	PostRemovedByAuthor PostStatus = "removed_by_author"
	PostRemovedByMod    PostStatus = "removed_by_mod"
)

type Post struct {
	ID                string     `json:"id"`
	AuthorID          string     `json:"author_id"`
	Body              string     `json:"body"`
	ImagePath         string     `json:"image_path,omitempty"`
	Status            PostStatus `json:"status"`
	RemovalReason     string     `json:"removal_reason,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	EditedAt          *time.Time `json:"edited_at,omitempty"`
	AuthorDisplayName string     `json:"author_display_name,omitempty"`
	AuthorAvatarURL   string     `json:"author_avatar_url,omitempty"`
	ReactionCounts    map[ReactionType]int `json:"reaction_counts,omitempty"`
	UserReactions     []ReactionType       `json:"user_reactions,omitempty"`
}

const MaxPostBodyLength = 1000
const EditWindowMinutes = 15

func (p *Post) CanEdit(userID string, now time.Time) bool {
	if p.AuthorID != userID {
		return false
	}
	if p.Status != PostVisible {
		return false
	}
	return now.Sub(p.CreatedAt).Minutes() <= EditWindowMinutes
}
