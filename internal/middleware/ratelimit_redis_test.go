//go:build integration

package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/middleware"
	"github.com/fireynis/the-bell/internal/testsupport"
)

// These tests exercise the real Lua sliding-window script in
// ratelimit_redis.go against a real Redis. The rest of the rate limiter's
// tests substitute an in-process Go reimplementation of the window, so they
// assert that stand-in's semantics rather than the script's; nothing here
// depends on it.

// TestRedisRateLimiter_AllowsUnderLimitThenDenies drives the middleware
// end-to-end so the script decides the outcome of real HTTP requests.
func TestRedisRateLimiter_AllowsUnderLimitThenDenies(t *testing.T) {
	rdb := testsupport.TestRedis(t)
	rl := middleware.NewRateLimiter(middleware.NewRedisRateLimiterClient(rdb), testLogger())

	const limit = 5
	user := &domain.User{ID: "user-under-limit", Role: domain.RoleMember, IsActive: true}
	handler := rl.Limit("posts", limit, time.Hour)(okHandler())

	do := func() int {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/posts", nil)
		req = req.WithContext(middleware.WithUser(req.Context(), user))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code
	}

	for i := range limit {
		if got := do(); got != http.StatusOK {
			t.Fatalf("request %d: status = %d, want %d", i+1, got, http.StatusOK)
		}
	}

	if got := do(); got != http.StatusTooManyRequests {
		t.Errorf("request %d: status = %d, want %d", limit+1, got, http.StatusTooManyRequests)
	}
}

// TestRedisRateLimiter_EvictsEntriesOutsideWindow checks that the
// ZREMRANGEBYSCORE trim actually drops aged-out entries, so a caller who
// exhausts the limit recovers once the window passes.
//
// The clock is supplied as an argument rather than slept through, so this
// exercises the real eviction path without a real one-minute wait.
func TestRedisRateLimiter_EvictsEntriesOutsideWindow(t *testing.T) {
	rdb := testsupport.TestRedis(t)
	client := middleware.NewRedisRateLimiterClient(rdb)
	ctx := context.Background()

	const key = "ratelimit:evict"
	window := time.Minute
	start := time.Now()

	// Fill the window.
	for i := range 3 {
		count, err := client.SlidingWindowCount(ctx, key, start.Add(time.Duration(i)*time.Second), window)
		if err != nil {
			t.Fatalf("call %d: %v", i+1, err)
		}
		if want := int64(i + 1); count != want {
			t.Fatalf("call %d: count = %d, want %d", i+1, count, want)
		}
	}

	// Still inside the window: the earlier entries must all still count.
	count, err := client.SlidingWindowCount(ctx, key, start.Add(10*time.Second), window)
	if err != nil {
		t.Fatalf("inside window: %v", err)
	}
	if count != 4 {
		t.Errorf("inside window: count = %d, want 4", count)
	}

	// Past the window: called far enough ahead that windowStart (now-window)
	// sits after the newest existing entry at start+10s, so every earlier entry
	// must be evicted and only the call being made now remains.
	count, err = client.SlidingWindowCount(ctx, key, start.Add(window+11*time.Second), window)
	if err != nil {
		t.Fatalf("past window: %v", err)
	}
	if count != 1 {
		t.Errorf("past window: count = %d, want 1 (all prior entries should have been evicted)", count)
	}
}

