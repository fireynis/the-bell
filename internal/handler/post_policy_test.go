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
