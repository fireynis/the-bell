package domain

import "time"

type PostStatus string

const (
	PostVisible         PostStatus = "visible"
	PostRemovedByAuthor PostStatus = "removed_by_author"
	PostRemovedByMod    PostStatus = "removed_by_mod"
)

type Post struct {
	ID        string     `json:"id"`
	AuthorID  string     `json:"author_id"`
	Body      string     `json:"body"`
	ImagePath string     `json:"image_path,omitempty"`
	Status    PostStatus `json:"status"`

	// RemovalReason is a moderator's private note and is never serialized.
	//
	// It is populated by GetPostByID, which deliberately has no status filter
	// so that the author and moderators can still read a removed post. That
	// made it reachable through GET /api/v1/posts/{id}, which does no role
	// check — and while post ids are unguessable UUIDv7s, the id was public
	// while the post was live, so the people holding one are exactly those who
	// saw the post before it was taken down.
	//
	// `json:"-"` rather than blanking the field in the handler: a scrub is one
	// more thing every present and future response path has to remember, which
	// is the failure mode that let this through in the first place. Exposure is
	// opt-in — a caller that should see the note must copy it onto an explicit
	// response type of its own.
	RemovalReason string `json:"-"`

	// RemovedBy is the id of the moderator who took the post down, and is
	// likewise never serialized.
	//
	// It is moderation metadata of exactly the same kind as RemovalReason, so it
	// gets the same treatment rather than a second convention: it reaches every
	// caller RemovalReason reaches, and leaking it would tell anyone holding a
	// post id which moderator handled the case.
	//
	// Empty means nobody — an author deleting their own post, or any post that
	// predates the removed_by column. The database stores that as NULL, since
	// the column is a foreign key to users and cannot hold "".
	RemovedBy string `json:"-"`

	CreatedAt         time.Time            `json:"created_at"`
	EditedAt          *time.Time           `json:"edited_at,omitempty"`
	AuthorDisplayName string               `json:"author_display_name,omitempty"`
	AuthorAvatarURL   string               `json:"author_avatar_url,omitempty"`
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
