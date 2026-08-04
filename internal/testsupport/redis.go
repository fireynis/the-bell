package testsupport

import (
	"context"
	"fmt"
	"net"
	"sync/atomic"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// redisLogicalDBs is the number of logical databases a stock Redis exposes.
// Each TestRedis caller is handed a different one so concurrent tests do not
// collide on key names.
const redisLogicalDBs = 16

var (
	redisOnce      once
	redisContainer testcontainers.Container
	redisAddr      string

	// redisCounter hands out a different logical database per caller.
	redisCounter atomic.Int64
)

// TestRedis returns a client connected to a flushed logical database on a
// Redis container shared across the test binary.
//
// The return type is the concrete *redis.Client rather than redis.Cmdable
// because middleware.NewRedisRateLimiterClient requires exactly that type, so
// rate-limiter tests can use this with no production refactor.
//
// The logical database is flushed on acquisition and again on cleanup. Indexes
// are reused after redisLogicalDBs callers, so tests calling TestRedis must not
// run concurrently beyond that many at a time.
func TestRedis(t *testing.T) *redis.Client {
	t.Helper()
	ctx := context.Background()

	if err := ensureRedis(ctx); err != nil {
		t.Fatalf("starting redis container: %v", err)
	}

	db := int(redisCounter.Add(1) % redisLogicalDBs)

	client := redis.NewClient(&redis.Options{Addr: redisAddr, DB: db})
	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		t.Fatalf("pinging redis: %v", err)
	}
	if err := client.FlushDB(ctx).Err(); err != nil {
		client.Close()
		t.Fatalf("flushing redis db %d: %v", db, err)
	}

	t.Cleanup(func() {
		if err := client.FlushDB(ctx).Err(); err != nil {
			t.Logf("flushing redis db %d: %v", db, err)
		}
		client.Close()
	})

	return client
}

// ensureRedis starts the shared Redis container once per test binary.
//
// This uses the generic container API rather than
// testcontainers-go/modules/redis: the generic API is already vendored, while
// the module would add a dependency and a full re-vendor to save the handful of
// lines below.
func ensureRedis(ctx context.Context) error {
	return redisOnce.do(func() error {
		container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
			ContainerRequest: testcontainers.ContainerRequest{
				Image:        redisImage,
				ExposedPorts: []string{"6379/tcp"},
				WaitingFor: wait.ForListeningPort("6379/tcp").
					WithStartupTimeout(startupTimeout),
			},
			Started: true,
		})
		if err != nil {
			return fmt.Errorf("starting redis container: %w", err)
		}
		redisContainer = container

		host, err := container.Host(ctx)
		if err != nil {
			return fmt.Errorf("getting redis host: %w", err)
		}
		port, err := container.MappedPort(ctx, "6379/tcp")
		if err != nil {
			return fmt.Errorf("getting redis port: %w", err)
		}
		redisAddr = net.JoinHostPort(host, port.Port())

		return nil
	})
}

// stopRedis tears down the shared Redis container, if one was started.
func stopRedis(ctx context.Context) {
	if redisContainer != nil {
		_ = redisContainer.Terminate(ctx)
		redisContainer = nil
	}
}
