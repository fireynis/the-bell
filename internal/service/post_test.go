package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
)

// mockPostRepo is an in-memory PostRepository for testing.
type mockPostRepo struct {
	posts map[string]*domain.Post
}

func newMockPostRepo() *mockPostRepo {
	return &mockPostRepo{posts: make(map[string]*domain.Post)}
}

func (m *mockPostRepo) CreatePost(_ context.Context, post *domain.Post) error {
	m.posts[post.ID] = post
	return nil
}

func (m *mockPostRepo) GetPostByID(_ context.Context, id string) (*domain.Post, error) {
	p, ok := m.posts[id]
	if !ok {
		return nil, ErrNotFound
	}
	return p, nil
}

func (m *mockPostRepo) ListPosts(_ context.Context, cursor string, limit int) ([]*domain.Post, error) {
	result := []*domain.Post{}
	for _, p := range m.posts {
		if p.Status != domain.PostVisible {
			continue
		}
		if cursor != "" && p.ID >= cursor {
			continue
		}
		result = append(result, p)
	}
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (m *mockPostRepo) ListPostsByAuthor(_ context.Context, authorID string, limit int) ([]*domain.Post, error) {
	var result []*domain.Post
	for _, p := range m.posts {
		if p.AuthorID == authorID {
			result = append(result, p)
		}
	}
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (m *mockPostRepo) UpdatePostContent(_ context.Context, id, body, altText string) (*domain.Post, error) {
	p, ok := m.posts[id]
	if !ok {
		return nil, ErrNotFound
	}
	p.Body = body
	p.AltText = altText
	now := time.Now()
	p.EditedAt = &now
	return p, nil
}

func (m *mockPostRepo) UpdatePostStatus(_ context.Context, id string, status domain.PostStatus, reason, removedBy string) error {
	p, ok := m.posts[id]
	if !ok {
		return ErrNotFound
	}
	p.Status = status
	p.RemovalReason = reason
	p.RemovedBy = removedBy
	return nil
}

func TestPostService_Create(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }

	tests := []struct {
		name      string
		authorID  string
		body      string
		imagePath string
		altText   string
		wantAlt   string
		wantErr   error
	}{
		{
			name:      "valid post",
			authorID:  "user-1",
			body:      "Hello, world!",
			imagePath: "/images/photo.jpg",
		},
		{
			name:      "image described",
			authorID:  "user-1",
			body:      "Look at this",
			imagePath: "/images/photo.jpg",
			altText:   "A heron on the frozen millpond",
			wantAlt:   "A heron on the frozen millpond",
		},
		{
			name:      "description is trimmed before storage",
			authorID:  "user-1",
			body:      "Look at this",
			imagePath: "/images/photo.jpg",
			altText:   "  A heron on the frozen millpond \n",
			wantAlt:   "A heron on the frozen millpond",
		},
		{
			name:      "whitespace-only description stores nothing",
			authorID:  "user-1",
			body:      "Look at this",
			imagePath: "/images/photo.jpg",
			altText:   "   \t\n ",
			wantAlt:   "",
		},
		{
			name:      "description at the rune limit",
			authorID:  "user-1",
			body:      "Look at this",
			imagePath: "/images/photo.jpg",
			altText:   strings.Repeat("é", domain.MaxAltTextRunes),
			wantAlt:   strings.Repeat("é", domain.MaxAltTextRunes),
		},
		{
			// Twice the byte budget, half the rune budget: a bytes-based bound
			// would reject this, and rejecting it would give a description
			// written in one alphabet less room than the same sentence in
			// another.
			name:      "multi-byte description inside the rune limit",
			authorID:  "user-1",
			body:      "Look at this",
			imagePath: "/images/photo.jpg",
			altText:   strings.Repeat("é", domain.MaxAltTextRunes/2),
			wantAlt:   strings.Repeat("é", domain.MaxAltTextRunes/2),
		},
		{
			name:      "description exceeds the rune limit",
			authorID:  "user-1",
			body:      "Look at this",
			imagePath: "/images/photo.jpg",
			altText:   strings.Repeat("a", domain.MaxAltTextRunes+1),
			wantErr:   ErrValidation,
		},
		{
			name:      "description without an image",
			authorID:  "user-1",
			body:      "Just text",
			imagePath: "",
			altText:   "A heron on the frozen millpond",
			wantErr:   ErrValidation,
		},
		{
			// The imageless case has to tolerate an empty field, or a client
			// that always sends alt_text cannot make a text-only post.
			name:      "empty description without an image is accepted",
			authorID:  "user-1",
			body:      "Just text",
			imagePath: "",
			altText:   "  ",
			wantAlt:   "",
		},
		{
			name:      "valid post without image",
			authorID:  "user-1",
			body:      "Just text",
			imagePath: "",
		},
		{
			name:      "body at max length",
			authorID:  "user-1",
			body:      strings.Repeat("a", domain.MaxPostBodyLength),
			imagePath: "",
		},
		{
			name:     "empty body",
			authorID: "user-1",
			body:     "",
			wantErr:  ErrValidation,
		},
		{
			name:     "whitespace-only body",
			authorID: "user-1",
			body:     "   \t\n  ",
			wantErr:  ErrValidation,
		},
		{
			name:     "body exceeds max length",
			authorID: "user-1",
			body:     strings.Repeat("a", domain.MaxPostBodyLength+1),
			wantErr:  ErrValidation,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newMockPostRepo()
			svc := NewPostService(repo, clock)

			post, err := svc.Create(context.Background(), postingUser(tt.authorID), tt.body, tt.imagePath, tt.altText)

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Create() error = %v, wantErr %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Create() unexpected error: %v", err)
			}
			if post.ID == "" {
				t.Error("Create() returned empty ID")
			}
			if post.AuthorID != tt.authorID {
				t.Errorf("AuthorID = %q, want %q", post.AuthorID, tt.authorID)
			}
			if post.Body != tt.body {
				t.Errorf("Body = %q, want %q", post.Body, tt.body)
			}
			if post.ImagePath != tt.imagePath {
				t.Errorf("ImagePath = %q, want %q", post.ImagePath, tt.imagePath)
			}
			if post.AltText != tt.wantAlt {
				t.Errorf("AltText = %q, want %q", post.AltText, tt.wantAlt)
			}
			if post.Status != domain.PostVisible {
				t.Errorf("Status = %q, want %q", post.Status, domain.PostVisible)
			}
			if !post.CreatedAt.Equal(now) {
				t.Errorf("CreatedAt = %v, want %v", post.CreatedAt, now)
			}
			// Verify post was stored in repo
			if _, ok := repo.posts[post.ID]; !ok {
				t.Error("post not stored in repository")
			}
		})
	}
}

