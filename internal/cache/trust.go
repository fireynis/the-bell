package cache

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	trustKeyPrefix = "trust:score:"
	recalcQueue    = "trust:recalc"
	defaultTTL     = 5 * time.Minute
)

// TrustCache provides Redis-backed caching for user trust scores.
type TrustCache struct {
	rdb *redis.Client
	ttl time.Duration
}

// NewTrustCache creates a TrustCache backed by the given Redis client.
func NewTrustCache(rdb *redis.Client) *TrustCache {
	return &TrustCache{
		rdb: rdb,
		ttl: defaultTTL,
	}
}

func trustKey(userID string) string {
	return trustKeyPrefix + userID
}

// GetTrustScore retrieves a cached trust score for the given user.
// Returns the score and true if found, or 0 and false on cache miss.
func (c *TrustCache) GetTrustScore(ctx context.Context, userID string) (float64, bool) {
	val, err := c.rdb.Get(ctx, trustKey(userID)).Result()
	if err != nil {
		return 0, false
	}
	score, err := strconv.ParseFloat(val, 64)
	if err != nil {
		return 0, false
	}
	return score, true
}

// SetTrustScore caches a trust score for the given user with the default TTL.
func (c *TrustCache) SetTrustScore(ctx context.Context, userID string, score float64) error {
	val := strconv.FormatFloat(score, 'f', 6, 64)
	return c.rdb.Set(ctx, trustKey(userID), val, c.ttl).Err()
}

// InvalidateUser removes the cached trust score for the given user.
func (c *TrustCache) InvalidateUser(ctx context.Context, userID string) error {
	return c.rdb.Del(ctx, trustKey(userID)).Err()
}

// EnqueueRecalc pushes a user ID onto the recalculation job queue.
// Duplicate entries are acceptable; the worker will deduplicate via cache.
func (c *TrustCache) EnqueueRecalc(ctx context.Context, userID string) error {
	return c.rdb.RPush(ctx, recalcQueue, userID).Err()
}

// DequeueRecalc blocks until a user ID is available on the recalculation queue
// or the context is cancelled. Returns the user ID or an error.
func (c *TrustCache) DequeueRecalc(ctx context.Context, timeout time.Duration) (string, error) {
	result, err := c.rdb.BLPop(ctx, timeout, recalcQueue).Result()
	if err != nil {
		return "", fmt.Errorf("dequeue recalc: %w", err)
	}
	// BLPop returns [key, value]
	if len(result) < 2 {
		return "", fmt.Errorf("dequeue recalc: unexpected result length %d", len(result))
	}
	return result[1], nil
}
