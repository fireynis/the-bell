package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/fireynis/the-bell/internal/config"
	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/middleware"
	"github.com/fireynis/the-bell/internal/service"
	"github.com/fireynis/the-bell/internal/sse"
	"github.com/fireynis/the-bell/internal/storage"
)

// Stub repositories for the read paths that are reachable without a session.
// Everything behind a guard is rejected by middleware before its handler runs,
// so those services can be wired with nil repositories.

type stubPostRepo struct{}

func (stubPostRepo) CreatePost(context.Context, *domain.Post) error { return nil }
func (stubPostRepo) GetPostByID(context.Context, string) (*domain.Post, error) {
	return &domain.Post{ID: "p1", AuthorID: "u1", Body: "hello", Status: domain.PostVisible}, nil
}
func (stubPostRepo) ListPosts(context.Context, string, int) ([]*domain.Post, error) {
	return nil, nil
}
func (stubPostRepo) ListPostsByAuthor(context.Context, string, int) ([]*domain.Post, error) {
	return nil, nil
}
func (stubPostRepo) UpdatePostBody(context.Context, string, string) (*domain.Post, error) {
	return &domain.Post{ID: "p1"}, nil
}
func (stubPostRepo) UpdatePostStatus(context.Context, string, domain.PostStatus, string, string) error {
	return nil
}

type stubUserRepo struct{}

func (stubUserRepo) CreateUser(context.Context, *domain.User) error { return nil }
func (stubUserRepo) GetUserByID(context.Context, string) (*domain.User, error) {
	return &domain.User{ID: "u1", DisplayName: "Stub", Role: domain.RoleMember, IsActive: true}, nil
}
func (stubUserRepo) GetUserByKratosID(context.Context, string) (*domain.User, error) {
	return &domain.User{ID: "u1"}, nil
}
func (stubUserRepo) UpdateUserProfile(context.Context, string, string, string, string) (*domain.User, error) {
	return &domain.User{ID: "u1"}, nil
}
func (stubUserRepo) ListDirectoryUsers(context.Context, string, int, int) ([]*domain.User, error) {
	return []*domain.User{{ID: "u1", DisplayName: "Stub", Role: domain.RoleMember, IsActive: true}}, nil
}
func (stubUserRepo) CountDirectoryUsers(context.Context, string) (int64, error) { return 1, nil }

type stubVouchRepo struct{}

func (stubVouchRepo) CreateVouch(context.Context, *domain.Vouch) error { return nil }
func (stubVouchRepo) GetVouchByID(context.Context, string) (*domain.Vouch, error) {
	return nil, nil
}
func (stubVouchRepo) GetVouchByPair(context.Context, string, string) (*domain.Vouch, error) {
	return nil, nil
}
func (stubVouchRepo) CountVouchesByVoucherSince(context.Context, string, time.Time) (int64, error) {
	return 0, nil
}
func (stubVouchRepo) ListActiveVouchesByVouchee(context.Context, string) ([]*domain.Vouch, error) {
	return nil, nil
}
func (stubVouchRepo) ListActiveVouchesByVoucher(context.Context, string) ([]*domain.Vouch, error) {
	return nil, nil
}
func (stubVouchRepo) RevokeVouch(context.Context, string) error { return nil }

type stubConfigRepo struct{}

func (stubConfigRepo) SetTownConfig(context.Context, string, string) error { return nil }
func (stubConfigRepo) GetTownConfig(context.Context, string) (string, error) {
	return "", nil
}
func (stubConfigRepo) ListTownConfig(context.Context) (map[string]string, error) {
	return map[string]string{"town_name": "Stubville"}, nil
}

type stubReactionEnricher struct{}

func (stubReactionEnricher) BatchCountByPosts(context.Context, []string) (map[string]map[domain.ReactionType]int, error) {
	return nil, nil
}
func (stubReactionEnricher) BatchGetUserReactions(context.Context, string, []string) (map[string][]domain.ReactionType, error) {
	return nil, nil
}

