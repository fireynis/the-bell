package cache

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/service"
	"github.com/redis/go-redis/v9"
)

const (
	feedKey    = "feed:latest"
	feedTTL    = 60 * time.Second
	feedMaxLen = 100
)

// FeedCache is a read-through cache for the post feed backed by a Redis
// sorted set. On miss it falls back to the PostRepository and populates
// the cache. Writes (create/delete) keep the sorted set consistent so
// subsequent reads are served from Redis.
type FeedCache struct {
	rdb    redis.Cmdable
	repo   service.PostRepository
	logger *slog.Logger
}

// NewFeedCache creates a FeedCache.
func NewFeedCache(rdb redis.Cmdable, repo service.PostRepository, logger *slog.Logger) *FeedCache {
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
	go c.warmCache(context.WithoutCancel(ctx))

	return posts, nil
}

// InvalidateOnCreate adds the new post to the sorted set and trims it to
// feedMaxLen entries, keeping the cache fresh without a full rebuild.
func (c *FeedCache) InvalidateOnCreate(ctx context.Context, post *domain.Post) {
	data, err := json.Marshal(post)
	if err != nil {
		c.logger.Error("feed cache: marshal post", "error", err)
		return
	}

	pipe := c.rdb.Pipeline()

	// Score = Unix timestamp in seconds (float64 for sub-second if needed).
	score := float64(post.CreatedAt.UnixMilli())
	pipe.ZAdd(ctx, feedKey, redis.Z{Score: score, Member: string(data)})

	// Trim to keep only the latest feedMaxLen entries (highest scores).
	// ZRemRangeByRank removes by ascending rank, so 0..(-(feedMaxLen+1))
	// removes everything except the top feedMaxLen.
	pipe.ZRemRangeByRank(ctx, feedKey, 0, int64(-feedMaxLen-1))

	// Refresh TTL
	pipe.Expire(ctx, feedKey, feedTTL)

	if _, err := pipe.Exec(ctx); err != nil {
		c.logger.Error("feed cache: invalidate on create", "error", err)
	}
}

// InvalidateOnDelete removes any entry whose JSON contains the given
// post ID. Because post IDs are UUIDs the substring match is safe.
// We do a full clear since scanning members for a matching ID is fragile.
func (c *FeedCache) InvalidateOnDelete(ctx context.Context, postID string) {
	if err := c.rdb.Del(ctx, feedKey).Err(); err != nil {
		c.logger.Error("feed cache: invalidate on delete", "error", err)
	}
}

// warmCache loads the latest feedMaxLen posts from Postgres and writes
// them to the Redis sorted set with a TTL.
func (c *FeedCache) warmCache(ctx context.Context) {
	posts, err := c.repo.ListPosts(ctx, "", feedMaxLen)
	if err != nil {
		c.logger.Error("feed cache: warm failed", "error", err)
		return
	}

	if len(posts) == 0 {
		return
	}

	members := make([]redis.Z, 0, len(posts))
	for _, p := range posts {
		data, err := json.Marshal(p)
		if err != nil {
			c.logger.Error("feed cache: marshal post during warm", "error", err)
			return
		}
		members = append(members, redis.Z{
			Score:  float64(p.CreatedAt.UnixMilli()),
			Member: string(data),
		})
	}

	pipe := c.rdb.Pipeline()
	pipe.Del(ctx, feedKey)
	pipe.ZAdd(ctx, feedKey, members...)
	pipe.Expire(ctx, feedKey, feedTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		c.logger.Error("feed cache: warm pipeline failed", "error", err)
	}
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
