package app

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/fireynis/the-bell/internal/config"
	"github.com/fireynis/the-bell/internal/service"
)

// Build's whole point is that one graph serves the CLI and the test harness, so
// its preconditions must fail loudly rather than produce a half-wired Deps that
// nil-panics later, deep inside a request.
func TestBuild_RequiresItsCollaborators(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))

	tests := []struct {
		name   string
		logger *slog.Logger
	}{
		{"nil pool", logger},
		{"nil pool and nil logger", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps, err := Build(config.Config{}, nil, nil, tt.logger)
			if err == nil {
				t.Fatal("expected an error, got nil")
			}
			if deps != nil {
				t.Errorf("deps = %+v, want nil alongside the error", deps)
			}
		})
	}
}

// TRUST_SWEEP_INTERVAL only means anything if it reaches the worker. The knob
// existed on TrustWorker for some time with nothing calling it, so the wiring
// is the part worth pinning rather than the setter.
//
// Neither pgxpool nor go-redis dials on construction, so Build can be exercised
// here without either server: nothing in it issues a query.
func TestBuild_WiresTheTrustSweepInterval(t *testing.T) {
	tests := []struct {
		name       string
		configured time.Duration
		want       time.Duration
	}{
		{"a configured interval reaches the worker", 6 * time.Hour, 6 * time.Hour},
		{"the documented default", 24 * time.Hour, 24 * time.Hour},
		// config.Load rejects zero, so this is only reachable from a hand-built
		// Config — the integration harness's. The worker's own guard keeps its
		// default rather than turning the sweep loop into a spin.
		{"a zero from a hand-built config falls back", 0, 24 * time.Hour},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool, err := pgxpool.New(context.Background(), "postgres://bell@127.0.0.1:1/bell")
			if err != nil {
				t.Fatalf("creating pool: %v", err)
			}
			t.Cleanup(pool.Close)

			rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
			t.Cleanup(func() { rdb.Close() })

			cfg := config.Config{
				Port:               8080,
				KratosPublicURL:    "http://kratos.invalid",
				KratosAdminURL:     "http://kratos.invalid",
				ImageStoragePath:   t.TempDir(),
				TrustSweepInterval: tt.configured,
			}

			deps, err := Build(cfg, pool, rdb, slog.New(slog.NewJSONHandler(io.Discard, nil)))
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			if deps.TrustWorker == nil {
				t.Fatal("Build returned no TrustWorker despite being given a Redis client")
			}
			if got := deps.TrustWorker.SweepInterval(); got != tt.want {
				t.Errorf("SweepInterval() = %s, want %s", got, tt.want)
			}
		})
	}
}

// trustInputs exists because service.TrustInputs spans five repositories and no
// single one satisfies it. If a repository stops providing its share — or two
// of them start colliding on a method name, making the promoted selector
// ambiguous — this stops compiling, which is the point.
func TestTrustInputsSatisfiesTheInterface(t *testing.T) {
	var _ service.TrustInputs = trustInputs{}
}