// newWiredServer builds a Server with every service present so apiRoutes takes
// all of its registration branches. The auth middleware is a pass-through that
// injects no user, which is what an unauthenticated request looks like once it
// reaches the guards.
//
// The image store is a real LocalStorage rooted at cfg.ImageStoragePath rather
// than a stub, because /uploads now serves through it: a stub that answered
// nothing would make every upload test assert against the stub instead of
// against the route. Docker is not needed for a directory, so there is no
// reason to fake one.
func newWiredServer(t *testing.T, cfg config.Config) *Server {
	t.Helper()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))

	imageStore, err := storage.NewLocalStorage(cfg.ImageStoragePath, "/uploads/")
	if err != nil {
		t.Fatalf("NewLocalStorage(%q): %v", cfg.ImageStoragePath, err)
	}

	return New(cfg, nil, logger,
		WithPostService(service.NewPostService(stubPostRepo{}, nil)),
		WithUserService(service.NewUserService(stubUserRepo{}, nil)),
		WithVouchService(service.NewVouchService(stubVouchRepo{}, nil, nil, nil)),
		WithReportService(service.NewReportService(nil, nil, nil)),
		WithModerationActionService(service.NewModerationActionService(nil, nil, nil, nil, nil, nil, nil)),
		WithApprovalService(service.NewApprovalService(nil, nil)),
		WithVotingService(service.NewVotingService(nil, nil)),
		WithReactionService(service.NewReactionService(nil, nil)),
		WithStatsService(service.NewStatsService(nil)),
		WithConfigRepo(stubConfigRepo{}),
		WithReactionRepo(stubReactionEnricher{}),
		WithSSEBroker(sse.NewBroker(nil, logger)),
		WithImageStore(imageStore),
		WithRateLimiter(middleware.NewRateLimiter(nil, logger)),
		WithAuth(func(next http.Handler) http.Handler { return next }),
	)
}

