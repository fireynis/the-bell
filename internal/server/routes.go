package server

import (
	"io/fs"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/handler"
	"github.com/fireynis/the-bell/internal/middleware"
)

func (s *Server) routes() http.Handler {
	r := chi.NewRouter()
	// RequestLogger first so Recoverer can see the statusWriter and tell whether
	// a response was already committed before the panic.
	r.Use(middleware.RequestLogger(s.logger))
	r.Use(middleware.Recoverer(s.logger))
	r.Get("/healthz", handler.Health)

	// SSE endpoint — registered before /api to avoid ContentTypeJSON middleware.
	if s.sseBroker != nil {
		sseH := handler.NewSSEHandler(s.sseBroker)
		r.Route("/api/v1/feed/live", func(r chi.Router) {
			r.Use(s.protected(guard{})...)
			r.Get("/", sseH.ServeFeed)
		})
	}

	// API routes — all JSON.
	r.Route("/api", func(r chi.Router) {
		r.Use(middleware.ContentTypeJSON)
		s.apiRoutes(r)
	})

	// Kratos reverse proxy — browser talks to /.ory/*, we forward to Kratos.
	if s.cfg.KratosPublicURL != "" {
		kratosTarget, err := url.Parse(s.cfg.KratosPublicURL)
		if err != nil {
			// The proxy is the browser's only route to Kratos, so without it
			// nobody can log in. The server still starts (health checks and the
			// SPA shell keep working), but the misconfiguration must be loud
			// rather than a silently missing route.
			s.logger.Error("kratos proxy not installed: invalid KRATOS_PUBLIC_URL",
				"url", s.cfg.KratosPublicURL, "error", err)
		} else {
			proxy := httputil.NewSingleHostReverseProxy(kratosTarget)
			// Decided once at wiring time: the environment cannot change under a
			// running server, and re-reading config per response invites drift.
			allowInsecureCookies := s.cfg.IsDev()
			proxy.ModifyResponse = func(resp *http.Response) error {
				cookies := resp.Header.Values("Set-Cookie")
				rewritten := rewriteSetCookies(cookies, allowInsecureCookies)
				if !slices.Equal(cookies, rewritten) {
					resp.Header.Del("Set-Cookie")
					for _, c := range rewritten {
						resp.Header.Add("Set-Cookie", c)
					}
				}
				return nil
			}
			r.HandleFunc("/.ory/*", func(w http.ResponseWriter, req *http.Request) {
				req.URL.Path = strings.TrimPrefix(req.URL.Path, "/.ory")
				if req.URL.Path == "" {
					req.URL.Path = "/"
				}
				proxy.ServeHTTP(w, req)
			})
		}
	}

	// Static file serving for uploaded images.
	//
	// This reads ImageStoragePath directly instead of going through
	// s.imageStore, because storage.Storage can Save an object and build a URL
	// for it but cannot open one — there is no method to serve through.
	//
	// The condition is therefore on the directory actually being served. It
	// used to be `s.imageStore != nil`, which implied a coupling that does not
	// exist: the store was never consulted, so a non-local (say S3) store still
	// produced a live /uploads route reading from whatever ImageStoragePath
	// happened to be. Gating on the real input makes the route's existence and
	// its behaviour agree. Serving through the interface needs a read method on
	// storage.Storage.
	if s.cfg.ImageStoragePath != "" {
		fileServer := http.StripPrefix("/uploads/", http.FileServer(fileOnlyFS{http.Dir(s.cfg.ImageStoragePath)}))
		r.Get("/uploads/*", func(w http.ResponseWriter, r *http.Request) {
			// Upload filenames are content-derived and never reused, so a hit is
			// safe to cache for a year. A miss must not be: caching a 404 would
			// hide an image that is uploaded to that path moments later.
			fileServer.ServeHTTP(&cacheOnSuccess{ResponseWriter: w}, r)
		})
	}

	// Serve SPA frontend (web/dist).
	spaDir := "web/dist"
	if _, err := os.Stat(spaDir); err == nil {
		r.Get("/*", spaHandler(spaDir))
	}

	return r
}

// rateSpec is a per-user sliding-window rate limit applied to a route group.
type rateSpec struct {
	endpoint string
	max      int
	window   time.Duration
}

// guard describes what a route group requires of its caller. The zero value is
// the safe one: authenticated and active, with no role floor and no rate limit.
type guard struct {
	role  domain.Role
	limit *rateSpec

	// skipActive drops the RequireActive check, letting suspended and banned
	// users through.
	//
	// It is deliberately phrased as an opt-out. When this was `active bool`,
	// the zero value skipped the check, so a new route group that simply
	// forgot to say `active: true` lost it silently and no test would notice.
	// Inverted, forgetting is safe and the exception has to be written down —
	// which also makes every exception greppable.
	skipActive bool
}

