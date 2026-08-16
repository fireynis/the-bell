package cache

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/redis/go-redis/v9"
)

const (
	feedKey = "feed:latest"
	feedTTL = 60 * time.Second

	// feedGenKey counts writes to the feed. Every invalidation bumps it before
	// touching feedKey, and a rebuild refuses to publish a snapshot taken
	// before the value it sees at publish time — see warmCache.
	//
	// It is deliberately left without a TTL. Losing it (eviction, FLUSHDB) is
	// safe in the direction that matters: a rebuild comparing a remembered
	// counter against a missing one sees a mismatch and declines to publish.
	feedGenKey = "feed:latest:gen"

	feedMaxLen = 100
)

// FeedSource is the one thing the cache needs from post storage: a page of the
// feed to fall back to and to rebuild from. It never creates, updates or
// removes a post.
//
// This is declared here, on the consumer side, rather than reusing
// service.PostRepository — the cache took all six of that interface's methods
// to call one of them, which said the cache could write posts when it cannot.
// *postgres.PostRepo satisfies both, so nothing at the wiring site changes.
// Same principle as TrustScoreUpdater in trust_worker.go.
type FeedSource interface {
	ListPosts(ctx context.Context, cursor string, limit int) ([]*domain.Post, error)
}

// FeedCache is a read-through cache for the post feed backed by a Redis
// sorted set. On miss it falls back to the FeedSource and populates
// the cache. Writes (create/delete) keep the sorted set consistent so
// subsequent reads are served from Redis.
type FeedCache struct {
	rdb    redis.Cmdable
	repo   FeedSource
	logger *slog.Logger

	// warming is held for the duration of a background rebuild so that a
	// burst of concurrent misses triggers one rebuild rather than one each.
	warming atomic.Bool
}

// NewFeedCache creates a FeedCache.
func NewFeedCache(rdb redis.Cmdable, repo FeedSource, logger *slog.Logger) *FeedCache {
	return &FeedCache{rdb: rdb, repo: repo, logger: logger}
}

// GetFeed returns up to `limit` visible posts, optionally starting after
// `cursor` (a post ID). It tries to serve from the Redis sorted set first;
// on cache miss it falls back to Postgres.
func (c *FeedCache) GetFeed(ctx context.Context, cursor string, limit int) ([]*domain.Post, error) {
	// Only serve first-page (no cursor) requests from the cache.
	// Cursor-based pagination always goes to Postgres to keep things simple.
	if cursor != "" {
		return c.repo.ListPosts(ctx, cursor, limit)
	}

	// A limit beyond what the cache is allowed to hold can never be satisfied
	// from it: a full sorted set would still be a short page. Nothing asks for
	// this today (the handler caps at feedMaxLen), but serving it from the
	// cache would truncate the feed the moment that cap moved.
	if limit > feedMaxLen {
		return c.repo.ListPosts(ctx, "", limit)
	}

	// A non-empty sorted set is authoritative for a first page of at most
	// feedMaxLen: only warmCache creates the key; it writes the newest
	// feedMaxLen posts straight from the source of truth, and only if the feed
	// did not change while it was reading them. See InvalidateOnCreate for why
	// appends must never create it, and warmCache for the generation check.
	posts, err := c.getFromRedis(ctx, limit)
	if err == nil && len(posts) > 0 {
		return posts, nil
	}
	if err != nil {
		c.logger.Warn("feed cache miss", "error", err)
	}

	// Fall back to Postgres
	posts, err = c.repo.ListPosts(ctx, "", limit)
	if err != nil {
		return nil, err
	}

	// Warm cache in the background so the current request isn't delayed.
	c.warmCacheOnce(context.WithoutCancel(ctx))

	return posts, nil
}

// warmCacheOnce starts a background rebuild unless one is already running.
//
// Every concurrent miss used to launch its own rebuild, so a cold cache under
// load fanned out into one full ListPosts(feedMaxLen) per in-flight request —
// hitting Postgres hardest exactly when it could least absorb it. Callers that
// lose the race simply skip: the winner's rebuild is the one they would have
// performed, and they have already served their own read from Postgres.
func (c *FeedCache) warmCacheOnce(ctx context.Context) {
	if !c.warming.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer c.warming.Store(false)
		c.warmCache(ctx)
	}()
}