// Every route below is registered somewhere in routes/apiRoutes, and each row
// names the pattern it must resolve to.
//
// The absence of a 404 is NOT sufficient evidence, which is what this test used
// to rely on. chi runs a subrouter's middleware before it resolves the path
// within that subrouter, so every group declared with r.Route + r.Use — /v1/me,
// /v1/moderation, /v1/feed/live, the report and reaction routes — answers 401
// or 403 for a path that was never registered at all. Verified: a request to
// /api/v1/moderation/nonexistent returns 401, and the moderator post-removal
// row passed this table before its route existed.
//
// So registration is checked against the router itself. chi's Find returns the
// registered pattern for a method and path, or "" when nothing matches, and it
// consults the routing tree rather than serving the request — no middleware, no
// guard, nothing to mistake for a match. Asserting the exact pattern rather
// than merely "something matched" also catches a route that silently moves.
//
// The status assertion stays: it is what proves the guard on a resolved route.
func TestRoutesAreRegistered(t *testing.T) {
	srv := newWiredServer(t, config.Config{Port: 0, ImageStoragePath: t.TempDir()})
	h := srv.Handler()

	// The handler is the chi router itself; Find is not on http.Handler.
	mux, ok := h.(*chi.Mux)
	if !ok {
		t.Fatalf("server handler is %T, want *chi.Mux — registration cannot be verified", h)
	}

	tests := []struct {
		name    string
		method  string
		path    string
		pattern string
		want    int
	}{
		{"health", http.MethodGet, "/healthz", "/healthz", http.StatusOK},

		// Public reads — no session required.
		{"feed", http.MethodGet, "/api/v1/posts", "/api/v1/posts", http.StatusOK},
		{"single post", http.MethodGet, "/api/v1/posts/p1", "/api/v1/posts/{id}", http.StatusOK},
		{"user profile", http.MethodGet, "/api/v1/users/u1", "/api/v1/users/{id}", http.StatusOK},
		{"user posts", http.MethodGet, "/api/v1/users/u1/posts", "/api/v1/users/{id}/posts", http.StatusOK},
		{"user vouches", http.MethodGet, "/api/v1/users/u1/vouches", "/api/v1/users/{id}/vouches", http.StatusOK},
		{"town config", http.MethodGet, "/api/v1/config", "/api/v1/config", http.StatusOK},

		// Guarded routes — rejected without a user in context.
		{"live feed", http.MethodGet, "/api/v1/feed/live", "/api/v1/feed/live", http.StatusUnauthorized},
		{"me", http.MethodGet, "/api/v1/me", "/api/v1/me", http.StatusUnauthorized},
		{"create post", http.MethodPost, "/api/v1/posts", "/api/v1/posts", http.StatusUnauthorized},
		{"edit post", http.MethodPatch, "/api/v1/posts/p1", "/api/v1/posts/{id}", http.StatusUnauthorized},
		{"delete post", http.MethodDelete, "/api/v1/posts/p1", "/api/v1/posts/{id}", http.StatusUnauthorized},
		{"add reaction", http.MethodPost, "/api/v1/posts/p1/reactions", "/api/v1/posts/{postId}/reactions", http.StatusUnauthorized},
		{"remove reaction", http.MethodDelete, "/api/v1/posts/p1/reactions/like", "/api/v1/posts/{postId}/reactions/{type}", http.StatusUnauthorized},
		{"user directory", http.MethodGet, "/api/v1/users", "/api/v1/users", http.StatusUnauthorized},
		{"user directory with a trailing slash", http.MethodGet, "/api/v1/users/", "/api/v1/users/", http.StatusUnauthorized},
		{"users me", http.MethodGet, "/api/v1/users/me", "/api/v1/users/me", http.StatusUnauthorized},
		{"update users me", http.MethodPut, "/api/v1/users/me", "/api/v1/users/me", http.StatusUnauthorized},
		{"report post", http.MethodPost, "/api/v1/posts/p1/report", "/api/v1/posts/{id}/report", http.StatusUnauthorized},
		{"moderation queue", http.MethodGet, "/api/v1/moderation/queue", "/api/v1/moderation/queue", http.StatusUnauthorized},
		{"update report", http.MethodPatch, "/api/v1/moderation/reports/r1", "/api/v1/moderation/reports/{id}", http.StatusUnauthorized},
		{"moderation action", http.MethodPost, "/api/v1/moderation/actions", "/api/v1/moderation/actions", http.StatusUnauthorized},
		{"list actions", http.MethodGet, "/api/v1/moderation/actions/u1", "/api/v1/moderation/actions/{user_id}", http.StatusUnauthorized},
		{"remove post", http.MethodPost, "/api/v1/moderation/posts/p1/remove", "/api/v1/moderation/posts/{id}/remove", http.StatusUnauthorized},
		{"pending vouches", http.MethodGet, "/api/v1/vouches/pending", "/api/v1/vouches/pending", http.StatusUnauthorized},
		{"approve vouch", http.MethodPost, "/api/v1/vouches/approve/v1", "/api/v1/vouches/approve/{id}", http.StatusUnauthorized},
		{"cast vote", http.MethodPost, "/api/v1/admin/council/votes", "/api/v1/admin/council/votes", http.StatusUnauthorized},
		{"list votes", http.MethodGet, "/api/v1/admin/council/votes", "/api/v1/admin/council/votes", http.StatusUnauthorized},
		{"admin stats", http.MethodGet, "/api/v1/admin/stats", "/api/v1/admin/stats", http.StatusUnauthorized},
		{"admin config", http.MethodPut, "/api/v1/admin/config", "/api/v1/admin/config", http.StatusUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Registration, asked of the routing tree directly.
			got := mux.Find(chi.NewRouteContext(), tt.method, tt.path)
			if got == "" {
				t.Fatalf("%s %s is not registered", tt.method, tt.path)
			}
			if got != tt.pattern {
				t.Fatalf("%s %s resolves to %q, want %q", tt.method, tt.path, got, tt.pattern)
			}

			// Behaviour of the guard on that resolved route.
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, nil))

			if rec.Code != tt.want {
				t.Errorf("%s %s status = %d, want %d", tt.method, tt.path, rec.Code, tt.want)
			}
		})
	}
}

// The guard order is auth → active → role → rate limit, and every element is
// optional in a way that must not reorder the rest.
func TestProtected_ChainComposition(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	limit := &rateSpec{"posts", 10, time.Hour}

	tests := []struct {
		name string
		srv  *Server
		g    guard
		want int
	}{
		{
			name: "no auth and no rate limiter installs only the active check",
			srv:  &Server{},
			g:    guard{},
			want: 1,
		},
		{
			name: "skipActive with auth is authentication only",
			srv:  &Server{authMiddleware: func(next http.Handler) http.Handler { return next }},
			g:    guard{skipActive: true},
			want: 1,
		},
		{
			name: "skipActive without auth installs nothing",
			srv:  &Server{},
			g:    guard{skipActive: true},
			want: 0,
		},
		{
			name: "role adds a third link",
			srv:  &Server{authMiddleware: func(next http.Handler) http.Handler { return next }},
			g:    guard{role: domain.RoleMember},
			want: 3,
		},
		{
			name: "a rate spec is ignored when no limiter is configured",
			srv:  &Server{authMiddleware: func(next http.Handler) http.Handler { return next }},
			g:    guard{role: domain.RoleMember, limit: limit},
			want: 3,
		},
		{
			name: "full chain",
			srv: &Server{
				authMiddleware: func(next http.Handler) http.Handler { return next },
				rateLimiter:    middleware.NewRateLimiter(nil, logger),
			},
			g:    guard{role: domain.RoleCouncil, limit: limit},
			want: 4,
		},
		{
			// REQUIRE_VERIFIED_EMAIL adds a link, and adds it beside the active
			// check rather than in place of it.
			name: "verified email adds a link when enabled",
			srv: &Server{
				cfg:            config.Config{RequireVerifiedEmail: true},
				authMiddleware: func(next http.Handler) http.Handler { return next },
			},
			g:    guard{},
			want: 3,
		},
		{
			// And it follows skipActive: the one route that lets a user who may
			// not participate see their own status must not acquire the guard
			// that would stop them.
			name: "verified email is skipped wherever the active check is",
			srv: &Server{
				cfg:            config.Config{RequireVerifiedEmail: true},
				authMiddleware: func(next http.Handler) http.Handler { return next },
			},
			g:    guard{skipActive: true},
			want: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := len(tt.srv.protected(tt.g)); got != tt.want {
				t.Errorf("chain length = %d, want %d", got, tt.want)
			}
		})
	}
}

