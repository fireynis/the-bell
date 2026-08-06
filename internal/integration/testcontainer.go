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
	"github.com/redis/go-redis/v9"
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
	return testDepsWithRedis(t, pool, testsupport.TestRedis(t))
}

// testDepsWithRedis is testDeps over a caller-supplied Redis client.
//
// testsupport.TestRedis hands out a different logical database on every call,
// so two servers built through testDeps cannot see each other's feed cache.
// Tests that need several identities acting against one cache — a moderator
// removing a post while the town's cached feed must stop serving it — supply
// one client for all of them.
func testDepsWithRedis(t *testing.T, pool *pgxpool.Pool, rdb *redis.Client) *app.Deps {
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

	deps, err := app.Build(cfg, pool, rdb, testsupport.DiscardLogger())
	if err != nil {
		t.Fatalf("building app dependencies: %v", err)
	}
	return deps
}

// testServer creates a fully wired Server for integration testing, using the
// given pool and injecting the authUser via mock auth middleware.
func testServer(t *testing.T, pool *pgxpool.Pool, authUser *domain.User) *server.Server {
	t.Helper()
	return serverFromDeps(t, pool, testDeps(t, pool), authUser)
}

// testServerWithRedis is testServer over a caller-supplied Redis client. See
// testDepsWithRedis for when that matters.
func testServerWithRedis(t *testing.T, pool *pgxpool.Pool, rdb *redis.Client, authUser *domain.User) *server.Server {
	t.Helper()
	return serverFromDeps(t, pool, testDepsWithRedis(t, pool, rdb), authUser)
}

// serverFromDeps installs the mock identity over a built dependency graph.
//
// BOTH auth middlewares are replaced, not just the guarded one. The public post
// routes — GET /api/v1/posts and GET /api/v1/posts/{id} — resolve their caller
// through OptionalAuth, which app.Build points at Kratos. Overriding only
// WithAuth left every integration test looking anonymous on those routes, so
// canViewPost's author-and-moderator branch and the whole user_reactions
// enrichment path were never exercised with a real identity: a moderator asking
// for a removed post got the stranger's 404, which reads as a product bug and
// is not one. This is the same class of gap as a harness that omits a service
// and silently loses a route family.
//
// A test that wants a genuinely anonymous reader uses anonymousTestServer.
func serverFromDeps(t *testing.T, pool *pgxpool.Pool, deps *app.Deps, authUser *domain.User) *server.Server {
	t.Helper()

	// Options apply in order, so appending replaces the middleware Build
	// installed. Clone first: appending to deps.ServerOptions in place would
	// write into its backing array.
	opts := append(slices.Clone(deps.ServerOptions),
		server.WithAuth(mockAuthMiddleware(authUser)),
		server.WithOptionalAuth(mockAuthMiddleware(authUser)),
	)

	return server.New(deps.Config, pool, deps.Logger, opts...)
}

// anonymousTestServer builds a server whose OptionalAuth passes callers through
// untouched, which is what a reader with no session looks like on a public
// route. The guarded middleware is left as Build installed it, so anything
// behind a guard is refused.
func anonymousTestServer(t *testing.T, pool *pgxpool.Pool, rdb *redis.Client) *server.Server {
	t.Helper()
	deps := testDepsWithRedis(t, pool, rdb)

	passthrough := func(next http.Handler) http.Handler { return next }
	opts := append(slices.Clone(deps.ServerOptions), server.WithOptionalAuth(passthrough))

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