func TestPostService_GetByID(t *testing.T) {
	repo := newMockPostRepo()
	svc := NewPostService(repo, nil)

	existing := &domain.Post{
		ID:       "post-1",
		AuthorID: "user-1",
		Body:     "test post",
		Status:   domain.PostVisible,
	}
	repo.posts["post-1"] = existing

	tests := []struct {
		name    string
		id      string
		wantErr error
	}{
		{"existing post", "post-1", nil},
		{"not found", "post-999", ErrNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			post, err := svc.GetByID(context.Background(), tt.id)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("GetByID() error = %v, wantErr %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("GetByID() unexpected error: %v", err)
			}
			if post.ID != tt.id {
				t.Errorf("ID = %q, want %q", post.ID, tt.id)
			}
		})
	}
}

// stubFeedCache records the feed request it was asked to serve.
type stubFeedCache struct {
	posts  []*domain.Post
	err    error
	calls  int
	cursor string
	limit  int
}

func (s *stubFeedCache) GetFeed(_ context.Context, cursor string, limit int) ([]*domain.Post, error) {
	s.calls++
	s.cursor, s.limit = cursor, limit
	return s.posts, s.err
}

func (s *stubFeedCache) InvalidateOnCreate(context.Context, *domain.Post) {}
func (s *stubFeedCache) InvalidateOnUpdate(context.Context, *domain.Post) {}
func (s *stubFeedCache) InvalidateOnDelete(context.Context, string)       {}

// ListFeed only decides where the feed comes from. Which posts are visible is
// decided by the SQL in the repository, so it is verified in the repository's
// own tests against real Postgres rather than through an in-memory stand-in
// that would only be re-testing itself.
func TestPostService_ListFeed_PrefersTheCacheWhenSet(t *testing.T) {
	repo := newMockPostRepo()
	repo.posts["from-repo"] = &domain.Post{ID: "from-repo", Status: domain.PostVisible}

	cached := []*domain.Post{{ID: "from-cache", Status: domain.PostVisible}}
	cache := &stubFeedCache{posts: cached}

	svc := NewPostService(repo, nil)
	svc.SetFeedCache(cache)

	posts, err := svc.ListFeed(context.Background(), "cursor-1", 25)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cache.calls != 1 {
		t.Errorf("cache consulted %d times, want exactly 1", cache.calls)
	}
	if len(posts) != 1 || posts[0].ID != "from-cache" {
		t.Errorf("posts = %+v, want the cached feed; the repository was queried instead", posts)
	}
	// The cursor and limit must reach the cache untouched, or paging silently
	// returns the wrong page.
	if cache.cursor != "cursor-1" || cache.limit != 25 {
		t.Errorf("cache got (cursor %q, limit %d), want (%q, %d)", cache.cursor, cache.limit, "cursor-1", 25)
	}
}

