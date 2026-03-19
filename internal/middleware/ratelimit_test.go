package middleware_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/middleware"
)

// memoryRateLimiterClient is an in-memory implementation of RateLimiterClient
// for testing without Redis.
type memoryRateLimiterClient struct {
	mu      sync.Mutex
	entries map[string][]time.Time
}

func newMemoryRateLimiterClient() *memoryRateLimiterClient {
	return &memoryRateLimiterClient{
		entries: make(map[string][]time.Time),
	}
}

func (m *memoryRateLimiterClient) SlidingWindowCount(_ context.Context, key string, now time.Time, window time.Duration) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	windowStart := now.Add(-window)

	// Remove expired entries
	var valid []time.Time
	for _, t := range m.entries[key] {
		if t.After(windowStart) {
			valid = append(valid, t)
		}
	}

	// Add current
	valid = append(valid, now)
	m.entries[key] = valid

	return int64(len(valid)), nil
}

// errorRateLimiterClient always returns an error.
type errorRateLimiterClient struct{}

func (e *errorRateLimiterClient) SlidingWindowCount(_ context.Context, _ string, _ time.Time, _ time.Duration) (int64, error) {
	return 0, context.DeadlineExceeded
}

func TestRateLimiter_AllowsUnderLimit(t *testing.T) {
	client := newMemoryRateLimiterClient()
	rl := middleware.NewRateLimiter(client, testLogger())

	user := &domain.User{ID: "user-1", Role: domain.RoleMember, IsActive: true}

	handler := rl.Limit("posts", 3, time.Hour)(okHandler())

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/posts", nil)
		ctx := middleware.WithUser(req.Context(), user)
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)
		assertStatus(t, rec, http.StatusOK)
	}
}

func TestRateLimiter_BlocksOverLimit(t *testing.T) {
	client := newMemoryRateLimiterClient()
	rl := middleware.NewRateLimiter(client, testLogger())

	user := &domain.User{ID: "user-2", Role: domain.RoleMember, IsActive: true}

	handler := rl.Limit("posts", 2, time.Hour)(okHandler())

	// First 2 requests should succeed
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/posts", nil)
		ctx := middleware.WithUser(req.Context(), user)
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		assertStatus(t, rec, http.StatusOK)
	}

	// Third request should be rate limited
	req := httptest.NewRequest(http.MethodPost, "/api/v1/posts", nil)
	ctx := middleware.WithUser(req.Context(), user)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusTooManyRequests)

	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to parse response body: %v", err)
	}
	if body["error"] != "rate limit exceeded" {
		t.Errorf("error = %q, want %q", body["error"], "rate limit exceeded")
	}
}

func TestRateLimiter_SetsRetryAfterHeader(t *testing.T) {
	client := newMemoryRateLimiterClient()
	rl := middleware.NewRateLimiter(client, testLogger())

	user := &domain.User{ID: "user-3", Role: domain.RoleMember, IsActive: true}

	handler := rl.Limit("posts", 1, time.Hour)(okHandler())

	// First request succeeds
	req := httptest.NewRequest(http.MethodPost, "/api/v1/posts", nil)
	ctx := middleware.WithUser(req.Context(), user)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assertStatus(t, rec, http.StatusOK)

	// Second request is rate limited with Retry-After
	req = httptest.NewRequest(http.MethodPost, "/api/v1/posts", nil)
	ctx = middleware.WithUser(req.Context(), user)
	req = req.WithContext(ctx)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusTooManyRequests)

	retryAfter := rec.Header().Get("Retry-After")
	if retryAfter != "3600" {
		t.Errorf("Retry-After = %q, want %q", retryAfter, "3600")
	}
}

