package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/service"
	"github.com/fireynis/the-bell/internal/testsupport"
	"github.com/redis/go-redis/v9"
)

// stubPostRepo is a minimal in-memory FeedSource for testing the cache.
//
// It holds the two methods the cache and these tests actually exercise:
// ListPosts, which is the whole of FeedSource, and UpdatePostBody, which one
// test calls directly to produce an edited post. It used to carry all six
// methods of service.PostRepository purely to satisfy a parameter type.
type stubPostRepo struct {
	posts []*domain.Post
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

// UpdatePostBody is not part of FeedSource; it stands in for the edit that
// TestInvalidateOnUpdate_FeedServesNeitherStaleBodyNorDuplicate invalidates on.
func (s *stubPostRepo) UpdatePostBody(_ context.Context, id string, body string) (*domain.Post, error) {
	for _, p := range s.posts {
		if p.ID == id {
			p.Body = body
			return p, nil
		}
	}
	return nil, service.ErrNotFound
}

// The production wiring hands the cache a *postgres.PostRepo, which satisfies
// both this and the full service.PostRepository, so narrowing the parameter
// cannot drift away from what production actually passes.
var _ FeedSource = (*stubPostRepo)(nil)

// newTestFeedCache builds a FeedCache over a real Redis.
//
// This previously ran against miniredis, an in-process reimplementation of
// Redis. The behaviour these tests turn on — sorted-set rank trimming, TTL and
// pipelining — is exactly what a reimplementation is liable to get subtly
// wrong, so asserting against it proved nothing about production.
func newTestFeedCache(t *testing.T, repo FeedSource) (*FeedCache, *redis.Client) {
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

// waitForWarmToFinish blocks until the background rebuild has run to
// completion. Unlike waitForWarm it does not assume the rebuild publishes
// anything: a rebuild that discovers the feed changed underneath it leaves the
// key absent on purpose, and a test for that has to be able to wait for it.
//
// warmCacheOnce takes the warming flag synchronously, before it spawns the
// goroutine, so by the time the GetFeed that triggered a rebuild has returned
// the flag is already set and this cannot observe a false "finished".
func waitForWarmToFinish(t *testing.T, fc *FeedCache) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !fc.warming.Load() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out waiting for the background rebuild to finish")
}

func postIDs(posts []*domain.Post) []string {
	ids := make([]string, len(posts))
	for i, p := range posts {
		ids[i] = p.ID
	}
	return ids
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

// A full clear followed by a single create used to poison the feed: the create
// re-made feed:latest holding exactly one member with a fresh TTL, and the next
// first-page read served that one post as if it were the whole town's feed —
// short enough that the handler also reported hasMore:false. The cache must
// only be authoritative when it was built from the source of truth.
func TestGetFeed_CreateAfterClearDoesNotTruncateFeed(t *testing.T) {
	const limit = 20
	ctx := context.Background()
	repo := &stubPostRepo{posts: makePosts(51)}
	fc, rdb := newTestFeedCache(t, repo)

	// Warm the cache, then clear it the way an edit or a delete does.
	_, _ = fc.GetFeed(ctx, "", limit)
	waitForWarm(t, rdb)
	fc.InvalidateOnDelete(ctx, postID(0))

	// One post is created before any read has rebuilt the cache.
	newPost := &domain.Post{
		ID:        "post-new",
		AuthorID:  "user-1",
		Body:      "brand new",
		Status:    domain.PostVisible,
		CreatedAt: time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC),
	}
	repo.posts = append(repo.posts, newPost)
	fc.InvalidateOnCreate(ctx, newPost)

	posts, err := fc.GetFeed(ctx, "", limit)
	if err != nil {
		t.Fatalf("GetFeed() error = %v", err)
	}
	if len(posts) != limit {
		t.Fatalf("GetFeed() returned %d posts, want %d (the cache served a partial feed as the whole feed)", len(posts), limit)
	}
	if posts[0].ID != newPost.ID {
		t.Errorf("first post = %q, want %q", posts[0].ID, newPost.ID)
	}
}

// InvalidateOnCreate must never be the command that creates feed:latest: a key
// built by an append holds one post but claims to be the feed. Only the warm
// path, which reads from the source of truth, may create it.
func TestInvalidateOnCreate_DoesNotSeedAnAbsentCache(t *testing.T) {
	ctx := context.Background()
	repo := &stubPostRepo{posts: makePosts(3)}
	fc, rdb := newTestFeedCache(t, repo)

	newPost := &domain.Post{
		ID:        "post-new",
		AuthorID:  "user-1",
		Body:      "brand new",
		Status:    domain.PostVisible,
		CreatedAt: time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC),
	}
	fc.InvalidateOnCreate(ctx, newPost)

	exists, err := rdb.Exists(ctx, feedKey).Result()
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if exists != 0 {
		t.Errorf("InvalidateOnCreate created %s on a cold cache; only a full rebuild may create it", feedKey)
	}
}

