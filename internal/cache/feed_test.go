package cache

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/service"
	"github.com/redis/go-redis/v9"
)

// stubPostRepo is a minimal in-memory PostRepository for testing the cache.
type stubPostRepo struct {
	posts []*domain.Post
}

func (s *stubPostRepo) CreatePost(_ context.Context, post *domain.Post) error {
	s.posts = append(s.posts, post)
	return nil
}

func (s *stubPostRepo) GetPostByID(_ context.Context, id string) (*domain.Post, error) {
	for _, p := range s.posts {
		if p.ID == id {
			return p, nil
		}
	}
	return nil, service.ErrNotFound
}

func (s *stubPostRepo) ListPosts(_ context.Context, cursor string, limit int) ([]*domain.Post, error) {
	var result []*domain.Post
	pastCursor := cursor == ""
	for i := len(s.posts) - 1; i >= 0; i-- {
		p := s.posts[i]
		if p.Status != domain.PostVisible {
			continue
		}
		if !pastCursor {
			if p.ID == cursor {
				pastCursor = true
			}
			continue
		}
		result = append(result, p)
		if len(result) >= limit {
			break
		}
	}
	return result, nil
}

func (s *stubPostRepo) ListPostsByAuthor(_ context.Context, authorID string, limit int) ([]*domain.Post, error) {
	var result []*domain.Post
	for _, p := range s.posts {
		if p.AuthorID == authorID {
			result = append(result, p)
			if len(result) >= limit {
				break
			}
		}
	}
	return result, nil
}

func (s *stubPostRepo) UpdatePostBody(_ context.Context, id string, body string) (*domain.Post, error) {
	for _, p := range s.posts {
		if p.ID == id {
			p.Body = body
			return p, nil
		}
	}
	return nil, service.ErrNotFound
}

func (s *stubPostRepo) UpdatePostStatus(_ context.Context, id string, status domain.PostStatus, reason string) error {
	for _, p := range s.posts {
		if p.ID == id {
			p.Status = status
			p.RemovalReason = reason
			return nil
		}
	}
	return service.ErrNotFound
}

func newTestFeedCache(t *testing.T, repo service.PostRepository) (*FeedCache, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { rdb.Close() })

	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	return NewFeedCache(rdb, repo, logger), mr
}

func makePosts(n int) []*domain.Post {
	posts := make([]*domain.Post, n)
	base := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	for i := range n {
		posts[i] = &domain.Post{
			ID:        "post-" + string(rune('a'+i)),
			AuthorID:  "user-1",
			Body:      "body " + string(rune('a'+i)),
			Status:    domain.PostVisible,
			CreatedAt: base.Add(time.Duration(i) * time.Second),
		}
	}
	return posts
}

func TestGetFeed_CacheMiss_FallsBackToRepo(t *testing.T) {
	repo := &stubPostRepo{posts: makePosts(3)}
	fc, _ := newTestFeedCache(t, repo)

	posts, err := fc.GetFeed(context.Background(), "", 10)
	if err != nil {
		t.Fatalf("GetFeed() error = %v", err)
	}
	if len(posts) != 3 {
		t.Fatalf("GetFeed() returned %d posts, want 3", len(posts))
	}
}

func TestGetFeed_CacheHit(t *testing.T) {
	repo := &stubPostRepo{posts: makePosts(3)}
	fc, _ := newTestFeedCache(t, repo)

	// First call: cache miss, falls through to repo, warms cache
	_, err := fc.GetFeed(context.Background(), "", 10)
	if err != nil {
		t.Fatalf("first GetFeed() error = %v", err)
	}

	// Wait for the async warm goroutine to complete.
	time.Sleep(50 * time.Millisecond)

	// Modify repo to return different data — but cached result should be served.
	repo.posts = nil

	posts, err := fc.GetFeed(context.Background(), "", 10)
	if err != nil {
		t.Fatalf("second GetFeed() error = %v", err)
	}
	if len(posts) != 3 {
		t.Fatalf("GetFeed() returned %d posts from cache, want 3", len(posts))
	}
}