func TestRateLimiter_DifferentUsersIndependent(t *testing.T) {
	client := newMemoryRateLimiterClient()
	rl := middleware.NewRateLimiter(client, testLogger())

	user1 := &domain.User{ID: "user-a", Role: domain.RoleMember, IsActive: true}
	user2 := &domain.User{ID: "user-b", Role: domain.RoleMember, IsActive: true}

	handler := rl.Limit("posts", 1, time.Hour)(okHandler())

	// User1 uses their limit
	req := httptest.NewRequest(http.MethodPost, "/api/v1/posts", nil)
	ctx := middleware.WithUser(req.Context(), user1)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assertStatus(t, rec, http.StatusOK)

	// User2 still has their own limit
	req = httptest.NewRequest(http.MethodPost, "/api/v1/posts", nil)
	ctx = middleware.WithUser(req.Context(), user2)
	req = req.WithContext(ctx)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assertStatus(t, rec, http.StatusOK)

	// User1 is rate limited
	req = httptest.NewRequest(http.MethodPost, "/api/v1/posts", nil)
	ctx = middleware.WithUser(req.Context(), user1)
	req = req.WithContext(ctx)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assertStatus(t, rec, http.StatusTooManyRequests)
}

func TestRateLimiter_DifferentEndpointsIndependent(t *testing.T) {
	client := newMemoryRateLimiterClient()
	rl := middleware.NewRateLimiter(client, testLogger())

	user := &domain.User{ID: "user-c", Role: domain.RoleMember, IsActive: true}

	postsHandler := rl.Limit("posts", 1, time.Hour)(okHandler())
	reportsHandler := rl.Limit("reports", 1, time.Hour)(okHandler())

	// Use posts limit
	req := httptest.NewRequest(http.MethodPost, "/api/v1/posts", nil)
	ctx := middleware.WithUser(req.Context(), user)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	postsHandler.ServeHTTP(rec, req)
	assertStatus(t, rec, http.StatusOK)

	// Reports limit is independent
	req = httptest.NewRequest(http.MethodPost, "/api/v1/posts/1/report", nil)
	ctx = middleware.WithUser(req.Context(), user)
	req = req.WithContext(ctx)
	rec = httptest.NewRecorder()
	reportsHandler.ServeHTTP(rec, req)
	assertStatus(t, rec, http.StatusOK)
}

func TestRateLimiter_NoUserInContext(t *testing.T) {
	client := newMemoryRateLimiterClient()
	rl := middleware.NewRateLimiter(client, testLogger())

	handler := rl.Limit("posts", 1, time.Hour)(okHandler())

	// No user in context — should pass through
	req := httptest.NewRequest(http.MethodPost, "/api/v1/posts", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assertStatus(t, rec, http.StatusOK)
}

func TestRateLimiter_NilClient(t *testing.T) {
	rl := middleware.NewRateLimiter(nil, testLogger())

	user := &domain.User{ID: "user-d", Role: domain.RoleMember, IsActive: true}

	handler := rl.Limit("posts", 1, time.Hour)(okHandler())

	// Should pass through even beyond the limit since client is nil
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/posts", nil)
		ctx := middleware.WithUser(req.Context(), user)
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		assertStatus(t, rec, http.StatusOK)
	}
}

func TestRateLimiter_RedisError_FailOpen(t *testing.T) {
	client := &errorRateLimiterClient{}
	rl := middleware.NewRateLimiter(client, testLogger())

	user := &domain.User{ID: "user-e", Role: domain.RoleMember, IsActive: true}

	handler := rl.Limit("posts", 1, time.Hour)(okHandler())

	// Even though Redis returns an error, requests should pass through
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/posts", nil)
		ctx := middleware.WithUser(req.Context(), user)
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		assertStatus(t, rec, http.StatusOK)
	}
}

func TestRateLimiter_DailyWindow_RetryAfter(t *testing.T) {
	client := newMemoryRateLimiterClient()
	rl := middleware.NewRateLimiter(client, testLogger())

	user := &domain.User{ID: "user-f", Role: domain.RoleMember, IsActive: true}

	handler := rl.Limit("vouches", 1, 24*time.Hour)(okHandler())

	// Use the limit
	req := httptest.NewRequest(http.MethodPost, "/api/v1/vouches/approve/1", nil)
	ctx := middleware.WithUser(req.Context(), user)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assertStatus(t, rec, http.StatusOK)

	// Should be rate limited with 86400 second Retry-After
	req = httptest.NewRequest(http.MethodPost, "/api/v1/vouches/approve/2", nil)
	ctx = middleware.WithUser(req.Context(), user)
	req = req.WithContext(ctx)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusTooManyRequests)

	retryAfter := rec.Header().Get("Retry-After")
	if retryAfter != "86400" {
		t.Errorf("Retry-After = %q, want %q", retryAfter, "86400")
	}
}