// protected builds the middleware chain for a guarded route group, always in
// the order auth → active → role → rate limit.
//
// Auth and the rate limiter are installed only when configured so the server
// still boots without Kratos or Redis (tests and the setup wizard rely on it).
func (s *Server) protected(g guard) []func(http.Handler) http.Handler {
	var mws []func(http.Handler) http.Handler
	if s.authMiddleware != nil {
		mws = append(mws, s.authMiddleware)
	}
	if !g.skipActive {
		mws = append(mws, middleware.RequireActive)
	}
	if g.role != "" {
		mws = append(mws, middleware.RequireRole(g.role))
	}
	if g.limit != nil && s.rateLimiter != nil {
		mws = append(mws, s.rateLimiter.Limit(g.limit.endpoint, g.limit.max, g.limit.window))
	}
	return mws
}

func (s *Server) apiRoutes(r chi.Router) {
	// Handlers are stateless over their services, so each is built once here
	// and shared by every route group that needs it.
	uh := handler.NewUserHandler(s.userService, s.postService, s.vouchService)
	var reportH *handler.ReportHandler
	if s.reportService != nil {
		reportH = handler.NewReportHandler(s.reportService)
	}

	// GET /api/v1/me — return the authenticated user.
	// Intentionally skips RequireActive so suspended/banned users can still
	// learn their own status and role (the frontend RequireRole guard needs this).
	r.Route("/v1/me", func(r chi.Router) {
		r.Use(s.protected(guard{skipActive: true})...)
		r.Get("/", uh.GetMe)
	})

	if s.postService != nil {
		var phOpts []handler.PostHandlerOption
		if s.imageStore != nil {
			phOpts = append(phOpts, handler.WithStorage(s.imageStore))
		}
		if s.reactionRepo != nil {
			phOpts = append(phOpts, handler.WithReactionEnricher(s.reactionRepo))
		}
		if s.sseBroker != nil {
			phOpts = append(phOpts, handler.WithPostPublisher(s.sseBroker))
		}
		ph := handler.NewPostHandler(s.postService, phOpts...)
		r.Route("/v1/posts", func(r chi.Router) {
			// Public reads, but personalized: the response depends on who is
			// asking. Without a user in context the handler cannot attach the
			// caller's own reactions, and cannot tell an author or moderator
			// from a stranger when deciding whether a removed post is theirs
			// to see. OptionalAuth supplies identity when there is one and
			// lets anonymous readers through untouched.
			r.Group(func(r chi.Router) {
				if s.optionalAuth != nil {
					r.Use(s.optionalAuth)
				}
				r.Get("/", ph.ListFeed)
				r.Get("/{id}", ph.GetByID)
			})

			r.Group(func(r chi.Router) {
				r.Use(s.protected(guard{
					role:  domain.RoleMember,
					limit: &rateSpec{"posts", 10, time.Hour},
				})...)
				r.Post("/", ph.Create)
				r.Patch("/{id}", ph.Update)
				r.Delete("/{id}", ph.Delete)
			})
		})
	}

	if s.reactionService != nil {
		var rhOpts []handler.ReactionHandlerOption
		if s.sseBroker != nil {
			rhOpts = append(rhOpts, handler.WithReactionPublisher(s.sseBroker))
		}
		rh := handler.NewReactionHandler(s.reactionService, s.postService, rhOpts...)
		r.Route("/v1/posts/{postId}/reactions", func(r chi.Router) {
			r.Use(s.protected(guard{
				role:  domain.RoleMember,
				limit: &rateSpec{"reactions", 60, time.Minute},
			})...)
			r.Post("/", rh.Add)
			r.Delete("/{type}", rh.Remove)
		})
	}

	if s.userService != nil {
		r.Route("/v1/users", func(r chi.Router) {
			r.Group(func(r chi.Router) {
				r.Use(s.protected(guard{})...)
				r.Get("/me", uh.GetMe)
				r.Put("/me", uh.UpdateMe)
			})

			r.Get("/{id}", uh.GetByID)
			r.Get("/{id}/posts", uh.ListPosts)
			r.Get("/{id}/vouches", uh.ListVouches)
		})
	}

	if reportH != nil {
		r.Route("/v1/posts/{id}/report", func(r chi.Router) {
			r.Use(s.protected(guard{
				role:  domain.RoleMember,
				limit: &rateSpec{"reports", 5, time.Hour},
			})...)
			r.Post("/", reportH.SubmitReport)
		})
	}

	if reportH != nil || s.moderationActionService != nil {
		r.Route("/v1/moderation", func(r chi.Router) {
			r.Use(s.protected(guard{role: domain.RoleModerator})...)

			if reportH != nil {
				r.Get("/queue", reportH.ListQueue)
				r.Patch("/reports/{id}", reportH.UpdateReportStatus)
			}

			if s.moderationActionService != nil {
				mh := handler.NewModerationHandler(s.moderationActionService)
				r.Post("/actions", mh.TakeAction)
				r.Get("/actions/{user_id}", mh.ListActions)
			}
		})
	}

	if s.approvalService != nil {
		ah := handler.NewApprovalHandler(s.approvalService)
		r.Route("/v1/vouches", func(r chi.Router) {
			r.Use(s.protected(guard{
				role:  domain.RoleCouncil,
				limit: &rateSpec{"vouches", 3, 24 * time.Hour},
			})...)
			r.Get("/pending", ah.ListPending)
			r.Post("/approve/{id}", ah.Approve)
		})
	}

	if s.votingService != nil {
		vh := handler.NewVotingHandler(s.votingService)
		r.Route("/v1/admin/council/votes", func(r chi.Router) {
			r.Use(s.protected(guard{role: domain.RoleCouncil})...)
			r.Post("/", vh.CastVote)
			r.Get("/", vh.ListPending)
		})
	}

	if s.statsService != nil {
		sh := handler.NewStatsHandler(s.statsService)
		r.Route("/v1/admin/stats", func(r chi.Router) {
			r.Use(s.protected(guard{role: domain.RoleCouncil})...)
			r.Get("/", sh.GetStats)
		})
	}

	if s.configRepo != nil {
		ch := handler.NewConfigHandler(s.configRepo, s.transactor)
		r.Get("/v1/config", ch.GetConfig)

		r.Route("/v1/admin/config", func(r chi.Router) {
			r.Use(s.protected(guard{role: domain.RoleCouncil})...)
			r.Put("/", ch.UpdateConfig)
		})
	}
}