// TestRedisRateLimiter_WindowBoundaryIsHalfOpen pins the edge semantics.
// ZREMRANGEBYSCORE '-inf' window_start is inclusive of window_start, so an
// entry landing exactly one window ago is evicted rather than retained. The
// window is therefore half-open, and a caller gets its next slot exactly one
// window after the call it is waiting on rather than one tick later.
func TestRedisRateLimiter_WindowBoundaryIsHalfOpen(t *testing.T) {
	rdb := testsupport.TestRedis(t)
	client := middleware.NewRedisRateLimiterClient(rdb)
	ctx := context.Background()

	const key = "ratelimit:boundary"
	window := time.Minute
	start := time.Now()

	if _, err := client.SlidingWindowCount(ctx, key, start, window); err != nil {
		t.Fatalf("first call: %v", err)
	}

	// Exactly one window later: the first entry's score equals windowStart and
	// is dropped by the inclusive range.
	count, err := client.SlidingWindowCount(ctx, key, start.Add(window), window)
	if err != nil {
		t.Fatalf("boundary call: %v", err)
	}
	if count != 1 {
		t.Errorf("at exactly one window later: count = %d, want 1 (entry at windowStart should be evicted)", count)
	}

	// One microsecond inside the window, the previous entry is retained.
	count, err = client.SlidingWindowCount(ctx, key, start.Add(window).Add(-time.Microsecond), window)
	if err != nil {
		t.Fatalf("inside boundary call: %v", err)
	}
	if count != 2 {
		t.Errorf("just inside the window: count = %d, want 2 (entry should still be retained)", count)
	}
}

// TestRedisRateLimiter_SetsKeyTTL guards against an unbounded key. Without the
// PEXPIRE every rate-limited user leaks a sorted set forever.
func TestRedisRateLimiter_SetsKeyTTL(t *testing.T) {
	rdb := testsupport.TestRedis(t)
	client := middleware.NewRedisRateLimiterClient(rdb)
	ctx := context.Background()

	const key = "ratelimit:ttl"
	window := 90 * time.Second

	if _, err := client.SlidingWindowCount(ctx, key, time.Now(), window); err != nil {
		t.Fatalf("SlidingWindowCount: %v", err)
	}

	ttl, err := rdb.PTTL(ctx, key).Result()
	if err != nil {
		t.Fatalf("PTTL: %v", err)
	}
	if ttl <= 0 {
		t.Fatalf("PTTL = %v, want > 0 (key has no expiry and will leak)", ttl)
	}
	if ttl > window {
		t.Errorf("PTTL = %v, want <= window %v", ttl, window)
	}
}

// TestRedisRateLimiter_ScoreAndTTLUnitsAgree pins the units on both sides of
// the script. The score and the window bound are microseconds; the PEXPIRE
// argument is milliseconds. If either drifts, windows come out 1000x wrong —
// a one-hour limit silently becoming 3.6 seconds or 41 days.
func TestRedisRateLimiter_ScoreAndTTLUnitsAgree(t *testing.T) {
	rdb := testsupport.TestRedis(t)
	client := middleware.NewRedisRateLimiterClient(rdb)
	ctx := context.Background()

	const key = "ratelimit:units"
	window := 2 * time.Second
	now := time.Now()

	if _, err := client.SlidingWindowCount(ctx, key, now, window); err != nil {
		t.Fatalf("SlidingWindowCount: %v", err)
	}

	// The single member's score must be the timestamp in microseconds.
	members, err := rdb.ZRangeWithScores(ctx, key, 0, -1).Result()
	if err != nil {
		t.Fatalf("ZRangeWithScores: %v", err)
	}
	if len(members) != 1 {
		t.Fatalf("got %d members, want 1", len(members))
	}
	if got, want := int64(members[0].Score), now.UnixMicro(); got != want {
		t.Errorf("score = %d, want %d (score is not in microseconds)", got, want)
	}

	// The TTL must be the same duration expressed in milliseconds.
	ttl, err := rdb.PTTL(ctx, key).Result()
	if err != nil {
		t.Fatalf("PTTL: %v", err)
	}
	if ttl > window || ttl < window-time.Second {
		t.Errorf("PTTL = %v, want ~%v (TTL is not the window in milliseconds)", ttl, window)
	}
}