// appendPostScript bumps the generation, then adds one post to an *existing*
// feed sorted set, trims it to the newest feedMaxLen entries and refreshes the
// TTL. Past the generation bump it does nothing at all when the key is absent —
// see InvalidateOnCreate.
//
// The generation bump comes first, and unconditionally, so that it happens even
// on the cold-cache path where the append itself is skipped: an in-flight
// rebuild whose snapshot predates this post must be stopped from publishing
// whether or not there is a warm cache to append to.
//
// The existence check has to happen inside the script rather than as a separate
// EXISTS: between two round trips the key can expire, and losing that race
// recreates exactly the single-member feed this guard exists to prevent.
//
// KEYS[1] feed key, KEYS[2] generation key.
// ARGV: score, member, trim bound, TTL in seconds.
var appendPostScript = redis.NewScript(`
redis.call('INCR', KEYS[2])
if redis.call('EXISTS', KEYS[1]) == 0 then
	return 0
end
redis.call('ZADD', KEYS[1], ARGV[1], ARGV[2])
redis.call('ZREMRANGEBYRANK', KEYS[1], 0, ARGV[3])
redis.call('EXPIRE', KEYS[1], ARGV[4])
return 1
`)

// InvalidateOnCreate adds the new post to the sorted set and trims it to
// feedMaxLen entries, keeping the cache fresh without a full rebuild.
//
// It appends only to a cache that is already warm, and never creates the key.
// An append to a missing key was the one way the feed could hold a set of posts
// that was neither the newest feedMaxLen nor the whole feed: a clear
// (InvalidateOnUpdate, InvalidateOnDelete, or the TTL) followed by one post
// creation left feed:latest holding that single post with a fresh 60s TTL, and
// GetFeed cannot distinguish it from a town that genuinely has one post — so it
// served a one-post feed, hasMore:false and all, until the TTL ran out. Skipping
// the append instead costs nothing: the key's absence is already the signal for
// the next read to rebuild from Postgres, which picks up this post anyway.
//
// Skipping the append is only free because of the generation bump that goes
// with it. A rebuild already in flight is holding a snapshot that predates this
// post, and its own DEL would erase an append even if one had landed; the bump
// is what makes that rebuild throw its snapshot away instead of publishing it.
func (c *FeedCache) InvalidateOnCreate(ctx context.Context, post *domain.Post) {
	data, err := json.Marshal(post)
	if err != nil {
		c.logger.Error("feed cache: marshal post", "error", err)
		return
	}

	// Score = Unix timestamp in milliseconds, matching warmCache. The trim
	// removes by ascending rank, so 0..(-(feedMaxLen+1)) drops everything
	// except the top (newest) feedMaxLen entries.
	err = appendPostScript.Run(ctx, c.rdb, []string{feedKey, feedGenKey},
		post.CreatedAt.UnixMilli(), string(data), -feedMaxLen-1, int64(feedTTL.Seconds()),
	).Err()
	if err != nil {
		c.logger.Error("feed cache: invalidate on create", "error", err)
	}
}

// InvalidateOnUpdate clears the feed after a post's body changed.
//
// Members of the sorted set are the marshalled posts themselves, so an edited
// post no longer matches the member holding its pre-edit body: adding the new
// JSON would leave the stale member in place and serve the post twice. Removing
// the old member instead would mean unmarshalling every member to find it, and
// doing so outside a transaction.
//
// Writing the post back is doubly wrong here: UpdatePostBody returns only the
// posts row, with no users join, so its author fields are empty — caching it
// would serve the post with no author name, the bug PostAuthor exists to avoid.
//
// A full clear is the same trade InvalidateOnDelete makes — the next read
// rebuilds from Postgres, and edits are rare (author-only, 15-minute window).
func (c *FeedCache) InvalidateOnUpdate(ctx context.Context, post *domain.Post) {
	if err := c.clearFeed(ctx); err != nil {
		c.logger.Error("feed cache: invalidate on update", "error", err)
	}
}

// InvalidateOnDelete drops the cached feed after a post was removed. We do a
// full clear since scanning members for a matching ID is fragile.
func (c *FeedCache) InvalidateOnDelete(ctx context.Context, postID string) {
	if err := c.clearFeed(ctx); err != nil {
		c.logger.Error("feed cache: invalidate on delete", "error", err)
	}
}

// clearFeed drops the cached feed and bumps the generation.
//
// Deleting the key is not enough on its own. A rebuild that snapshotted
// Postgres before this call is about to write that snapshot — which still
// contains the edited or removed post — over the top of the deletion, and hand
// it a fresh TTL. That is the same in-flight-rebuild window InvalidateOnCreate
// describes, and it is worse on this side: it puts a moderator-removed post
// back in front of the whole town. The bump is what makes the rebuild discard
// its snapshot.
//
// MULTI/EXEC so a rebuild cannot observe the deletion without the bump and
// conclude nothing has changed.
func (c *FeedCache) clearFeed(ctx context.Context) error {
	pipe := c.rdb.TxPipeline()
	pipe.Incr(ctx, feedGenKey)
	pipe.Del(ctx, feedKey)
	_, err := pipe.Exec(ctx)
	return err
}