// newVerificationServer wires a server whose auth middleware injects a member
// along with the verification state Kratos would have reported, so the
// REQUIRE_VERIFIED_EMAIL gate can be exercised without a Kratos.
func newVerificationServer(t *testing.T, requireVerified, verified bool) *Server {
	t.Helper()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	member := &domain.User{ID: "u1", DisplayName: "Stub", Role: domain.RoleMember, IsActive: true}

	return New(config.Config{
		Port:                 0,
		ImageStoragePath:     t.TempDir(),
		RequireVerifiedEmail: requireVerified,
	}, nil, logger,
		WithUserService(service.NewUserService(stubUserRepo{}, nil)),
		WithAuth(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ctx := middleware.WithEmailVerified(middleware.WithUser(r.Context(), member), verified)
				next.ServeHTTP(w, r.WithContext(ctx))
			})
		}),
	)
}

// The flag is off by default and its whole reason for being off is that a town
// with no working SMTP would otherwise lock everyone out — so an unverified
// resident must behave exactly as they do today until an operator says
// otherwise.
func TestVerifiedEmailGate(t *testing.T) {
	tests := []struct {
		name            string
		requireVerified bool
		verified        bool
		path            string
		want            int
	}{
		{"flag off lets an unverified resident participate", false, false, "/api/v1/users/me", http.StatusOK},
		{"flag off lets them browse the directory", false, false, "/api/v1/users", http.StatusOK},
		{"flag on admits a verified resident", true, true, "/api/v1/users/me", http.StatusOK},
		{"flag on blocks an unverified resident", true, false, "/api/v1/users/me", http.StatusForbidden},
		{"flag on blocks them from the directory too", true, false, "/api/v1/users", http.StatusForbidden},

		// GET /v1/me is the exception, and it is the one that matters: a
		// resident who cannot participate has to be able to load the page that
		// tells them why. Blocking it would leave them staring at a failed
		// request with no explanation and no route to the verification flow.
		{"flag on still lets an unverified resident see their own status", true, false, "/api/v1/me", http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newVerificationServer(t, tt.requireVerified, tt.verified)
			rec := httptest.NewRecorder()
			srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tt.path, nil))

			if rec.Code != tt.want {
				t.Fatalf("GET %s status = %d, want %d; body: %s", tt.path, rec.Code, tt.want, rec.Body.String())
			}
			if tt.want == http.StatusForbidden && !strings.Contains(rec.Body.String(), "email not verified") {
				t.Errorf("rejection body = %s, want the distinct verification message", rec.Body.String())
			}
		})
	}
}

// The uploads route is the reason cacheOnSuccess exists: a real file gets the
// year-long directive, a missing one must not.
func TestUploadsCacheControl(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "photo.jpg"), []byte("bytes"), 0o600); err != nil {
		t.Fatalf("failed to write fixture: %v", err)
	}

	srv := newWiredServer(t, config.Config{Port: 0, ImageStoragePath: dir})
	h := srv.Handler()

	tests := []struct {
		name       string
		path       string
		wantStatus int
		wantCache  string
	}{
		{"existing upload is cached for a year", "/uploads/photo.jpg", http.StatusOK, "public, max-age=31536000"},
		{"missing upload is not cached", "/uploads/gone.jpg", http.StatusNotFound, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tt.path, nil))

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if got := rec.Header().Get("Cache-Control"); got != tt.wantCache {
				t.Errorf("Cache-Control = %q, want %q", got, tt.wantCache)
			}
		})
	}
}