// TestRedisRateLimiter_ConcurrentCallsCountExactly is the one that matters for
// correctness under load. The script's ZADD member is
// `now .. '-' .. math.random(1000000)`, and ZADD on an existing member updates
// its score instead of adding a row — so any duplicate member silently
// under-counts and lets the caller exceed its limit.
//
// Concurrent requests land on the same key with near-identical timestamps,
// which is exactly the case where members can repeat, so drive them
// concurrently and require the count to be exact.
func TestRedisRateLimiter_ConcurrentCallsCountExactly(t *testing.T) {
	rdb := testsupport.TestRedis(t)
	client := middleware.NewRedisRateLimiterClient(rdb)
	ctx := context.Background()

	const (
		key   = "ratelimit:concurrent"
		calls = 500
	)
	window := time.Hour

	var wg sync.WaitGroup
	errs := make(chan error, calls)
	for range calls {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := client.SlidingWindowCount(ctx, key, time.Now(), window); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("SlidingWindowCount: %v", err)
	}

	got, err := rdb.ZCard(ctx, key).Result()
	if err != nil {
		t.Fatalf("ZCard: %v", err)
	}
	if got != calls {
		t.Errorf("ZCard = %d after %d concurrent calls, want %d: %d request(s) were not counted, "+
			"so a caller can exceed its limit by that many", got, calls, calls, calls-got)
	}
}

// TestRedisRateLimiter_MemberCollisionRate quantifies the headroom in the
// member's entropy. It deliberately pins every call to one timestamp, which is
// the worst case the script can face, and reports how many of N distinct
// requests survive as distinct members.
//
// This is a characterisation test: it documents the size of the margin rather
// than asserting a specific count, and fails only if the loss is far worse than
// random collisions in math.random's range can explain.
func TestRedisRateLimiter_MemberCollisionRate(t *testing.T) {
	rdb := testsupport.TestRedis(t)
	client := middleware.NewRedisRateLimiterClient(rdb)
	ctx := context.Background()

	const calls = 2000
	key := "ratelimit:collision"
	frozen := time.Now()

	for range calls {
		if _, err := client.SlidingWindowCount(ctx, key, frozen, time.Hour); err != nil {
			t.Fatalf("SlidingWindowCount: %v", err)
		}
	}

	got, err := rdb.ZCard(ctx, key).Result()
	if err != nil {
		t.Fatalf("ZCard: %v", err)
	}

	lost := calls - got
	t.Logf("identical-timestamp calls: %d, distinct members: %d, lost to collisions: %d (%.3f%%)",
		calls, got, lost, float64(lost)*100/float64(calls))

	// With a 1e6 random range, the birthday expectation for 2000 draws is
	// ~2 collisions. An order of magnitude beyond that means the member is
	// far less unique than the range implies.
	if lost > 50 {
		t.Errorf("lost %d of %d calls to member collisions, far beyond the ~2 expected: "+
			"the member is not as unique as math.random(1000000) suggests", lost, calls)
	}
}

// TestRedisRateLimiter_KeysAreIndependent confirms the script scopes state to
// KEYS[1] only, so one user or endpoint exhausting its limit cannot affect
// another.
func TestRedisRateLimiter_KeysAreIndependent(t *testing.T) {
	rdb := testsupport.TestRedis(t)
	client := middleware.NewRedisRateLimiterClient(rdb)
	ctx := context.Background()

	for i := range 3 {
		if _, err := client.SlidingWindowCount(ctx, "ratelimit:userA:posts", time.Now(), time.Hour); err != nil {
			t.Fatalf("userA call %d: %v", i+1, err)
		}
	}

	count, err := client.SlidingWindowCount(ctx, "ratelimit:userB:posts", time.Now(), time.Hour)
	if err != nil {
		t.Fatalf("userB: %v", err)
	}
	if count != 1 {
		t.Errorf("userB count = %d, want 1 (state leaked across keys)", count)
	}

	// Same user, different endpoint, must also be independent.
	count, err = client.SlidingWindowCount(ctx, "ratelimit:userA:reports", time.Now(), time.Hour)
	if err != nil {
		t.Fatalf("userA reports: %v", err)
	}
	if count != 1 {
		t.Errorf("userA reports count = %d, want 1 (state leaked across endpoints)", count)
	}
}
