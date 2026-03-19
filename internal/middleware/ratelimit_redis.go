package middleware

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisRateLimiterClient implements RateLimiterClient using a Redis sorted set
// for sliding-window rate limiting.
type RedisRateLimiterClient struct {
	rdb *redis.Client
}

// NewRedisRateLimiterClient wraps a go-redis client for use as a rate limiter backend.
func NewRedisRateLimiterClient(rdb *redis.Client) *RedisRateLimiterClient {
	return &RedisRateLimiterClient{rdb: rdb}
}

// slidingWindowScript atomically:
//  1. Removes entries outside the window (ZREMRANGEBYSCORE)
//  2. Adds the current timestamp (ZADD)
//  3. Counts remaining entries (ZCARD)
//  4. Sets TTL on the key (PEXPIRE)
//
// Returns the count after the add.
var slidingWindowScript = redis.NewScript(`
local key = KEYS[1]
local window_start = tonumber(ARGV[1])
local now = tonumber(ARGV[2])
local ttl_ms = tonumber(ARGV[3])

redis.call('ZREMRANGEBYSCORE', key, '-inf', window_start)
redis.call('ZADD', key, now, now .. '-' .. math.random(1000000))
local count = redis.call('ZCARD', key)
redis.call('PEXPIRE', key, ttl_ms)
return count
`)

// SlidingWindowCount implements RateLimiterClient.
func (c *RedisRateLimiterClient) SlidingWindowCount(ctx context.Context, key string, now time.Time, window time.Duration) (int64, error) {
	nowMicro := float64(now.UnixMicro())
	windowStart := float64(now.Add(-window).UnixMicro())
	ttlMs := int64(window.Milliseconds())

	result, err := slidingWindowScript.Run(ctx, c.rdb, []string{key},
		windowStart, nowMicro, ttlMs,
	).Int64()
	if err != nil {
		return 0, err
	}
	return result, nil
}