// The SPA catch-all is registered only when web/dist exists relative to the
// working directory, so a backend-only build still 404s unknown paths instead
// of pretending to have a frontend.
func TestSPARouteRegistration(t *testing.T) {
	t.Run("registered when web/dist exists", func(t *testing.T) {
		root := t.TempDir()
		dist := filepath.Join(root, "web", "dist")
		if err := os.MkdirAll(dist, 0o750); err != nil {
			t.Fatalf("failed to create web/dist: %v", err)
		}
		writeFile(t, filepath.Join(dist, "index.html"), "<html>spa shell</html>")
		t.Chdir(root)

		srv := newWiredServer(t, config.Config{Port: 0, ImageStoragePath: t.TempDir()})
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/some/client/route", nil))

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}
		if body := rec.Body.String(); body != "<html>spa shell</html>" {
			t.Errorf("body = %q, want the SPA shell", body)
		}
	})

	t.Run("absent when web/dist does not exist", func(t *testing.T) {
		t.Chdir(t.TempDir())

		srv := newWiredServer(t, config.Config{Port: 0, ImageStoragePath: t.TempDir()})
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/some/client/route", nil))

		if rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
		}
	})
}

// End-to-end proof of the fail-safe: what the browser receives from /.ory/*
// keeps its Secure attribute unless the server was explicitly told it is a dev
// instance. bell.themacarthurs.ca runs on HTTPS, so stripping it there would
// expose every real session cookie to a downgrade.
func TestKratosProxyPreservesSecureCookies(t *testing.T) {
	const setCookie = "ory_kratos_session=abc; Path=/; HttpOnly; Secure; SameSite=Lax"

	kratos := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Set-Cookie", setCookie)
		w.Header().Add("Set-Cookie", "csrf_token=xyz; Path=/; Secure")
		w.WriteHeader(http.StatusOK)
	}))
	defer kratos.Close()

	tests := []struct {
		name string
		env  string
		want []string
	}{
		{
			name: "unset BELL_ENV keeps Secure",
			env:  "",
			want: []string{setCookie, "csrf_token=xyz; Path=/; Secure"},
		},
		{
			name: "production keeps Secure",
			env:  "production",
			want: []string{setCookie, "csrf_token=xyz; Path=/; Secure"},
		},
		{
			name: "dev strips Secure so plain-HTTP localhost can hold the session",
			env:  "dev",
			want: []string{
				"ory_kratos_session=abc; Path=/; HttpOnly; SameSite=Lax",
				"csrf_token=xyz; Path=/",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newWiredServer(t, config.Config{
				Port:             0,
				Env:              tt.env,
				KratosPublicURL:  kratos.URL,
				ImageStoragePath: t.TempDir(),
			})

			rec := httptest.NewRecorder()
			srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.ory/sessions/whoami", nil))

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
			}
			got := rec.Header().Values("Set-Cookie")
			if !slices.Equal(got, tt.want) {
				t.Errorf("Set-Cookie = %q, want %q", got, tt.want)
			}
		})
	}
}

// countingRateLimiterClient is a sliding window that never forgets, which is
// all a wiring test needs: the nth request to one key reports n.
type countingRateLimiterClient struct {
	mu     sync.Mutex
	counts map[string]int64
}

func (c *countingRateLimiterClient) SlidingWindowCount(_ context.Context, key string, _ time.Time, _ time.Duration) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.counts == nil {
		c.counts = make(map[string]int64)
	}
	c.counts[key]++
	return c.counts[key], nil
}