func TestPostService_ListFeed_FallsBackToTheRepositoryWithoutACache(t *testing.T) {
	repo := newMockPostRepo()
	repo.posts["from-repo"] = &domain.Post{ID: "from-repo", Status: domain.PostVisible}

	svc := NewPostService(repo, nil)

	posts, err := svc.ListFeed(context.Background(), "", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(posts) != 1 || posts[0].ID != "from-repo" {
		t.Errorf("posts = %+v, want the repository feed", posts)
	}
}

// A cache failure must surface rather than silently falling back to the
// repository, which would mask an unhealthy cache behind a slower feed.
func TestPostService_ListFeed_PropagatesCacheErrors(t *testing.T) {
	wantErr := errors.New("redis down")
	repo := newMockPostRepo()
	repo.posts["from-repo"] = &domain.Post{ID: "from-repo", Status: domain.PostVisible}

	svc := NewPostService(repo, nil)
	svc.SetFeedCache(&stubFeedCache{err: wantErr})

	if _, err := svc.ListFeed(context.Background(), "", 10); !errors.Is(err, wantErr) {
		t.Errorf("error = %v, want %v", err, wantErr)
	}
}

// sent marks a table case as having supplied alt_text, so that `sent("")` — an
// author clearing the description — reads differently from `nil`, which is a
// PATCH that never mentioned it.
func sent(altText string) *string { return &altText }

func TestPostService_UpdateContent(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name    string
		post    *domain.Post
		userID  string
		body    string
		altText *string
		wantAlt string
		clock   time.Time
		wantErr error
	}{
		{
			name: "author within window",
			post: &domain.Post{
				ID:        "post-1",
				AuthorID:  "user-1",
				Body:      "original",
				Status:    domain.PostVisible,
				CreatedAt: now.Add(-10 * time.Minute),
			},
			userID: "user-1",
			body:   "updated body",
			clock:  now,
		},
		{
			name: "at exact 15-min boundary",
			post: &domain.Post{
				ID:        "post-2",
				AuthorID:  "user-1",
				Body:      "original",
				Status:    domain.PostVisible,
				CreatedAt: now.Add(-15 * time.Minute),
			},
			userID: "user-1",
			body:   "updated at boundary",
			clock:  now,
		},
		{
			name: "wrong user",
			post: &domain.Post{
				ID:        "post-3",
				AuthorID:  "user-1",
				Body:      "original",
				Status:    domain.PostVisible,
				CreatedAt: now.Add(-5 * time.Minute),
			},
			userID:  "user-2",
			body:    "hijack attempt",
			clock:   now,
			wantErr: ErrEditWindow,
		},
		{
			name: "past 15-min window",
			post: &domain.Post{
				ID:        "post-4",
				AuthorID:  "user-1",
				Body:      "original",
				Status:    domain.PostVisible,
				CreatedAt: now.Add(-20 * time.Minute),
			},
			userID:  "user-1",
			body:    "too late",
			clock:   now,
			wantErr: ErrEditWindow,
		},
		{
			name: "removed post",
			post: &domain.Post{
				ID:        "post-5",
				AuthorID:  "user-1",
				Body:      "original",
				Status:    domain.PostRemovedByAuthor,
				CreatedAt: now.Add(-5 * time.Minute),
			},
			userID:  "user-1",
			body:    "revive attempt",
			clock:   now,
			wantErr: ErrEditWindow,
		},
		{
			name:    "not found",
			post:    nil, // no post seeded
			userID:  "user-1",
			body:    "edit ghost",
			clock:   now,
			wantErr: ErrNotFound,
		},
		{
			name: "empty new body",
			post: &domain.Post{
				ID:        "post-6",
				AuthorID:  "user-1",
				Body:      "original",
				Status:    domain.PostVisible,
				CreatedAt: now.Add(-5 * time.Minute),
			},
			userID:  "user-1",
			body:    "",
			clock:   now,
			wantErr: ErrValidation,
		},
		{
			name: "body exceeds max",
			post: &domain.Post{
				ID:        "post-7",
				AuthorID:  "user-1",
				Body:      "original",
				Status:    domain.PostVisible,
				CreatedAt: now.Add(-5 * time.Minute),
			},
			userID:  "user-1",
			body:    strings.Repeat("x", domain.MaxPostBodyLength+1),
			clock:   now,
			wantErr: ErrValidation,
		},
		{
			// The contract the whole feature turns on: an edit that says
			// nothing about the description must not remove it.
			name: "omitted alt text survives a body edit",
			post: &domain.Post{
				ID:        "post-8",
				AuthorID:  "user-1",
				Body:      "original",
				ImagePath: "/uploads/heron.jpg",
				AltText:   "A heron on the frozen millpond",
				Status:    domain.PostVisible,
				CreatedAt: now.Add(-5 * time.Minute),
			},
			userID:  "user-1",
			body:    "original, with the typo fixed",
			altText: nil,
			wantAlt: "A heron on the frozen millpond",
			clock:   now,
		},
		{
			name: "alt text replaced when sent",
			post: &domain.Post{
				ID:        "post-9",
				AuthorID:  "user-1",
				Body:      "original",
				ImagePath: "/uploads/heron.jpg",
				AltText:   "A bird",
				Status:    domain.PostVisible,
				CreatedAt: now.Add(-5 * time.Minute),
			},
			userID:  "user-1",
			body:    "original",
			altText: sent("A heron on the frozen millpond"),
			wantAlt: "A heron on the frozen millpond",
			clock:   now,
		},
		{
			// An empty string is the only way to say "I had it wrong, describe
			// nothing", so it has to reach the column rather than being read as
			// an omission.
			name: "empty alt text clears the description",
			post: &domain.Post{
				ID:        "post-10",
				AuthorID:  "user-1",
				Body:      "original",
				ImagePath: "/uploads/heron.jpg",
				AltText:   "A heron on the frozen millpond",
				Status:    domain.PostVisible,
				CreatedAt: now.Add(-5 * time.Minute),
			},
			userID:  "user-1",
			body:    "original",
			altText: sent(""),
			wantAlt: "",
			clock:   now,
		},
		{
			name: "alt text on a post with no image",
			post: &domain.Post{
				ID:        "post-11",
				AuthorID:  "user-1",
				Body:      "original",
				Status:    domain.PostVisible,
				CreatedAt: now.Add(-5 * time.Minute),
			},
			userID:  "user-1",
			body:    "original",
			altText: sent("A heron on the frozen millpond"),
			clock:   now,
			wantErr: ErrValidation,
		},
		{
			name: "alt text exceeds the rune limit",
			post: &domain.Post{
				ID:        "post-12",
				AuthorID:  "user-1",
				Body:      "original",
				ImagePath: "/uploads/heron.jpg",
				Status:    domain.PostVisible,
				CreatedAt: now.Add(-5 * time.Minute),
			},
			userID:  "user-1",
			body:    "original",
			altText: sent(strings.Repeat("a", domain.MaxAltTextRunes+1)),
			clock:   now,
			wantErr: ErrValidation,
		},
		{
			// Same window as the body, and it closes on both at once: there is
			// one edit, not an edit and a separate description edit.
			name: "alt text is not editable once the window closes",
			post: &domain.Post{
				ID:        "post-13",
				AuthorID:  "user-1",
				Body:      "original",
				ImagePath: "/uploads/heron.jpg",
				AltText:   "A bird",
				Status:    domain.PostVisible,
				CreatedAt: now.Add(-20 * time.Minute),
			},
			userID:  "user-1",
			body:    "original",
			altText: sent("A heron on the frozen millpond"),
			clock:   now,
			wantErr: ErrEditWindow,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newMockPostRepo()
			svc := NewPostService(repo, func() time.Time { return tt.clock })

			postID := "nonexistent"
			if tt.post != nil {
				repo.posts[tt.post.ID] = tt.post
				postID = tt.post.ID
			}

			updated, err := svc.UpdateContent(context.Background(), postID, tt.userID, tt.body, tt.altText)

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("UpdateContent() error = %v, wantErr %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("UpdateContent() unexpected error: %v", err)
			}
			if updated.Body != tt.body {
				t.Errorf("Body = %q, want %q", updated.Body, tt.body)
			}
			if updated.AltText != tt.wantAlt {
				t.Errorf("AltText = %q, want %q", updated.AltText, tt.wantAlt)
			}
		})
	}
}

