package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/service"
	"github.com/fireynis/the-bell/internal/testsupport"
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

func (s *stubPostRepo) UpdatePostStatus(_ context.Context, id string, status domain.PostStatus, reason, removedBy string) error {
	for _, p := range s.posts {
		if p.ID == id {
			p.Status = status
			p.RemovalReason = reason
			return nil
		}
	}
	return service.ErrNotFound
}

// newTestFeedCache builds a FeedCache over a real Redis.
//
// This previously ran against miniredis, an in-process reimplementation of
// Redis. The behaviour these tests turn on — sorted-set rank trimming, TTL and
// pipelining — is exactly what a reimplementation is liable to get subtly
// wrong, so asserting against it proved nothing about production.
func newTestFeedCache(t *testing.T, repo service.PostRepository) (*FeedCache, *redis.Client) {
	t.Helper()
	rdb := testsupport.TestRedis(t)
	return NewFeedCache(rdb, repo, testLogger()), rdb
}

// postID names posts so that lexical and chronological order agree, which lets
// the trim tests say exactly which posts should have survived.
func postID(i int) string {
	return fmt.Sprintf("post-%03d", i)
}

func makePosts(n int) []*domain.Post {
	posts := make([]*domain.Post, n)
	base := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	for i := range n {
		posts[i] = &domain.Post{
			ID:        postID(i),
			AuthorID:  "user-1",
			Body:      "body " + postID(i),
			Status:    domain.PostVisible,
			CreatedAt: base.Add(time.Duration(i) * time.Second),
		}
	}
	return posts
}

// waitForWarm blocks until the background warm goroutine has populated the
// feed key. GetFeed warms the cache asynchronously, and a real Redis round-trip
// is slower and less predictable than an in-process fake, so polling for the
// result beats sleeping a fixed interval and hoping.
func waitForWarm(t *testing.T, rdb *redis.Client) {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		n, err := rdb.Exists(ctx, feedKey).Result()
		if err != nil {
			t.Fatalf("Exists: %v", err)
		}
		if n == 1 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out waiting for the feed cache to warm")
}