// Appending to a warm cache must still push the expiry out; the whole point of
// serving a new post from Redis is lost if the key dies moments later.
func TestInvalidateOnCreate_RefreshesTTLOfAWarmCache(t *testing.T) {
	ctx := context.Background()
	repo := &stubPostRepo{posts: makePosts(3)}
	fc, rdb := newTestFeedCache(t, repo)

	_, _ = fc.GetFeed(ctx, "", 10)
	waitForWarm(t, rdb)

	// Age the key so a refresh is visible.
	if err := rdb.Expire(ctx, feedKey, 2*time.Second).Err(); err != nil {
		t.Fatalf("Expire: %v", err)
	}

	fc.InvalidateOnCreate(ctx, &domain.Post{
		ID:        "post-new",
		AuthorID:  "user-1",
		Body:      "brand new",
		Status:    domain.PostVisible,
		CreatedAt: time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC),
	})

	ttl, err := rdb.TTL(ctx, feedKey).Result()
	if err != nil {
		t.Fatalf("TTL: %v", err)
	}
	if ttl <= 2*time.Second || ttl > feedTTL {
		t.Errorf("feed key TTL after create = %v, want (2s, %v]", ttl, feedTTL)
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

// writeDuringWarmRepo reproduces the interleaving that loses a write: the warm
// path reads its snapshot of Postgres, and only then does the write land and
// tell the cache about itself. warmCache is the only caller that asks for
// feedMaxLen posts, so the limit identifies it without the repo having to know
// who called.
//
// The snapshot the warm path is holding is already out of date by the time it
// reaches Redis, which is the whole point: a rebuild must never publish one.
type writeDuringWarmRepo struct {
	*stubPostRepo
	write func(ctx context.Context, s *stubPostRepo)
	once  sync.Once
}

func (r *writeDuringWarmRepo) ListPosts(ctx context.Context, cursor string, limit int) ([]*domain.Post, error) {
	posts, err := r.stubPostRepo.ListPosts(ctx, cursor, limit)
	if err != nil || limit != feedMaxLen {
		return posts, err
	}
	r.once.Do(func() { r.write(ctx, r.stubPostRepo) })
	return posts, err
}

// A post created between warmCache's snapshot and its write to Redis used to
// vanish for a full TTL. InvalidateOnCreate cannot save it — it declines to
// write while the key is absent, and an append that did land would be wiped by
// the rebuild's own DEL — so the rebuild published a snapshot that predated the
// post, with a fresh 60s TTL, and GetFeed served that as the authoritative
// first page. The author reloaded and their own post was not there.
func TestGetFeed_PostCreatedWhileWarmingIsNotLostBehindAStaleCache(t *testing.T) {
	ctx := context.Background()
	newPost := &domain.Post{
		ID:        "post-new",
		AuthorID:  "user-1",
		Body:      "posted while the cache was rebuilding",
		Status:    domain.PostVisible,
		CreatedAt: time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC),
	}

	repo := &writeDuringWarmRepo{stubPostRepo: &stubPostRepo{posts: makePosts(3)}}
	fc, rdb := newTestFeedCache(t, repo)
	repo.write = func(ctx context.Context, s *stubPostRepo) {
		s.posts = append(s.posts, newPost)
		fc.InvalidateOnCreate(ctx, newPost)
	}

	// A cold read serves from Postgres and starts the rebuild the create races.
	if _, err := fc.GetFeed(ctx, "", 10); err != nil {
		t.Fatalf("GetFeed() error = %v", err)
	}
	waitForWarmToFinish(t, fc)

	// Leaving the key absent is a fine outcome — the next read rebuilds. What is
	// not fine is a populated key that predates the post, because GetFeed treats
	// any non-empty key as the authoritative first page until the TTL runs out.
	if cached := feedMembers(t, rdb); len(cached) > 0 && !slices.Contains(postIDs(cached), newPost.ID) {
		t.Errorf("the rebuild published %v, which is missing %s; a stale snapshot is now authoritative for %v",
			postIDs(cached), newPost.ID, feedTTL)
	}

	posts, err := fc.GetFeed(ctx, "", 10)
	if err != nil {
		t.Fatalf("GetFeed() after the rebuild error = %v", err)
	}
	if !slices.Contains(postIDs(posts), newPost.ID) {
		t.Errorf("feed = %v, want it to contain %s, created while the cache was rebuilding",
			postIDs(posts), newPost.ID)
	}
}