// A rejected description must leave the post exactly as it was — including its
// body, which is validated first and would otherwise be written by an edit the
// service went on to refuse.
func TestPostService_UpdateContent_RejectedAltTextWritesNothing(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	repo := newMockPostRepo()
	repo.posts["post-1"] = &domain.Post{
		ID:        "post-1",
		AuthorID:  "user-1",
		Body:      "original",
		ImagePath: "/uploads/heron.jpg",
		AltText:   "A heron on the frozen millpond",
		Status:    domain.PostVisible,
		CreatedAt: now.Add(-5 * time.Minute),
	}
	svc := NewPostService(repo, func() time.Time { return now })

	tooLong := strings.Repeat("a", domain.MaxAltTextRunes+1)
	if _, err := svc.UpdateContent(context.Background(), "post-1", "user-1", "new body", &tooLong); !errors.Is(err, ErrValidation) {
		t.Fatalf("UpdateContent() error = %v, want %v", err, ErrValidation)
	}

	stored := repo.posts["post-1"]
	if stored.Body != "original" {
		t.Errorf("Body = %q, want the edit rolled back to %q", stored.Body, "original")
	}
	if stored.AltText != "A heron on the frozen millpond" {
		t.Errorf("AltText = %q, want it untouched", stored.AltText)
	}
}