// The middleware tests prove the limiter works; this proves it is installed.
// The /.ory proxy is registered on its own, outside every guard the API routes
// share, so a limiter that exists and is never wired to it looks exactly like a
// working one until somebody scripts the registration endpoint.
func TestKratosProxyRateLimitsRegistration(t *testing.T) {
	kratos := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer kratos.Close()

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	srv := New(config.Config{Port: 0, KratosPublicURL: kratos.URL, ImageStoragePath: t.TempDir()}, nil, logger,
		WithRateLimiter(middleware.NewRateLimiter(&countingRateLimiterClient{}, logger)),
	)
	h := srv.Handler()

	// One more than the budget, from one address.
	var last int
	for range registrationMaxPerIP + 1 {
		req := httptest.NewRequest(http.MethodPost, "/.ory/self-service/registration", nil)
		req.RemoteAddr = "203.0.113.20:5000"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		last = rec.Code
	}
	if last != http.StatusTooManyRequests {
		t.Errorf("request %d to the registration endpoint = %d, want %d — the proxy is not rate limited",
			registrationMaxPerIP+1, last, http.StatusTooManyRequests)
	}

	// The session check a signed-in resident makes on every page load shares
	// the proxy route and must not share the budget.
	for i := range registrationMaxPerIP + 5 {
		req := httptest.NewRequest(http.MethodGet, "/.ory/sessions/whoami", nil)
		req.RemoteAddr = "203.0.113.20:5000"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code == http.StatusTooManyRequests {
			t.Fatalf("session check %d was rate limited", i+1)
		}
	}
}

// unusableKratosURL is rejected by config.Load, so a real deployment can never
// reach routes() carrying it. It is shared with the test below that pins that
// guarantee.
const unusableKratosURL = "http://[::1]:namedport"

// A KratosPublicURL that will not parse leaves the browser with no way to
// reach Kratos, so it is now refused at config load and the process does not
// start — see TestKratosProxyRegistration_UnusableURLIsRejectedAtLoad and
// config.TestLoad_RejectsUnusableKratosPublicURL.
//
// What is left here is defence in depth for a Config built directly rather
// than through Load (tests do this, and so would any future in-process
// wiring): the route must still not be registered against a half-built proxy.
// The empty case is different and unchanged — the proxy is genuinely optional
// when the URL is unset.
func TestKratosProxyRegistration(t *testing.T) {
	tests := []struct {
		name           string
		kratosURL      string
		wantRegistered bool
	}{
		{"valid URL installs the proxy", "http://127.0.0.1:1", true},
		{"unparseable URL leaves the route off", unusableKratosURL, false},
		{"empty URL leaves the route off", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newWiredServer(t, config.Config{
				Port:             0,
				KratosPublicURL:  tt.kratosURL,
				ImageStoragePath: t.TempDir(),
			})

			rec := httptest.NewRecorder()
			srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.ory/sessions/whoami", nil))

			// A registered proxy cannot reach the fake host, so it answers 502;
			// an unregistered one falls through to chi's 404.
			registered := rec.Code != http.StatusNotFound
			if registered != tt.wantRegistered {
				t.Errorf("proxy registered = %v (status %d), want %v", registered, rec.Code, tt.wantRegistered)
			}
		})
	}
}

// The case above only stays defence in depth for as long as config.Load
// actually refuses the value. If that check were ever dropped, a bad
// KRATOS_PUBLIC_URL would silently become "server starts, nobody can log in"
// again — with the wiring test above still green, since it asserts exactly
// that behaviour. This pins the two layers together.
func TestKratosProxyRegistration_UnusableURLIsRejectedAtLoad(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost:5432/bell")
	t.Setenv("KRATOS_ADMIN_URL", "http://localhost:4434")
	t.Setenv("KRATOS_PUBLIC_URL", unusableKratosURL)

	if _, err := config.Load(); err == nil {
		t.Fatalf("config.Load() accepted %q; the server-level fallback is not a substitute for failing fast", unusableKratosURL)
	}
}

// reactingEnricher reports that the caller has bell-reacted to every post it
// is asked about, so a populated user_reactions field is unambiguous.
type reactingEnricher struct{}

func (reactingEnricher) BatchCountByPosts(_ context.Context, ids []string) (map[string]map[domain.ReactionType]int, error) {
	counts := make(map[string]map[domain.ReactionType]int, len(ids))
	for _, id := range ids {
		counts[id] = map[domain.ReactionType]int{domain.ReactionBell: 1}
	}
	return counts, nil
}

func (reactingEnricher) BatchGetUserReactions(_ context.Context, _ string, ids []string) (map[string][]domain.ReactionType, error) {
	reactions := make(map[string][]domain.ReactionType, len(ids))
	for _, id := range ids {
		reactions[id] = []domain.ReactionType{domain.ReactionBell}
	}
	return reactions, nil
}

// feedPostRepo returns one visible post, so the feed has something to enrich.
type feedPostRepo struct{ stubPostRepo }

func (feedPostRepo) ListPosts(context.Context, string, int) ([]*domain.Post, error) {
	return []*domain.Post{{ID: "p1", AuthorID: "u1", Body: "hello", Status: domain.PostVisible}}, nil
}

