package middleware

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"
)

// RateLimiterClient abstracts the Redis operations needed for sliding-window
// rate limiting. This allows testing without a real Redis connection.
type RateLimiterClient interface {
	// SlidingWindowCount atomically removes expired entries, adds the current
	// timestamp, sets the key TTL, and returns the current count of entries
	// within the window.
	SlidingWindowCount(ctx context.Context, key string, now time.Time, window time.Duration) (int64, error)
}

// RateLimiter produces chi-compatible middleware that enforces per-user
// sliding-window rate limits backed by Redis.
type RateLimiter struct {
	client RateLimiterClient
	logger *slog.Logger
}

// NewRateLimiter creates a RateLimiter. If client is nil, all requests are
// allowed (fail open).
func NewRateLimiter(client RateLimiterClient, logger *slog.Logger) *RateLimiter {
	return &RateLimiter{client: client, logger: logger}
}

// Limit returns chi middleware that restricts the authenticated user to
// maxRequests within the given window. The endpoint string is used to
// namespace the rate-limit key: ratelimit:{user_id}:{endpoint}:{window}.
//
// If the user is not in context (unauthenticated) or Redis is unreachable,
// the request is allowed through (fail open).
func (rl *RateLimiter) Limit(endpoint string, maxRequests int, window time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if rl.client == nil {
				next.ServeHTTP(w, r)
				return
			}

			user, ok := UserFromContext(r.Context())
			if !ok {
				// No authenticated user — let the auth middleware handle it.
				next.ServeHTTP(w, r)
				return
			}

			key := fmt.Sprintf("ratelimit:%s:%s:%s", user.ID, endpoint, formatWindow(window))
			now := time.Now()

			count, err := rl.client.SlidingWindowCount(r.Context(), key, now, window)
			if err != nil {
				// Fail open: if Redis is down, allow the request.
				rl.logger.Warn("ratelimit: redis error, allowing request",
					"error", err,
					"user_id", user.ID,
					"endpoint", endpoint,
				)
				next.ServeHTTP(w, r)
				return
			}

			if count > int64(maxRequests) {
				retryAfter := int(window.Seconds())
				w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
				writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// formatWindow returns a human-readable window label for the Redis key.
func formatWindow(d time.Duration) string {
	switch {
	case d%(24*time.Hour) == 0:
		days := int(d / (24 * time.Hour))
		return fmt.Sprintf("%dd", days)
	case d%time.Hour == 0:
		hours := int(d / time.Hour)
		return fmt.Sprintf("%dh", hours)
	case d%time.Minute == 0:
		mins := int(d / time.Minute)
		return fmt.Sprintf("%dm", mins)
	default:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
}