func TestPostService_Delete(t *testing.T) {
	tests := []struct {
		name    string
		post    *domain.Post
		userID  string
		wantErr error
	}{
		{
			name: "author deletes own post",
			post: &domain.Post{
				ID:       "post-1",
				AuthorID: "user-1",
				Body:     "to be deleted",
				Status:   domain.PostVisible,
			},
			userID: "user-1",
		},
		{
			name: "non-author forbidden",
			post: &domain.Post{
				ID:       "post-2",
				AuthorID: "user-1",
				Body:     "not yours",
				Status:   domain.PostVisible,
			},
			userID:  "user-2",
			wantErr: ErrForbidden,
		},
		{
			name:    "not found",
			post:    nil,
			userID:  "user-1",
			wantErr: ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newMockPostRepo()
			svc := NewPostService(repo, nil)

			postID := "nonexistent"
			if tt.post != nil {
				repo.posts[tt.post.ID] = tt.post
				postID = tt.post.ID
			}

			err := svc.Delete(context.Background(), postID, tt.userID)

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Delete() error = %v, wantErr %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Delete() unexpected error: %v", err)
			}

			// Verify post was soft-deleted
			p := repo.posts[postID]
			if p.Status != domain.PostRemovedByAuthor {
				t.Errorf("Status = %q, want %q", p.Status, domain.PostRemovedByAuthor)
			}
		})
	}
}

// recordingFeedCache captures what the service hands to the cache.
type recordingFeedCache struct {
	created *domain.Post
	updated *domain.Post
}

func (c *recordingFeedCache) GetFeed(context.Context, string, int) ([]*domain.Post, error) {
	return nil, nil
}
func (c *recordingFeedCache) InvalidateOnCreate(_ context.Context, p *domain.Post) {
	// Copy, because the caller may keep mutating the post afterwards.
	cp := *p
	c.created = &cp
}
func (c *recordingFeedCache) InvalidateOnUpdate(_ context.Context, p *domain.Post) {
	cp := *p
	c.updated = &cp
}
func (c *recordingFeedCache) InvalidateOnDelete(context.Context, string) {}

