package cache

import (
	"context"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// redisAvailable returns a connected redis client if Redis is reachable,
// otherwise it skips the test.
func redisAvailable(t *testing.T) *redis.Client {
	t.Helper()
	rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379", DB: 15})
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skipf("redis not available: %v", err)
	}
	t.Cleanup(func() { rdb.FlushDB(context.Background()); rdb.Close() })
	return rdb
}

func TestTrustCache_GetSet(t *testing.T) {
	rdb := redisAvailable(t)
	tc := NewTrustCache(rdb)
	ctx := context.Background()

	// Cache miss
	_, ok := tc.GetTrustScore(ctx, "user-1")
	if ok {
		t.Fatal("expected cache miss")
	}

	// Set + hit
	if err := tc.SetTrustScore(ctx, "user-1", 85.5); err != nil {
		t.Fatalf("set: %v", err)
	}
	score, ok := tc.GetTrustScore(ctx, "user-1")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if score < 85.499999 || score > 85.500001 {
		t.Fatalf("expected ~85.5, got %f", score)
	}
}

func TestTrustCache_Invalidate(t *testing.T) {
	rdb := redisAvailable(t)
	tc := NewTrustCache(rdb)
	ctx := context.Background()

	if err := tc.SetTrustScore(ctx, "user-2", 70.0); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := tc.InvalidateUser(ctx, "user-2"); err != nil {
		t.Fatalf("invalidate: %v", err)
	}
	_, ok := tc.GetTrustScore(ctx, "user-2")
	if ok {
		t.Fatal("expected cache miss after invalidation")
	}
}

func TestTrustCache_EnqueueDequeue(t *testing.T) {
	rdb := redisAvailable(t)
	tc := NewTrustCache(rdb)
	ctx := context.Background()

	if err := tc.EnqueueRecalc(ctx, "user-3"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	userID, err := tc.DequeueRecalc(ctx, 1*time.Second)
	if err != nil {
		t.Fatalf("dequeue: %v", err)
	}
	if userID != "user-3" {
		t.Fatalf("expected user-3, got %s", userID)
	}
}