// The mirror image, and the more serious one: a post removed between the
// snapshot and the write. InvalidateOnDelete clears a key that is not there
// yet, and the rebuild then republishes the removed post — so a moderator's
// removal is undone by a rebuild that was already in flight, and the town keeps
// reading the post for another TTL.
func TestGetFeed_PostRemovedWhileWarmingIsNotRepublished(t *testing.T) {
	ctx := context.Background()
	removed := postID(2)

	repo := &writeDuringWarmRepo{stubPostRepo: &stubPostRepo{posts: makePosts(3)}}
	fc, rdb := newTestFeedCache(t, repo)
	repo.write = func(ctx context.Context, s *stubPostRepo) {
		s.posts = slices.DeleteFunc(s.posts, func(p *domain.Post) bool { return p.ID == removed })
		fc.InvalidateOnDelete(ctx, removed)
	}

	if _, err := fc.GetFeed(ctx, "", 10); err != nil {
		t.Fatalf("GetFeed() error = %v", err)
	}
	waitForWarmToFinish(t, fc)

	if cached := feedMembers(t, rdb); slices.Contains(postIDs(cached), removed) {
		t.Errorf("the rebuild republished removed post %s: cache holds %v", removed, postIDs(cached))
	}

	posts, err := fc.GetFeed(ctx, "", 10)
	if err != nil {
		t.Fatalf("GetFeed() after the rebuild error = %v", err)
	}
	if slices.Contains(postIDs(posts), removed) {
		t.Errorf("feed = %v, want removed post %s gone", postIDs(posts), removed)
	}
}

// genReadFailsRedis is a real Redis with one fault injected: reading the feed's
// generation counter fails. Everything else — including the writes the rebuild
// would perform — still works, which is what makes the assertion mean
// something.
type genReadFailsRedis struct {
	redis.Cmdable
	err error
}

func (r genReadFailsRedis) Get(ctx context.Context, key string) *redis.StringCmd {
	if key == feedGenKey {
		cmd := redis.NewStringCmd(ctx, "get", key)
		cmd.SetErr(r.err)
		return cmd
	}
	return r.Cmdable.Get(ctx, key)
}

// A rebuild that cannot read the generation cannot know whether its snapshot is
// current, and an unverifiable snapshot must not be published: the degradation
// for a broken Redis is "no cache, Postgres serves", never "a cache that might
// be wrong". Publishing anyway would be worse than the bug the generation check
// was added to fix, because the snapshot gets a fresh TTL either way.
func TestWarmCache_UnreadableGenerationPublishesNothing(t *testing.T) {
	ctx := context.Background()
	repo := &stubPostRepo{posts: makePosts(3)}
	rdb := testsupport.TestRedis(t)
	fc := NewFeedCache(genReadFailsRedis{Cmdable: rdb, err: errors.New("redis is having a day")}, repo, testLogger())

	posts, err := fc.GetFeed(ctx, "", 10)
	if err != nil {
		t.Fatalf("GetFeed() error = %v", err)
	}
	if len(posts) != 3 {
		t.Errorf("GetFeed() returned %d posts, want 3 from Postgres", len(posts))
	}
	waitForWarmToFinish(t, fc)

	exists, err := rdb.Exists(ctx, feedKey).Result()
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if exists != 0 {
		t.Errorf("the rebuild published %v without being able to verify it was current",
			postIDs(feedMembers(t, rdb)))
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