// The post is written to the feed cache before Create returns, so the author
// fields must already be set. Filling them in afterwards cached — and served —
// a post with no author name, which json omitempty dropped entirely.
func TestPostService_Create_CachesPostWithAuthorFields(t *testing.T) {
	repo := newMockPostRepo()
	cache := &recordingFeedCache{}
	svc := NewPostService(repo, nil)
	svc.SetFeedCache(cache)

	author := &domain.User{
		ID: "u1", DisplayName: "Ada Lovelace", AvatarURL: "/avatars/ada.png",
		IsActive: true, TrustScore: 50, Role: domain.RoleMember,
	}

	post, err := svc.Create(context.Background(), author, "hello town", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if post.AuthorDisplayName != author.DisplayName {
		t.Errorf("returned post display name = %q, want %q", post.AuthorDisplayName, author.DisplayName)
	}

	if cache.created == nil {
		t.Fatal("post was never handed to the feed cache")
	}
	if cache.created.AuthorDisplayName != author.DisplayName {
		t.Errorf("cached display name = %q, want %q", cache.created.AuthorDisplayName, author.DisplayName)
	}
	if cache.created.AuthorAvatarURL != author.AvatarURL {
		t.Errorf("cached avatar = %q, want %q", cache.created.AuthorAvatarURL, author.AvatarURL)
	}
}

// The cache holds whole posts, not IDs, so an edit that skips invalidation
// keeps serving the pre-edit body until the entry is evicted by length.
func TestPostService_UpdateContent_InvalidatesFeedCache(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

	repo := newMockPostRepo()
	repo.posts["post-1"] = &domain.Post{
		ID:        "post-1",
		AuthorID:  "user-1",
		Body:      "original",
		Status:    domain.PostVisible,
		CreatedAt: now.Add(-5 * time.Minute),
	}

	cache := &recordingFeedCache{}
	svc := NewPostService(repo, func() time.Time { return now })
	svc.SetFeedCache(cache)

	if _, err := svc.UpdateContent(context.Background(), "post-1", "user-1", "edited", nil); err != nil {
		t.Fatalf("UpdateContent() unexpected error: %v", err)
	}

	if cache.updated == nil {
		t.Fatal("edited post was never handed to the feed cache")
	}
	if cache.updated.Body != "edited" {
		t.Errorf("invalidated with body = %q, want %q", cache.updated.Body, "edited")
	}
}

// A rejected edit must not invalidate: the feed still holds the current body.
func TestPostService_UpdateContent_DoesNotInvalidateWhenEditRejected(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

	repo := newMockPostRepo()
	repo.posts["post-1"] = &domain.Post{
		ID:        "post-1",
		AuthorID:  "user-1",
		Body:      "original",
		Status:    domain.PostVisible,
		CreatedAt: now.Add(-20 * time.Minute), // outside the edit window
	}

	cache := &recordingFeedCache{}
	svc := NewPostService(repo, func() time.Time { return now })
	svc.SetFeedCache(cache)

	if _, err := svc.UpdateContent(context.Background(), "post-1", "user-1", "too late", nil); !errors.Is(err, ErrEditWindow) {
		t.Fatalf("UpdateContent() error = %v, want %v", err, ErrEditWindow)
	}

	if cache.updated != nil {
		t.Error("feed cache was invalidated for an edit that never happened")
	}
}

// postingUser is a member who clears every gate in CanPost, so a test asserting
// on body validation is not silently short-circuited by authorization.
func postingUser(id string) *domain.User {
	return &domain.User{
		ID:         id,
		IsActive:   true,
		TrustScore: domain.PostingThreshold,
		Role:       domain.RoleMember,
	}
}

// The handler checks CanPost too, but this is the check that cannot be
// bypassed: before it existed, one line in one handler was the only thing
// between a muted user and a post. Every gate is exercised here, not just the
// mute, because the service is now the authority for all of them.
func TestPostService_Create_RefusesUsersWhoCannotPost(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	muted := now.Add(time.Hour)

	tests := []struct {
		name   string
		author *domain.User
	}{
		{"muted", &domain.User{ID: "u1", IsActive: true, TrustScore: 95, Role: domain.RoleMember, MutedUntil: &muted}},
		{"banned", &domain.User{ID: "u1", IsActive: true, TrustScore: 95, Role: domain.RoleBanned}},
		{"pending", &domain.User{ID: "u1", IsActive: true, TrustScore: 95, Role: domain.RolePending}},
		{"deactivated", &domain.User{ID: "u1", IsActive: false, TrustScore: 95, Role: domain.RoleMember}},
		{"below the posting threshold", &domain.User{ID: "u1", IsActive: true, TrustScore: 10, Role: domain.RoleMember}},
		{"no author at all", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newMockPostRepo()
			svc := NewPostService(repo, func() time.Time { return now })

			post, err := svc.Create(context.Background(), tt.author, "hello town", "", "")
			if !errors.Is(err, ErrForbidden) {
				t.Fatalf("Create() error = %v, want ErrForbidden", err)
			}
			if post != nil {
				t.Errorf("Create() returned %+v, want nil", post)
			}
			if len(repo.posts) != 0 {
				t.Errorf("%d posts were written despite the rejection", len(repo.posts))
			}
		})
	}
}

// The mute is evaluated against the service's own clock, so a mute that has
// expired by the time the post arrives does not block it.
func TestPostService_Create_ExpiredMuteDoesNotBlock(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	expired := now.Add(-time.Second)

	repo := newMockPostRepo()
	svc := NewPostService(repo, func() time.Time { return now })

	author := &domain.User{
		ID: "u1", IsActive: true, TrustScore: 50, Role: domain.RoleMember, MutedUntil: &expired,
	}

	if _, err := svc.Create(context.Background(), author, "hello town", "", ""); err != nil {
		t.Fatalf("Create() error = %v, want the expired mute ignored", err)
	}
}

// moderatingUser is an active moderator, so a test asserting on removal
// behaviour is not silently short-circuited by authorization.
func moderatingUser(id string) *domain.User {
	return &domain.User{ID: id, IsActive: true, Role: domain.RoleModerator}
}

