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
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/config"
	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/middleware"
	"github.com/fireynis/the-bell/internal/service"
	"github.com/fireynis/the-bell/internal/sse"
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
func (stubPostRepo) UpdatePostStatus(context.Context, string, domain.PostStatus, string) error {
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

type stubStorage struct{}

func (stubStorage) Save(context.Context, string, io.Reader) (string, error) { return "", nil }
func (stubStorage) URL(path string) string                                  { return "/uploads/" + path }

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
func newWiredServer(t *testing.T, cfg config.Config) *Server {
	t.Helper()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))

	return New(cfg, nil, logger,
		WithPostService(service.NewPostService(stubPostRepo{}, nil)),
		WithUserService(service.NewUserService(stubUserRepo{}, nil)),
		WithVouchService(service.NewVouchService(stubVouchRepo{}, nil, nil, nil)),
		WithReportService(service.NewReportService(nil, nil, nil)),
		WithModerationActionService(service.NewModerationActionService(nil, nil, nil, nil, nil, nil)),
		WithApprovalService(service.NewApprovalService(nil, nil)),
		WithVotingService(service.NewVotingService(nil, nil)),
		WithReactionService(service.NewReactionService(nil, nil)),
		WithStatsService(service.NewStatsService(nil)),
		WithConfigRepo(stubConfigRepo{}),
		WithReactionRepo(stubReactionEnricher{}),
		WithSSEBroker(sse.NewBroker(nil, logger)),
		WithImageStore(stubStorage{}),
		WithRateLimiter(middleware.NewRateLimiter(nil, logger)),
		WithAuth(func(next http.Handler) http.Handler { return next }),
	)
}

// Every route below is registered somewhere in routes/apiRoutes. The point of
// the test is the absence of 404: a 401 or 403 proves the path resolved and
// the guard ran, whereas a 404 means the route silently stopped existing.
func TestRoutesAreRegistered(t *testing.T) {
	srv := newWiredServer(t, config.Config{Port: 0, ImageStoragePath: t.TempDir()})
	h := srv.Handler()

	tests := []struct {
		name   string
		method string
		path   string
		want   int
	}{
		{"health", http.MethodGet, "/healthz", http.StatusOK},

		// Public reads — no session required.
		{"feed", http.MethodGet, "/api/v1/posts", http.StatusOK},
		{"single post", http.MethodGet, "/api/v1/posts/p1", http.StatusOK},
		{"user profile", http.MethodGet, "/api/v1/users/u1", http.StatusOK},
		{"user posts", http.MethodGet, "/api/v1/users/u1/posts", http.StatusOK},
		{"user vouches", http.MethodGet, "/api/v1/users/u1/vouches", http.StatusOK},
		{"town config", http.MethodGet, "/api/v1/config", http.StatusOK},

		// Guarded routes — rejected without a user in context.
		{"live feed", http.MethodGet, "/api/v1/feed/live", http.StatusUnauthorized},
		{"me", http.MethodGet, "/api/v1/me", http.StatusUnauthorized},
		{"create post", http.MethodPost, "/api/v1/posts", http.StatusUnauthorized},
		{"edit post", http.MethodPatch, "/api/v1/posts/p1", http.StatusUnauthorized},
		{"delete post", http.MethodDelete, "/api/v1/posts/p1", http.StatusUnauthorized},
		{"add reaction", http.MethodPost, "/api/v1/posts/p1/reactions", http.StatusUnauthorized},
		{"remove reaction", http.MethodDelete, "/api/v1/posts/p1/reactions/like", http.StatusUnauthorized},
		{"users me", http.MethodGet, "/api/v1/users/me", http.StatusUnauthorized},
		{"update users me", http.MethodPut, "/api/v1/users/me", http.StatusUnauthorized},
		{"report post", http.MethodPost, "/api/v1/posts/p1/report", http.StatusUnauthorized},
		{"moderation queue", http.MethodGet, "/api/v1/moderation/queue", http.StatusUnauthorized},
		{"update report", http.MethodPatch, "/api/v1/moderation/reports/r1", http.StatusUnauthorized},
		{"moderation action", http.MethodPost, "/api/v1/moderation/actions", http.StatusUnauthorized},
		{"list actions", http.MethodGet, "/api/v1/moderation/actions/u1", http.StatusUnauthorized},
		{"pending vouches", http.MethodGet, "/api/v1/vouches/pending", http.StatusUnauthorized},
		{"approve vouch", http.MethodPost, "/api/v1/vouches/approve/v1", http.StatusUnauthorized},
		{"cast vote", http.MethodPost, "/api/v1/admin/council/votes", http.StatusUnauthorized},
		{"list votes", http.MethodGet, "/api/v1/admin/council/votes", http.StatusUnauthorized},
		{"admin stats", http.MethodGet, "/api/v1/admin/stats", http.StatusUnauthorized},
		{"admin config", http.MethodPut, "/api/v1/admin/config", http.StatusUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, nil))

			if rec.Code == http.StatusNotFound || rec.Code == http.StatusMethodNotAllowed {
				t.Fatalf("%s %s is not registered: status = %d", tt.method, tt.path, rec.Code)
			}
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := len(tt.srv.protected(tt.g)); got != tt.want {
				t.Errorf("chain length = %d, want %d", got, tt.want)
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