// swapFeedScript replaces the feed sorted set with a freshly read snapshot,
// but only if the generation still matches the one the caller read before
// taking that snapshot. A mismatch means the feed was written to while the
// snapshot was in flight, so the snapshot is stale and is dropped on the floor.
//
// One script rather than a MULTI/EXEC because the check and the swap have to be
// atomic with each other: reading the generation in one round trip and swapping
// in the next reopens the same window one command later, and a transaction
// cannot branch on what it reads. Everything inside a script is atomic too, so
// the key is still either the old feed or the new one and never nothing —
// which is what keeps a concurrent append from writing to a missing key.
//
// KEYS[1] feed key, KEYS[2] generation key.
// ARGV[1] expected generation, ARGV[2] TTL in seconds, then score/member pairs.
var swapFeedScript = redis.NewScript(`
if (redis.call('GET', KEYS[2]) or '0') ~= ARGV[1] then
	return 0
end
redis.call('DEL', KEYS[1])
for i = 3, #ARGV, 2 do
	redis.call('ZADD', KEYS[1], ARGV[i], ARGV[i + 1])
end
redis.call('EXPIRE', KEYS[1], ARGV[2])
return 1
`)

// warmCache loads the latest feedMaxLen posts from Postgres and writes them to
// the Redis sorted set with a TTL — unless the feed changed while it was
// reading, in which case it publishes nothing and leaves the rebuild to the
// next read.
//
// Reading the generation *before* the snapshot is the whole mechanism: any
// create, edit or removal that lands after this point bumps the counter, and
// the swap below runs only while the counter is still where it was. Without it
// a post created between the snapshot and the write was simply lost — the
// snapshot predated it, InvalidateOnCreate declined to append to a key that did
// not exist yet, and the stale page then held a fresh 60s TTL and served as
// authoritative. Declining costs one Postgres read on the next request; the
// absent key is already the signal to rebuild.
func (c *FeedCache) warmCache(ctx context.Context) {
	gen, err := c.currentGeneration(ctx)
	if err != nil {
		// Unverifiable is not the same as unchanged. Serving from Postgres is
		// the safe degradation; publishing a snapshot we cannot vouch for is
		// not.
		c.logger.Error("feed cache: reading generation before warm", "error", err)
		return
	}

	posts, err := c.repo.ListPosts(ctx, "", feedMaxLen)
	if err != nil {
		c.logger.Error("feed cache: warm failed", "error", err)
		return
	}

	if len(posts) == 0 {
		return
	}

	args := make([]any, 0, 2+2*len(posts))
	args = append(args, gen, int64(feedTTL.Seconds()))
	for _, p := range posts {
		data, err := json.Marshal(p)
		if err != nil {
			c.logger.Error("feed cache: marshal post during warm", "error", err)
			return
		}
		args = append(args, p.CreatedAt.UnixMilli(), string(data))
	}

	published, err := swapFeedScript.Run(ctx, c.rdb, []string{feedKey, feedGenKey}, args...).Int()
	if err != nil {
		c.logger.Error("feed cache: warm swap failed", "error", err)
		return
	}
	if published == 0 {
		c.logger.Debug("feed cache: discarded a warm snapshot the feed outran")
	}
}

// currentGeneration reads the feed's write counter, treating an absent key as
// generation zero exactly as swapFeedScript does.
func (c *FeedCache) currentGeneration(ctx context.Context) (string, error) {
	gen, err := c.rdb.Get(ctx, feedGenKey).Result()
	if errors.Is(err, redis.Nil) {
		return "0", nil
	}
	return gen, err
}

// getFromRedis retrieves the latest `limit` posts from the sorted set.
func (c *FeedCache) getFromRedis(ctx context.Context, limit int) ([]*domain.Post, error) {
	// ZRevRangeByScore returns members with highest score first (newest).
	results, err := c.rdb.ZRevRange(ctx, feedKey, 0, int64(limit-1)).Result()
	if err != nil {
		return nil, err
	}

	if len(results) == 0 {
		return nil, nil
	}

	posts := make([]*domain.Post, 0, len(results))
	for _, raw := range results {
		var p domain.Post
		if err := json.Unmarshal([]byte(raw), &p); err != nil {
			return nil, err
		}
		posts = append(posts, &p)
	}
	return posts, nil
}