// newFeedServer wires a server whose optional-auth middleware injects viewer,
// standing in for a request that arrived with a valid session cookie. A nil
// viewer models a logged-out reader.
func newFeedServer(t *testing.T, viewer *domain.User) *Server {
	t.Helper()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))

	return New(config.Config{Port: 0, ImageStoragePath: t.TempDir()}, nil, logger,
		WithPostService(service.NewPostService(feedPostRepo{}, nil)),
		WithUserService(service.NewUserService(stubUserRepo{}, nil)),
		WithReactionRepo(reactingEnricher{}),
		WithOptionalAuth(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if viewer != nil {
					r = r.WithContext(middleware.WithUser(r.Context(), viewer))
				}
				next.ServeHTTP(w, r)
			})
		}),
	)
}

// The regression test for a live, user-visible bug: enrichPosts reads the
// caller from the request context, but GET /v1/posts carried no auth
// middleware at all, so it always saw an anonymous reader and never attached
// user_reactions. PostCard reads exactly that field, so a member's own
// reactions rendered as inactive on every refresh. The batch query and the
// enrichment were both correct and both tested — merely unreachable.
func TestFeedCarriesTheCallersOwnReactions(t *testing.T) {
	viewer := &domain.User{ID: "u1", Role: domain.RoleMember, IsActive: true}
	srv := newFeedServer(t, viewer)

	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/posts", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "user_reactions") {
		t.Errorf("feed carried no user_reactions for a signed-in caller: %s", rec.Body.String())
	}
}

// The same route must still serve a logged-out reader, without the
// personalized field.
func TestFeedStillServesAnonymousReaders(t *testing.T) {
	srv := newFeedServer(t, nil)

	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/posts", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if strings.Contains(rec.Body.String(), "user_reactions") {
		t.Errorf("anonymous feed carried user_reactions: %s", rec.Body.String())
	}
}

// newRoleServer wires a server whose auth middleware injects viewer, so a route
// group's role floor can be exercised directly. A nil viewer is an
// unauthenticated request.
func newRoleServer(t *testing.T, viewer *domain.User) *Server {
	t.Helper()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))

	return New(config.Config{Port: 0, ImageStoragePath: t.TempDir()}, nil, logger,
		WithUserService(service.NewUserService(stubUserRepo{}, nil)),
		WithVouchService(service.NewVouchService(stubVouchRepo{}, nil, nil, nil)),
		WithApprovalService(service.NewApprovalService(nil, nil)),
		WithAuth(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if viewer != nil {
					r = r.WithContext(middleware.WithUser(r.Context(), viewer))
				}
				next.ServeHTTP(w, r)
			})
		}),
	)
}

// Registering member vouching and council approval under one prefix has to be
// two Groups inside one Route. A second r.Route on the same pattern makes chi
// panic while the router is being built — a boot-time crash, which is the kind
// of failure that should be caught here rather than on deploy.
func TestVouchRoutesBuildWithoutPanicking(t *testing.T) {
	defer func() {
		if rec := recover(); rec != nil {
			t.Fatalf("building the router panicked with both vouch services present: %v", rec)
		}
	}()

	srv := newRoleServer(t, nil)
	if srv.Handler() == nil {
		t.Fatal("router built to nil")
	}
}

// The point of the shared prefix is that it carries two different guards, so
// each is checked from both sides: the role that must pass, and the role that
// must not.
func TestVouchRoutesCarryTwoDifferentGuards(t *testing.T) {
	member := &domain.User{ID: "m1", Role: domain.RoleMember, IsActive: true}
	council := &domain.User{ID: "c1", Role: domain.RoleCouncil, IsActive: true}
	pending := &domain.User{ID: "p1", Role: domain.RolePending, IsActive: true}

	tests := []struct {
		name       string
		viewer     *domain.User
		method     string
		path       string
		wantAllowd bool // false means the role guard must reject with 403
	}{
		// Member vouching: a member is enough.
		{"a member may vouch", member, http.MethodPost, "/api/v1/vouches/", true},
		{"a member may revoke a vouch", member, http.MethodDelete, "/api/v1/vouches/v1", true},
		{"a pending user may not vouch", pending, http.MethodPost, "/api/v1/vouches/", false},
		// Council outranks member, so the floor admits them too.
		{"council may vouch", council, http.MethodPost, "/api/v1/vouches/", true},

		// Council approval: a member is NOT enough, which is the whole reason
		// the two groups cannot share one guard.
		{"a member may not approve", member, http.MethodPost, "/api/v1/vouches/approve/u1", false},
		{"a member may not list pending approvals", member, http.MethodGet, "/api/v1/vouches/pending", false},
		{"council may approve", council, http.MethodPost, "/api/v1/vouches/approve/u1", true},
		{"council may list pending approvals", council, http.MethodGet, "/api/v1/vouches/pending", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newRoleServer(t, tt.viewer)
			rec := httptest.NewRecorder()
			srv.Handler().ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, strings.NewReader(`{}`)))

			if rec.Code == http.StatusNotFound {
				t.Fatalf("%s %s returned 404 — the route is not registered", tt.method, tt.path)
			}
			if tt.wantAllowd && rec.Code == http.StatusForbidden {
				t.Errorf("%s %s was rejected as forbidden for role %q", tt.method, tt.path, tt.viewer.Role)
			}
			if !tt.wantAllowd && rec.Code != http.StatusForbidden {
				t.Errorf("%s %s status = %d for role %q, want %d", tt.method, tt.path, rec.Code, tt.viewer.Role, http.StatusForbidden)
			}
		})
	}
}

