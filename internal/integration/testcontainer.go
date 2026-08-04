//go:build integration

package integration

import (
	"net/http"
	"slices"
	"testing"

	"github.com/fireynis/the-bell/internal/app"
	"github.com/fireynis/the-bell/internal/config"
	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/middleware"
	"github.com/fireynis/the-bell/internal/repository/postgres"
	"github.com/fireynis/the-bell/internal/server"
	"github.com/fireynis/the-bell/internal/service"
	"github.com/fireynis/the-bell/internal/testsupport"
	"github.com/jackc/pgx/v5/pgxpool"
)

// mockAuthMiddleware returns middleware that injects the given user into the
// request context, simulating a successful Kratos authentication.
//
// This is the one thing here that is faked rather than stood up for real:
// tests that exercise Kratos itself do so against a real client in auth_test.go.
func mockAuthMiddleware(user *domain.User) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := middleware.WithUser(r.Context(), user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// testDeps builds the production dependency graph over the test's database and
// Redis.
//
// Going through app.Build rather than re-listing the wiring here is the point:
// routes register conditionally on a non-nil dependency, so a harness that
// omits one silently drops a whole route family and a test asserting 404 passes
// for the wrong reason. Sharing one definition with cmd/bell makes that
// impossible by construction.
func testDeps(t *testing.T, pool *pgxpool.Pool) *app.Deps {
	t.Helper()

	cfg := config.Config{
		Port:        8080,
		DatabaseURL: "unused",
		// Any absolute URL will do: the Kratos-backed auth middleware Build
		// installs is replaced below, and no test resolves this host.
		KratosPublicURL: "http://kratos.invalid",
		KratosAdminURL:  "http://kratos.invalid",
		// Uploads land in a per-test temp dir that the testing package removes.
		ImageStoragePath: t.TempDir(),
	}

	deps, err := app.Build(cfg, pool, testsupport.TestRedis(t), testsupport.DiscardLogger())
	if err != nil {
		t.Fatalf("building app dependencies: %v", err)
	}
	return deps
}

// testServer creates a fully wired Server for integration testing, using the
// given pool and injecting the authUser via mock auth middleware.
func testServer(t *testing.T, pool *pgxpool.Pool, authUser *domain.User) *server.Server {
	t.Helper()
	deps := testDeps(t, pool)

	// Options apply in order, so appending WithAuth replaces the Kratos
	// middleware Build installed. Clone first: appending to deps.ServerOptions
	// in place would write into its backing array.
	opts := append(slices.Clone(deps.ServerOptions), server.WithAuth(mockAuthMiddleware(authUser)))

	return server.New(deps.Config, pool, deps.Logger, opts...)
}

// testServices exposes the pieces of the graph that tests drive directly,
// below the HTTP layer.
type testServices struct {
	VouchService            *service.VouchService
	ModerationActionService *service.ModerationActionService

	// AGEQuerier is built here rather than taken from app.Deps, which exposes
	// services rather than repositories. It is a single constructor over the
	// pool, so there is no wiring graph to drift.
	AGEQuerier *postgres.AGEQuerier
}

// newTestServices creates services backed by the given pool.
func newTestServices(t *testing.T, pool *pgxpool.Pool) *testServices {
	t.Helper()
	deps := testDeps(t, pool)

	return &testServices{
		VouchService:            deps.VouchService,
		ModerationActionService: deps.ModerationService,
		AGEQuerier:              postgres.NewAGEQuerier(pool),
	}
}