func TestGetFeed_CursorBypassesCache(t *testing.T) {
	repo := &stubPostRepo{posts: makePosts(5)}
	fc, _ := newTestFeedCache(t, repo)

	// Cursor-based requests always go to Postgres.
	posts, err := fc.GetFeed(context.Background(), "post-c", 10)
	if err != nil {
		t.Fatalf("GetFeed() with cursor error = %v", err)
	}
	// The stub returns posts after the cursor (post-b, post-a).
	if len(posts) != 2 {
		t.Fatalf("GetFeed() with cursor returned %d posts, want 2", len(posts))
	}
}

func TestInvalidateOnCreate_AddsToCache(t *testing.T) {
	repo := &stubPostRepo{posts: makePosts(2)}
	fc, _ := newTestFeedCache(t, repo)

	// Warm the cache first.
	_, _ = fc.GetFeed(context.Background(), "", 10)
	time.Sleep(50 * time.Millisecond)

	// Create a new post and invalidate.
	newPost := &domain.Post{
		ID:        "post-new",
		AuthorID:  "user-1",
		Body:      "brand new",
		Status:    domain.PostVisible,
		CreatedAt: time.Date(2026, 3, 1, 13, 0, 0, 0, time.UTC),
	}
	fc.InvalidateOnCreate(context.Background(), newPost)

	// Clear repo so only cache is serving.
	repo.posts = nil

	posts, err := fc.GetFeed(context.Background(), "", 10)
	if err != nil {
		t.Fatalf("GetFeed() after create error = %v", err)
	}
	if len(posts) != 3 {
		t.Fatalf("GetFeed() returned %d posts, want 3 (2 original + 1 new)", len(posts))
	}

	// Newest post should be first (highest score).
	if posts[0].ID != "post-new" {
		t.Errorf("newest post ID = %q, want %q", posts[0].ID, "post-new")
	}
}

func TestInvalidateOnDelete_ClearsCache(t *testing.T) {
	repo := &stubPostRepo{posts: makePosts(3)}
	fc, mr := newTestFeedCache(t, repo)

	// Warm the cache.
	_, _ = fc.GetFeed(context.Background(), "", 10)
	time.Sleep(50 * time.Millisecond)

	// Verify cache key exists.
	if !mr.Exists(feedKey) {
		t.Fatal("expected feed key to exist after warm")
	}

	fc.InvalidateOnDelete(context.Background(), "post-a")

	// Key should be removed.
	if mr.Exists(feedKey) {
		t.Error("expected feed key to be deleted after InvalidateOnDelete")
	}
}

func TestInvalidateOnCreate_TrimsToMaxLen(t *testing.T) {
	// Start with feedMaxLen posts in the cache.
	repo := &stubPostRepo{posts: makePosts(feedMaxLen)}
	fc, mr := newTestFeedCache(t, repo)

	// Warm the cache.
	_, _ = fc.GetFeed(context.Background(), "", feedMaxLen)
	time.Sleep(50 * time.Millisecond)

	// Add one more — cache should still be capped at feedMaxLen.
	newPost := &domain.Post{
		ID:        "post-overflow",
		AuthorID:  "user-1",
		Body:      "over the limit",
		Status:    domain.PostVisible,
		CreatedAt: time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC),
	}
	fc.InvalidateOnCreate(context.Background(), newPost)

	members, err := mr.ZMembers(feedKey)
	if err != nil {
		t.Fatalf("ZMembers error: %v", err)
	}
	if len(members) != feedMaxLen {
		t.Errorf("sorted set has %d members, want %d", len(members), feedMaxLen)
	}
}

func TestGetFeed_TTLIsSet(t *testing.T) {
	repo := &stubPostRepo{posts: makePosts(2)}
	fc, mr := newTestFeedCache(t, repo)

	// Warm the cache.
	_, _ = fc.GetFeed(context.Background(), "", 10)
	time.Sleep(50 * time.Millisecond)

	ttl := mr.TTL(feedKey)
	if ttl <= 0 {
		t.Errorf("feed key TTL = %v, want > 0", ttl)
	}
	if ttl > feedTTL {
		t.Errorf("feed key TTL = %v, want <= %v", ttl, feedTTL)
	}
}
