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

func (m *mockPostRepo) UpdatePostBody(_ context.Context, id string, body string) (*domain.Post, error) {
	p, ok := m.posts[id]
	if !ok {
		return nil, ErrNotFound
	}
	p.Body = body
	now := time.Now()
	p.EditedAt = &now
	return p, nil
}

func (m *mockPostRepo) UpdatePostStatus(_ context.Context, id string, status domain.PostStatus, reason string) error {
	p, ok := m.posts[id]
	if !ok {
		return ErrNotFound
	}
	p.Status = status
	p.RemovalReason = reason
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
		wantErr   error
	}{
		{
			name:      "valid post",
			authorID:  "user-1",
			body:      "Hello, world!",
			imagePath: "/images/photo.jpg",
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

			post, err := svc.Create(context.Background(), postingUser(tt.authorID), tt.body, tt.imagePath)

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

func TestPostService_UpdateBody(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name    string
		post    *domain.Post
		userID  string
		body    string
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

			updated, err := svc.UpdateBody(context.Background(), postID, tt.userID, tt.body)

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("UpdateBody() error = %v, wantErr %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("UpdateBody() unexpected error: %v", err)
			}
			if updated.Body != tt.body {
				t.Errorf("Body = %q, want %q", updated.Body, tt.body)
			}
		})
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

	post, err := svc.Create(context.Background(), author, "hello town", "")
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
func TestPostService_UpdateBody_InvalidatesFeedCache(t *testing.T) {
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

	if _, err := svc.UpdateBody(context.Background(), "post-1", "user-1", "edited"); err != nil {
		t.Fatalf("UpdateBody() unexpected error: %v", err)
	}

	if cache.updated == nil {
		t.Fatal("edited post was never handed to the feed cache")
	}
	if cache.updated.Body != "edited" {
		t.Errorf("invalidated with body = %q, want %q", cache.updated.Body, "edited")
	}
}

// A rejected edit must not invalidate: the feed still holds the current body.
func TestPostService_UpdateBody_DoesNotInvalidateWhenEditRejected(t *testing.T) {
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

	if _, err := svc.UpdateBody(context.Background(), "post-1", "user-1", "too late"); !errors.Is(err, ErrEditWindow) {
		t.Fatalf("UpdateBody() error = %v, want %v", err, ErrEditWindow)
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

			post, err := svc.Create(context.Background(), tt.author, "hello town", "")
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

	if _, err := svc.Create(context.Background(), author, "hello town", ""); err != nil {
		t.Fatalf("Create() error = %v, want the expired mute ignored", err)
	}
}
