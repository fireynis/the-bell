package domain

import "time"

type PostStatus string

const (
	PostVisible         PostStatus = "visible"
	PostRemovedByAuthor PostStatus = "removed_by_author"
	PostRemovedByMod    PostStatus = "removed_by_mod"
)

type Post struct {
	ID            string
	AuthorID      string
	Body          string
	ImagePath     string
	Status        PostStatus
	RemovalReason string
	CreatedAt     time.Time
	EditedAt      *time.Time
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