// feedMembers returns the cached posts in Redis rank order (oldest first).
func feedMembers(t *testing.T, rdb *redis.Client) []*domain.Post {
	t.Helper()
	raw, err := rdb.ZRange(context.Background(), feedKey, 0, -1).Result()
	if err != nil {
		t.Fatalf("ZRange: %v", err)
	}
	posts := make([]*domain.Post, 0, len(raw))
	for _, r := range raw {
		var p domain.Post
		if err := json.Unmarshal([]byte(r), &p); err != nil {
			t.Fatalf("unmarshalling cached member: %v", err)
		}
		posts = append(posts, &p)
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
	fc, rdb := newTestFeedCache(t, repo)

	// First call: cache miss, falls through to repo, warms cache
	_, err := fc.GetFeed(context.Background(), "", 10)
	if err != nil {
		t.Fatalf("first GetFeed() error = %v", err)
	}

	waitForWarm(t, rdb)

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
	posts, err := fc.GetFeed(context.Background(), postID(2), 10)
	if err != nil {
		t.Fatalf("GetFeed() with cursor error = %v", err)
	}
	// The stub returns posts after the cursor (post-001, post-000).
	if len(posts) != 2 {
		t.Fatalf("GetFeed() with cursor returned %d posts, want 2", len(posts))
	}
}

func TestInvalidateOnCreate_AddsToCache(t *testing.T) {
	repo := &stubPostRepo{posts: makePosts(2)}
	fc, rdb := newTestFeedCache(t, repo)

	// Warm the cache first.
	_, _ = fc.GetFeed(context.Background(), "", 10)
	waitForWarm(t, rdb)

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
	fc, rdb := newTestFeedCache(t, repo)
	ctx := context.Background()

	// Warm the cache.
	_, _ = fc.GetFeed(ctx, "", 10)
	waitForWarm(t, rdb)

	fc.InvalidateOnDelete(ctx, postID(0))

	exists, err := rdb.Exists(ctx, feedKey).Result()
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if exists != 0 {
		t.Error("expected feed key to be deleted after InvalidateOnDelete")
	}
}

// Sorted-set members are the marshalled posts, so an edited post no longer
// matches the member holding its old body. The feed must not keep serving the
// pre-edit body, and must not serve the post twice under two bodies.
func TestInvalidateOnUpdate_FeedServesNeitherStaleBodyNorDuplicate(t *testing.T) {
	repo := &stubPostRepo{posts: makePosts(3)}
	fc, rdb := newTestFeedCache(t, repo)

	// Warm the cache so the post is in the sorted set with its original body.
	_, _ = fc.GetFeed(context.Background(), "", 10)
	waitForWarm(t, rdb)

	updated, err := repo.UpdatePostBody(context.Background(), postID(1), "edited body")
	if err != nil {
		t.Fatalf("UpdatePostBody() error = %v", err)
	}
	fc.InvalidateOnUpdate(context.Background(), updated)

	posts, err := fc.GetFeed(context.Background(), "", 10)
	if err != nil {
		t.Fatalf("GetFeed() after update error = %v", err)
	}

	var seen int
	for _, p := range posts {
		if p.ID != postID(1) {
			continue
		}
		seen++
		if p.Body != "edited body" {
			t.Errorf("feed served body %q for %s, want %q", p.Body, postID(1), "edited body")
		}
	}
	if seen != 1 {
		t.Errorf("%s appeared %d times in the feed, want 1", postID(1), seen)
	}
}

func TestInvalidateOnCreate_TrimsToMaxLen(t *testing.T) {
	// Start with feedMaxLen posts in the cache.
	repo := &stubPostRepo{posts: makePosts(feedMaxLen)}
	fc, rdb := newTestFeedCache(t, repo)

	// Warm the cache.
	_, _ = fc.GetFeed(context.Background(), "", feedMaxLen)
	waitForWarm(t, rdb)

	// Add one more — cache should still be capped at feedMaxLen.
	newPost := &domain.Post{
		ID:        "post-overflow",
		AuthorID:  "user-1",
		Body:      "over the limit",
		Status:    domain.PostVisible,
		CreatedAt: time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC),
	}
	fc.InvalidateOnCreate(context.Background(), newPost)

	cached := feedMembers(t, rdb)
	if len(cached) != feedMaxLen {
		t.Fatalf("sorted set has %d members, want %d", len(cached), feedMaxLen)
	}

	// The trim bound ZRemRangeByRank(key, 0, -feedMaxLen-1) removes by
	// ascending rank, so it must drop the OLDEST entry and keep the newest.
	// Off by one in either direction and the feed silently loses its newest
	// post or keeps one too many.
	if cached[0].ID != postID(1) {
		t.Errorf("oldest cached post = %q, want %q (the original oldest, %q, should have been trimmed)",
			cached[0].ID, postID(1), postID(0))
	}
	if newest := cached[len(cached)-1]; newest.ID != "post-overflow" {
		t.Errorf("newest cached post = %q, want %q (the trim dropped the newest entry)",
			newest.ID, "post-overflow")
	}

	// And the trimmed post must really be gone.
	for _, p := range cached {
		if p.ID == postID(0) {
			t.Errorf("post %q survived the trim", postID(0))
		}
	}
}

// TestGetFeed_RoundTripsInNewestFirstOrder checks the ordering contract the
// feed depends on: ZRevRange must hand back the newest post first, and the
// bodies must survive the marshal/store/fetch/unmarshal round trip intact.
func TestGetFeed_RoundTripsInNewestFirstOrder(t *testing.T) {
	const n = 5
	repo := &stubPostRepo{posts: makePosts(n)}
	fc, rdb := newTestFeedCache(t, repo)

	_, _ = fc.GetFeed(context.Background(), "", n)
	waitForWarm(t, rdb)

	// Serve purely from cache.
	repo.posts = nil

	posts, err := fc.GetFeed(context.Background(), "", n)
	if err != nil {
		t.Fatalf("GetFeed() error = %v", err)
	}
	if len(posts) != n {
		t.Fatalf("GetFeed() returned %d posts, want %d", len(posts), n)
	}

	// makePosts increments CreatedAt with the index, so newest-first is
	// descending index.
	for i, p := range posts {
		want := postID(n - 1 - i)
		if p.ID != want {
			t.Errorf("position %d: post ID = %q, want %q (feed is not newest-first)", i, p.ID, want)
		}
		if p.Body != "body "+want {
			t.Errorf("position %d: body = %q, want %q (post did not survive the round trip)", i, p.Body, "body "+want)
		}
	}
}