// rewriteSetCookies returns the Set-Cookie header values to pass on to the
// browser. When allowInsecure is false — which is every environment except a
// local dev stack — the cookies are returned exactly as Kratos wrote them.
//
// Only a dev server on plain HTTP needs the Secure attribute gone; doing it in
// production hands the session cookie to any downgrade attacker. The caller
// supplies the decision so this stays a pure function over header values.
func rewriteSetCookies(cookies []string, allowInsecure bool) []string {
	if !allowInsecure || len(cookies) == 0 {
		return cookies
	}
	out := make([]string, len(cookies))
	for i, c := range cookies {
		out[i] = stripSecureAttr(c)
	}
	return out
}

// stripSecureAttr removes the Secure attribute from one Set-Cookie value.
//
// It drops whole ';'-separated attributes that are exactly "Secure" (attribute
// names are case-insensitive per RFC 6265) rather than doing a substring
// replace, so a cookie carrying "SecureFlag=no" or "Path=/Secure" survives
// intact instead of being silently corrupted.
func stripSecureAttr(cookie string) string {
	parts := strings.Split(cookie, ";")
	kept := make([]string, 0, len(parts))
	for _, p := range parts {
		if strings.EqualFold(strings.TrimSpace(p), "Secure") {
			continue
		}
		kept = append(kept, p)
	}
	return strings.Join(kept, ";")
}

// fileOnlyFS serves regular files and reports directories as missing.
//
// http.FileServer renders an index page for any directory without an
// index.html, so GET /uploads/ would otherwise hand an unauthenticated caller
// a link to every image ever uploaded. Nothing needs to browse the directory —
// clients only ever follow a URL built by storage.Storage.URL for one known
// file — so the listing is pure exposure.
type fileOnlyFS struct {
	fs http.FileSystem
}

func (f fileOnlyFS) Open(name string) (http.File, error) {
	file, err := f.fs.Open(name)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}
	if info.IsDir() {
		file.Close()
		// ErrNotExist so FileServer answers 404 rather than 403: whether a
		// directory exists is itself information the caller has no use for.
		return nil, fs.ErrNotExist
	}
	return file, nil
}

// cacheOnSuccess adds the long-lived Cache-Control header only once the
// wrapped handler has committed to a cacheable status, so error responses are
// not cached along with the files that do exist.
type cacheOnSuccess struct {
	http.ResponseWriter
	wroteHeader bool
}

func (w *cacheOnSuccess) WriteHeader(status int) {
	if !w.wroteHeader {
		w.wroteHeader = true
		// 304 is deliberate, not an oversight. http.FileServer answers a
		// conditional request (If-None-Match / If-Modified-Since) for a file
		// that still exists with 304 and no body, so a pure-2xx rule would
		// drop Cache-Control on exactly the revalidations that are supposed to
		// renew it — the browser would then re-ask on every subsequent load.
		if (status >= 200 && status < 300) || status == http.StatusNotModified {
			w.Header().Set("Cache-Control", "public, max-age=31536000")
		}
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *cacheOnSuccess) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

// spaHandler serves static files from dir, falling back to index.html
// for SPA client-side routing.
func spaHandler(dir string) http.HandlerFunc {
	fileServer := http.FileServer(http.Dir(dir))
	return func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			http.ServeFile(w, r, filepath.Join(dir, "index.html"))
			return
		}
		_, err := fs.Stat(os.DirFS(dir), path)
		if err != nil {
			http.ServeFile(w, r, filepath.Join(dir, "index.html"))
			return
		}
		fileServer.ServeHTTP(w, r)
	}
}