// recordingPostRepo captures the status write, so a test can tell a route that
// reached its handler from one that merely got past the guard.
type recordingPostRepo struct {
	stubPostRepo
	status    domain.PostStatus
	reason    string
	removedBy string
}

func (r *recordingPostRepo) UpdatePostStatus(_ context.Context, _ string, status domain.PostStatus, reason, removedBy string) error {
	r.status, r.reason, r.removedBy = status, reason, removedBy
	return nil
}

// TestRoutesAreRegistered cannot prove a route under /v1/moderation exists.
// chi runs a subrouter's middleware before it resolves the path within it, so
// the group's own guard answers 401 for a path that was never registered —
// every moderation entry in that table passes whether or not its route is
// wired. This test closes that hole for the removal route by supplying a
// moderator, so only a route that actually resolves can reach the handler.
func TestModerationPostRemovalRouteResolves(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	repo := &recordingPostRepo{}
	moderator := &domain.User{ID: "mod-1", Role: domain.RoleModerator, IsActive: true}

	srv := New(config.Config{Port: 0, ImageStoragePath: t.TempDir()}, nil, logger,
		WithPostService(service.NewPostService(repo, nil)),
		WithAuth(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				next.ServeHTTP(w, r.WithContext(middleware.WithUser(r.Context(), moderator)))
			})
		}),
	)

	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/moderation/posts/p1/remove", strings.NewReader(`{"reason":"harassment"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d (body %s)", rec.Code, http.StatusNoContent, rec.Body.String())
	}
	if repo.status != domain.PostRemovedByMod {
		t.Errorf("status written = %q, want %q", repo.status, domain.PostRemovedByMod)
	}
	if repo.reason != "harassment" {
		t.Errorf("reason written = %q, want %q", repo.reason, "harassment")
	}
}

// The removal route must be registered on the post service alone. Hanging it
// off the report or moderation-action branch would mean a deployment without
// those silently loses the ability to take a post down.
func TestModerationPostRemovalRouteNeedsOnlyThePostService(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))

	srv := New(config.Config{Port: 0, ImageStoragePath: t.TempDir()}, nil, logger,
		WithPostService(service.NewPostService(stubPostRepo{}, nil)),
	)

	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/moderation/posts/p1/remove", strings.NewReader(`{"reason":"spam"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code == http.StatusNotFound || rec.Code == http.StatusMethodNotAllowed {
		t.Fatalf("route is not registered without the report service: status = %d", rec.Code)
	}
}

// A member who reaches the route is refused. The service checks the role too,
// but the route guard is what keeps a non-moderator from ever reaching it.
func TestModerationPostRemovalRouteRefusesAMember(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	repo := &recordingPostRepo{}
	member := &domain.User{ID: "u1", Role: domain.RoleMember, IsActive: true}

	srv := New(config.Config{Port: 0, ImageStoragePath: t.TempDir()}, nil, logger,
		WithPostService(service.NewPostService(repo, nil)),
		WithAuth(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				next.ServeHTTP(w, r.WithContext(middleware.WithUser(r.Context(), member)))
			})
		}),
	)

	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/moderation/posts/p1/remove", strings.NewReader(`{"reason":"spam"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
	if repo.status != "" {
		t.Errorf("a member's removal reached the repository: status written = %q", repo.status)
	}
}
