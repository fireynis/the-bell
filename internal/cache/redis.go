package cache

import (
	"github.com/redis/go-redis/v9"
)

// NewRedisClient creates a go-redis client from a URL string.
// The URL should follow the redis:// scheme, e.g. "redis://localhost:6379/0".
func NewRedisClient(url string) (*redis.Client, error) {
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, err
	}
	return redis.NewClient(opts), nil
}