func TestPostService_RemoveByModerator(t *testing.T) {
	repo := newMockPostRepo()
	repo.posts["post-1"] = &domain.Post{
		ID: "post-1", AuthorID: "user-1", Body: "offending", Status: domain.PostVisible,
	}

	svc := NewPostService(repo, nil)

	err := svc.RemoveByModerator(context.Background(), moderatingUser("mod-1"), "post-1", "  harassment  ")
	if err != nil {
		t.Fatalf("RemoveByModerator() unexpected error: %v", err)
	}

	p := repo.posts["post-1"]
	if p.Status != domain.PostRemovedByMod {
		t.Errorf("Status = %q, want %q", p.Status, domain.PostRemovedByMod)
	}
	// The trimmed reason is what lands, matching validateRemovalReason.
	if p.RemovalReason != "harassment" {
		t.Errorf("RemovalReason = %q, want %q", p.RemovalReason, "harassment")
	}
	// Without this the removal has a note but no author, which is not an audit
	// trail — it records that "a moderator" acted, not which one.
	if p.RemovedBy != "mod-1" {
		t.Errorf("RemovedBy = %q, want the acting moderator %q", p.RemovedBy, "mod-1")
	}
}

// An author deleting their own post has no moderator to name. Writing anything
// here would be an invention, and the column is a foreign key to users.
func TestPostService_Delete_RecordsNoRemovingModerator(t *testing.T) {
	repo := newMockPostRepo()
	repo.posts["post-1"] = &domain.Post{
		ID: "post-1", AuthorID: "user-1", Status: domain.PostVisible,
	}
	svc := NewPostService(repo, nil)

	if err := svc.Delete(context.Background(), "post-1", "user-1"); err != nil {
		t.Fatalf("Delete() unexpected error: %v", err)
	}

	if got := repo.posts["post-1"].RemovedBy; got != "" {
		t.Errorf("RemovedBy = %q on an author deletion, want empty", got)
	}
}

// The route group guards this endpoint, but the service is the check that
// cannot be bypassed: one line in routes.go is otherwise all that stands
// between an ordinary member and taking down anyone's post. This is the same
// reasoning that put the CanPost check inside Create.
func TestPostService_RemoveByModerator_RefusesUsersWhoCannotModerate(t *testing.T) {
	tests := []struct {
		name      string
		moderator *domain.User
	}{
		{"an ordinary member", &domain.User{ID: "u1", IsActive: true, Role: domain.RoleMember}},
		{"a pending user", &domain.User{ID: "u1", IsActive: true, Role: domain.RolePending}},
		{"a banned user", &domain.User{ID: "u1", IsActive: true, Role: domain.RoleBanned}},
		{"a deactivated moderator", &domain.User{ID: "u1", IsActive: false, Role: domain.RoleModerator}},
		{"a deactivated council member", &domain.User{ID: "u1", IsActive: false, Role: domain.RoleCouncil}},
		{"no user at all", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newMockPostRepo()
			repo.posts["post-1"] = &domain.Post{
				ID: "post-1", AuthorID: "user-1", Status: domain.PostVisible,
			}
			svc := NewPostService(repo, nil)

			err := svc.RemoveByModerator(context.Background(), tt.moderator, "post-1", "spam")
			if !errors.Is(err, ErrForbidden) {
				t.Fatalf("RemoveByModerator() error = %v, want ErrForbidden", err)
			}
			if repo.posts["post-1"].Status != domain.PostVisible {
				t.Errorf("post was removed despite the rejection: status = %q", repo.posts["post-1"].Status)
			}
		})
	}
}

// The council moderates too, and the route group admits them.
func TestPostService_RemoveByModerator_AllowsCouncil(t *testing.T) {
	repo := newMockPostRepo()
	repo.posts["post-1"] = &domain.Post{ID: "post-1", AuthorID: "user-1", Status: domain.PostVisible}
	svc := NewPostService(repo, nil)

	council := &domain.User{ID: "c1", IsActive: true, Role: domain.RoleCouncil}
	if err := svc.RemoveByModerator(context.Background(), council, "post-1", "spam"); err != nil {
		t.Fatalf("RemoveByModerator() error = %v, want the council admitted", err)
	}
}