// TestGetFeed_RespectsLimit checks that a limit smaller than the cached set
// returns exactly that many posts, still newest-first.
func TestGetFeed_RespectsLimit(t *testing.T) {
	repo := &stubPostRepo{posts: makePosts(10)}
	fc, rdb := newTestFeedCache(t, repo)

	_, _ = fc.GetFeed(context.Background(), "", 10)
	waitForWarm(t, rdb)

	repo.posts = nil

	posts, err := fc.GetFeed(context.Background(), "", 3)
	if err != nil {
		t.Fatalf("GetFeed() error = %v", err)
	}
	if len(posts) != 3 {
		t.Fatalf("GetFeed(limit=3) returned %d posts, want 3", len(posts))
	}
	if posts[0].ID != postID(9) {
		t.Errorf("first post = %q, want %q", posts[0].ID, postID(9))
	}
	if posts[2].ID != postID(7) {
		t.Errorf("third post = %q, want %q", posts[2].ID, postID(7))
	}
}

// warmGatedRepo holds background rebuilds open so the test can inspect how many
// were started while one was already running, and counts them.
//
// warmCache is the only caller that reads feedMaxLen posts; a request serving
// its own miss asks for its own smaller limit, so the limit distinguishes the
// two without the repo needing to know who called it.
type warmGatedRepo struct {
	*stubPostRepo
	entered   chan struct{} // closed when the first rebuild starts
	release   chan struct{} // closed by the test to let rebuilds finish
	warmCalls atomic.Int32
	once      sync.Once
}

func (r *warmGatedRepo) ListPosts(ctx context.Context, cursor string, limit int) ([]*domain.Post, error) {
	if limit == feedMaxLen {
		r.warmCalls.Add(1)
		r.once.Do(func() { close(r.entered) })
		<-r.release
	}
	return r.stubPostRepo.ListPosts(ctx, cursor, limit)
}

// A cold cache under load used to fan out into one full rebuild per in-flight
// request — every concurrent miss launched its own warmCache goroutine, so the
// database took N identical ListPosts(feedMaxLen) queries at its worst moment.
func TestGetFeed_ConcurrentMissesTriggerOneRebuild(t *testing.T) {
	repo := &warmGatedRepo{
		stubPostRepo: &stubPostRepo{posts: makePosts(3)},
		entered:      make(chan struct{}),
		release:      make(chan struct{}),
	}
	fc, rdb := newTestFeedCache(t, repo)

	// Park a rebuild in flight, so the burst below races against a live one
	// rather than against an already-finished one.
	go func() { _, _ = fc.GetFeed(context.Background(), "", 10) }()
	<-repo.entered

	const burst = 8
	var wg sync.WaitGroup
	for range burst {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := fc.GetFeed(context.Background(), "", 10); err != nil {
				t.Errorf("GetFeed() error = %v", err)
			}
		}()
	}
	wg.Wait()

	// Let the rebuild finish and land in Redis. Every miss above has returned,
	// so any rebuild they started has been counted by now.
	close(repo.release)
	waitForWarm(t, rdb)

	if got := repo.warmCalls.Load(); got != 1 {
		t.Errorf("%d concurrent misses triggered %d rebuilds, want 1", burst+1, got)
	}
}

// The guard must not latch: once a rebuild finishes, a later miss is entitled
// to start a fresh one, otherwise the cache would never rebuild again.
func TestGetFeed_RebuildSlotIsReleasedAfterWarming(t *testing.T) {
	repo := &warmGatedRepo{
		stubPostRepo: &stubPostRepo{posts: makePosts(3)},
		entered:      make(chan struct{}),
		release:      make(chan struct{}),
	}
	close(repo.release) // never block; we want rebuilds to complete
	fc, rdb := newTestFeedCache(t, repo)

	_, _ = fc.GetFeed(context.Background(), "", 10)
	waitForWarm(t, rdb)

	// Drop the cache so the next read misses again.
	if err := rdb.Del(context.Background(), feedKey).Err(); err != nil {
		t.Fatalf("Del: %v", err)
	}

	_, _ = fc.GetFeed(context.Background(), "", 10)
	waitForWarm(t, rdb)

	if got := repo.warmCalls.Load(); got < 2 {
		t.Errorf("rebuilds after two separate misses = %d, want at least 2 (the guard latched)", got)
	}
}

func TestGetFeed_TTLIsSet(t *testing.T) {
	repo := &stubPostRepo{posts: makePosts(2)}
	fc, rdb := newTestFeedCache(t, repo)

	// Warm the cache.
	_, _ = fc.GetFeed(context.Background(), "", 10)
	waitForWarm(t, rdb)

	ttl, err := rdb.TTL(context.Background(), feedKey).Result()
	if err != nil {
		t.Fatalf("TTL: %v", err)
	}
	if ttl <= 0 {
		t.Errorf("feed key TTL = %v, want > 0", ttl)
	}
	if ttl > feedTTL {
		t.Errorf("feed key TTL = %v, want <= %v", ttl, feedTTL)
	}
}
