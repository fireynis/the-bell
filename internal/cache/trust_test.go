package cache

import (
	"context"
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/testsupport"
	"github.com/redis/go-redis/v9"
)

// redisAvailable returns a client for a Redis logical database isolated to the
// calling test, backed by the container in internal/testsupport.
//
// It previously dialled a hardcoded localhost:6379 DB 15 and skipped the test
// when nothing answered, which made these tests both unsound and invisible: any
// other test binary running at the same time shared that one database, so a
// user ID enqueued by one process was dequeued by another, and on a machine
// with no local Redis every assertion here silently vanished.
func redisAvailable(t *testing.T) *redis.Client {
	t.Helper()
	return testsupport.TestRedis(t)
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