func TestPostService_RemoveByModerator_RejectsAnEmptyReason(t *testing.T) {
	repo := newMockPostRepo()
	repo.posts["post-1"] = &domain.Post{ID: "post-1", AuthorID: "user-1", Status: domain.PostVisible}
	svc := NewPostService(repo, nil)

	err := svc.RemoveByModerator(context.Background(), moderatingUser("mod-1"), "post-1", "   ")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("RemoveByModerator() error = %v, want ErrValidation", err)
	}
	if repo.posts["post-1"].Status != domain.PostVisible {
		t.Error("post was removed despite the reason being rejected")
	}
}

func TestPostService_RemoveByModerator_UnknownPost(t *testing.T) {
	svc := NewPostService(newMockPostRepo(), nil)

	err := svc.RemoveByModerator(context.Background(), moderatingUser("mod-1"), "no-such-post", "spam")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("RemoveByModerator() error = %v, want ErrNotFound", err)
	}
}

// Re-removing would overwrite the record: on a post the author deleted it
// would erase removed_by_author, and on one another moderator handled it would
// replace their note. Neither post is visible to the town either way.
func TestPostService_RemoveByModerator_RefusesAPostThatIsAlreadyGone(t *testing.T) {
	for _, status := range []domain.PostStatus{domain.PostRemovedByAuthor, domain.PostRemovedByMod} {
		t.Run(string(status), func(t *testing.T) {
			repo := newMockPostRepo()
			repo.posts["post-1"] = &domain.Post{
				ID: "post-1", AuthorID: "user-1", Status: status, RemovalReason: "the original note",
			}
			svc := NewPostService(repo, nil)

			err := svc.RemoveByModerator(context.Background(), moderatingUser("mod-1"), "post-1", "a second note")
			if !errors.Is(err, ErrValidation) {
				t.Fatalf("RemoveByModerator() error = %v, want ErrValidation", err)
			}
			if repo.posts["post-1"].RemovalReason != "the original note" {
				t.Errorf("RemovalReason = %q, want the original note left intact",
					repo.posts["post-1"].RemovalReason)
			}
			if repo.posts["post-1"].Status != status {
				t.Errorf("Status = %q, want %q left intact", repo.posts["post-1"].Status, status)
			}
		})
	}
}

// The feed cache holds whole posts in a Redis sorted set, so a removal that
// skips invalidation keeps serving the post to the whole town until the entry
// is evicted by length — which is not bounded by the feed TTL. PostService's
// author-deletion path calls InvalidateOnDelete for exactly this reason.
func TestPostService_RemoveByModerator_InvalidatesFeedCache(t *testing.T) {
	repo := newMockPostRepo()
	repo.posts["post-1"] = &domain.Post{ID: "post-1", AuthorID: "user-1", Status: domain.PostVisible}

	cache := &deleteRecordingFeedCache{}
	svc := NewPostService(repo, nil)
	svc.SetFeedCache(cache)

	if err := svc.RemoveByModerator(context.Background(), moderatingUser("mod-1"), "post-1", "spam"); err != nil {
		t.Fatalf("RemoveByModerator() unexpected error: %v", err)
	}

	if cache.deleted != "post-1" {
		t.Errorf("feed cache invalidated for %q, want %q; a removed post keeps serving from Redis",
			cache.deleted, "post-1")
	}
}

// A rejected removal must not invalidate: the post is still in the feed, and
// clearing the key would evict the whole town's feed for nothing.
func TestPostService_RemoveByModerator_DoesNotInvalidateWhenRefused(t *testing.T) {
	repo := newMockPostRepo()
	repo.posts["post-1"] = &domain.Post{ID: "post-1", AuthorID: "user-1", Status: domain.PostVisible}

	cache := &deleteRecordingFeedCache{}
	svc := NewPostService(repo, nil)
	svc.SetFeedCache(cache)

	member := &domain.User{ID: "u1", IsActive: true, Role: domain.RoleMember}
	if err := svc.RemoveByModerator(context.Background(), member, "post-1", "spam"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("RemoveByModerator() error = %v, want ErrForbidden", err)
	}

	if cache.deleted != "" {
		t.Errorf("feed cache was invalidated for %q, but the removal never happened", cache.deleted)
	}
}

// deleteRecordingFeedCache captures the post ID the service invalidates on.
type deleteRecordingFeedCache struct {
	deleted string
}

func (c *deleteRecordingFeedCache) GetFeed(context.Context, string, int) ([]*domain.Post, error) {
	return nil, nil
}
func (c *deleteRecordingFeedCache) InvalidateOnCreate(context.Context, *domain.Post) {}
func (c *deleteRecordingFeedCache) InvalidateOnUpdate(context.Context, *domain.Post) {}
func (c *deleteRecordingFeedCache) InvalidateOnDelete(_ context.Context, postID string) {
	c.deleted = postID
}
