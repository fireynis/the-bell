// Package testsupport provides shared infrastructure for tests that need real
// backing services rather than fakes.
//
// The project rule is that tests may only mock what cannot be stood up in
// Docker. Postgres and Redis both can, so this package stands them up: one
// container of each per test binary, handing out a freshly-migrated database
// (or a flushed Redis logical database) per call.
//
// This package deliberately carries no build tag. The //go:build integration
// tag belongs on the *tests that call it*, not here — a tagged package cannot
// be imported by untagged callers, which is what previously kept the harness
// locked inside package integration.
//
// Callers should delegate their TestMain to RunMain so the shared containers
// are torn down promptly when the binary exits:
//
//	//go:build integration
//
//	func TestMain(m *testing.M) { os.Exit(testsupport.RunMain(m)) }
package testsupport

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const (
	// postgresImage must match the image pinned in docker-compose.yml and
	// deploy/docker-compose.yml so tests exercise the same Postgres major and
	// the same Apache AGE version as production.
	postgresImage = "apache/age:release_PG18_1.7.0"

	// redisImage matches the redis service in both compose files.
	redisImage = "redis:7-alpine"
)

// startupTimeout bounds how long we wait for a container to become usable.
// Generous because CI runners pull the image cold.
const startupTimeout = 120 * time.Second

// dbCounter names the per-test databases handed out by TestDB.
var dbCounter atomic.Int64

// RunMain runs the test binary and tears down any containers it started,
// returning the exit code. Call it from TestMain:
//
//	func TestMain(m *testing.M) { os.Exit(testsupport.RunMain(m)) }
//
// Delegating to RunMain is not required for correctness — the containers start
// lazily on first use and Testcontainers' reaper eventually collects them — but
// it releases them as soon as the binary finishes.
func RunMain(m *testing.M) int {
	code := m.Run()
	shutdown()
	return code
}

// shutdown terminates every container this package started.
func shutdown() {
	ctx := context.Background()
	for _, stop := range []func(context.Context){stopPostgres, stopRedis} {
		stop(ctx)
	}
}

// UniqueKratosID generates a unique Kratos identity ID for test users.
func UniqueKratosID(suffix string) string {
	return fmt.Sprintf("kratos-%s-%d", suffix, time.Now().UnixNano())
}

// DiscardLogger returns a logger that discards all output, useful for
// middleware and services that require a logger but whose output is noise.
func DiscardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// once wraps sync.Once so a failed start is reported to every caller rather
// than leaving later callers to trip over a nil resource.
type once struct {
	sync.Once
	err error
}

func (o *once) do(fn func() error) error {
	o.Do(func() { o.err = fn() })
	return o.err
}
