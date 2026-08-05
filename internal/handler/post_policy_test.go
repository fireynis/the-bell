package handler

import (
	"testing"

	"github.com/fireynis/the-bell/internal/domain"
)

func postsWithIDs(ids ...string) []*domain.Post {
	posts := make([]*domain.Post, len(ids))
	for i, id := range ids {
		posts[i] = &domain.Post{ID: id}
	}
	return posts
}

func TestNextCursor(t *testing.T) {
	tests := []struct {
		name  string
		posts []*domain.Post
		limit int
		want  string
	}{
		{"a full page hands back its last post id", postsWithIDs("c", "b", "a"), 3, "a"},
		{"a short page is the last page", postsWithIDs("c", "b"), 3, ""},
		{"no posts is the last page", nil, 20, ""},
		{"empty slice is the last page", []*domain.Post{}, 20, ""},
		{"single post filling a limit of one", postsWithIDs("a"), 1, "a"},
		{"more posts than the limit still uses the last one", postsWithIDs("c", "b", "a"), 2, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := nextCursor(tt.posts, tt.limit); got != tt.want {
				t.Errorf("nextCursor() = %q, want %q", got, tt.want)
			}
		})
	}
}

// A page of exactly limit posts is the only signal the feed query gives that
// more posts may exist, so that boundary must always produce a cursor — losing
// it would silently truncate the feed at the first full page.
func TestNextCursor_FullPageBoundary(t *testing.T) {
	posts := postsWithIDs("post-3", "post-2", "post-1")

	if got := nextCursor(posts, len(posts)); got != "post-1" {
		t.Errorf("len(posts) == limit: cursor = %q, want the last post id %q", got, "post-1")
	}
	if got := nextCursor(posts, len(posts)+1); got != "" {
		t.Errorf("len(posts) < limit: cursor = %q, want empty", got)
	}
}

// parseLimit never yields zero, but the pure function must not index into an
// empty page if it ever did.
func TestNextCursor_ZeroLimitDoesNotPanic(t *testing.T) {
	if got := nextCursor(nil, 0); got != "" {
		t.Errorf("cursor = %q, want empty", got)
	}
}

// A removed post stays readable to the people with a reason to read it, and is
// invisible to everyone else. The caller turns false into a 404 rather than a
// 403 so that a refusal cannot be told apart from a post that never existed.
func TestCanViewPost(t *testing.T) {
	const authorID = "author-1"

	post := func(status domain.PostStatus) *domain.Post {
		return &domain.Post{ID: "post-1", AuthorID: authorID, Status: status}
	}
	viewer := func(id string, role domain.Role) *domain.User {
		return &domain.User{ID: id, Role: role, IsActive: true}
	}

	tests := []struct {
		name   string
		post   *domain.Post
		viewer *domain.User
		want   bool
	}{
		{"anonymous reader sees a visible post", post(domain.PostVisible), nil, true},
		{"any member sees a visible post", post(domain.PostVisible), viewer("other", domain.RoleMember), true},
		{"the author sees their own visible post", post(domain.PostVisible), viewer(authorID, domain.RoleMember), true},

		{"the author still sees their post after a moderator removes it", post(domain.PostRemovedByMod), viewer(authorID, domain.RoleMember), true},
		{"the author still sees a post they deleted themselves", post(domain.PostRemovedByAuthor), viewer(authorID, domain.RoleMember), true},
		{"a moderator sees a removed post", post(domain.PostRemovedByMod), viewer("mod-1", domain.RoleModerator), true},
		{"council sees a removed post", post(domain.PostRemovedByMod), viewer("council-1", domain.RoleCouncil), true},
		// The report queue's live case: reported, then deleted by the author.
		{"a moderator sees a post the author deleted", post(domain.PostRemovedByAuthor), viewer("mod-1", domain.RoleModerator), true},

		{"an unrelated member cannot see a removed post", post(domain.PostRemovedByMod), viewer("other", domain.RoleMember), false},
		{"an anonymous reader cannot see a removed post", post(domain.PostRemovedByMod), nil, false},
		{"an anonymous reader cannot see an author-deleted post", post(domain.PostRemovedByAuthor), nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := canViewPost(tt.post, tt.viewer); got != tt.want {
				t.Errorf("canViewPost() = %v, want %v", got, tt.want)
			}
		})
	}
}

// A suspended moderator is an ordinary reader. CanModerate already requires an
// active account; this pins that canViewPost does not route around it.
func TestCanViewPost_SuspendedModeratorLosesTheModeratorView(t *testing.T) {
	removed := &domain.Post{ID: "post-1", AuthorID: "author-1", Status: domain.PostRemovedByMod}
	suspended := &domain.User{ID: "mod-1", Role: domain.RoleModerator, IsActive: false}

	if canViewPost(removed, suspended) {
		t.Error("a suspended moderator was shown a removed post")
	}
}

// Anonymous callers reach this endpoint, and a missing post reaches this
// function on some paths; neither may panic.
func TestCanViewPost_NilsAreSafe(t *testing.T) {
	if canViewPost(nil, nil) {
		t.Error("a nil post was reported viewable")
	}
	if canViewPost(nil, &domain.User{ID: "u1", Role: domain.RoleCouncil, IsActive: true}) {
		t.Error("a nil post was reported viewable to council")
	}
	if !canViewPost(&domain.Post{Status: domain.PostVisible}, nil) {
		t.Error("a visible post was hidden from an anonymous reader")
	}
}
