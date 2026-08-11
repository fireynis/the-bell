package server

import (
	"errors"
	"io/fs"
	"log/slog"
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
	"github.com/fireynis/the-bell/internal/storage"
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

	// Static file serving for uploaded images, read back through the same store
	// that wrote them.
	//
	// This used to be http.FileServer over http.Dir(cfg.ImageStoragePath),
	// because storage.Storage could Save an object and build a URL for it but
	// not open one. The route and the store were then decoupled by
	// construction: the store was never consulted, so a non-local (say S3)
	// store still produced a live /uploads route reading whatever local
	// directory ImageStoragePath happened to name — and a misconfigured store
	// still yielded a live route. storage.Storage.Open closes that, and the
	// gate goes back to the store, which is now the thing actually serving.
	if s.imageStore != nil {
		r.Get("/uploads/*", uploadHandler(s.imageStore, s.logger))
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
	// Built here rather than inside the /v1/posts block because moderator post
	// removal lives under /v1/moderation and needs the same handler.
	var postH *handler.PostHandler
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
		postH = handler.NewPostHandler(s.postService, phOpts...)
	}

	// GET /api/v1/me — return the authenticated user.
	// Intentionally skips RequireActive so suspended/banned users can still
	// learn their own status and role (the frontend RequireRole guard needs this).
	r.Route("/v1/me", func(r chi.Router) {
		r.Use(s.protected(guard{skipActive: true})...)
		r.Get("/", uh.GetMe)
	})

	if postH != nil {
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
				r.Get("/", postH.ListFeed)
				r.Get("/{id}", postH.GetByID)
			})

			r.Group(func(r chi.Router) {
				r.Use(s.protected(guard{
					role:  domain.RoleMember,
					limit: &rateSpec{"posts", 10, time.Hour},
				})...)
				r.Post("/", postH.Create)
				r.Patch("/{id}", postH.Update)
				r.Delete("/{id}", postH.Delete)
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

	if reportH != nil || s.moderationActionService != nil || postH != nil {
		r.Route("/v1/moderation", func(r chi.Router) {
			r.Use(s.protected(guard{role: domain.RoleModerator})...)

			if reportH != nil {
				r.Get("/queue", reportH.ListQueue)
				r.Patch("/reports/{id}", reportH.UpdateReportStatus)
			}

			// Taking a post down needs only the post service, so it is
			// registered on its own branch: a deployment without reports or
			// moderation actions must not lose the remedy for the post itself.
			if postH != nil {
				r.Post("/posts/{id}/remove", postH.RemoveByModerator)
			}

			if s.moderationActionService != nil {
				mh := handler.NewModerationHandler(s.moderationActionService)
				r.Post("/actions", mh.TakeAction)
				r.Get("/actions/{user_id}", mh.ListActions)
				// Lifting a mute is a DELETE of the mute itself, not another
				// action taken against the person: it writes no
				// moderation_actions row, so it does not belong under
				// /actions. The group's moderator guard covers it, and the
				// service re-checks the role regardless.
				r.Get("/users/{user_id}/mute", mh.MuteStatus)
				r.Delete("/users/{user_id}/mute", mh.LiftMute)
			}
		})
	}

	// Member vouching and council approval share the /v1/vouches prefix but not
	// their guards, so they are two Groups inside one Route: a second
	// r.Route on the same pattern panics at startup, when chi refuses to Mount
	// twice on one path.
	if s.approvalService != nil || s.vouchService != nil {
		r.Route("/v1/vouches", func(r chi.Router) {
			if s.approvalService != nil {
				ah := handler.NewApprovalHandler(s.approvalService)
				r.Group(func(r chi.Router) {
					// Deliberately unlimited. This group used to carry the
					// "vouches" limiter, which capped council approvals at 3
					// per day — but bootstrapExitThreshold is 20, so standing
					// up a town could not finish in under a week and the
					// operator saw an unexplained 429 on the fourth approval.
					// The limit belongs to the member budget below, and landed
					// here only because the two share a prefix.
					r.Use(s.protected(guard{role: domain.RoleCouncil})...)
					r.Get("/pending", ah.ListPending)
					r.Post("/approve/{id}", ah.Approve)
				})
			}

			if s.vouchService != nil {
				vh := handler.NewVouchHandler(s.vouchService)
				r.Group(func(r chi.Router) {
					// The limiter keys on the endpoint NAME, not the route, so
					// "vouches" is the per-member vouching budget and nothing
					// else may reuse it. Naming it here again on the approval
					// group would put a member's vouches and a council
					// member's approvals in one shared bucket.
					r.Use(s.protected(guard{
						role:  domain.RoleMember,
						limit: &rateSpec{"vouches", 3, 24 * time.Hour},
					})...)
					r.Post("/", vh.Create)
					r.Delete("/{id}", vh.Revoke)
				})
			}
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

// uploadHandler serves one stored upload per request, through storage.Storage.
//
// Every failure to open is one 404 with one body. That is deliberate and it is
// what replaces fileOnlyFS, which existed because http.FileServer renders an
// index page for any directory without an index.html — GET /uploads/ would
// otherwise hand an unauthenticated caller the filename of every image ever
// uploaded. Here "/uploads/" and "/uploads/." arrive as "" and ".", which
// Storage.Open refuses as unsafe names before touching the filesystem, so the
// listing has no code path to come back through.
//
// A missing file, a traversal attempt and a name that turns out to be a
// directory therefore look identical from outside. Distinguishing them would
// re-offer the same enumeration one request at a time, and there is nothing
// the caller could do with the difference — clients only ever follow a URL
// built by storage.Storage.URL for one file they were already given.
//
// The reason still has to reach the operator, so anything that is not simply
// "no such object" is logged. The underlying error names a server-side path,
// which is another reason it does not go on the wire.
func uploadHandler(store storage.Storage, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := chi.URLParam(r, "*")

		f, err := store.Open(r.Context(), name)
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, storage.ErrUnsafeName) {
				// A permission or I/O failure is a deployment problem and is
				// completely invisible in a 404, so the log is the only place
				// it can surface.
				logger.Error("upload could not be read", "name", name, "error", err)
			}
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		defer f.Close()

		// ServeContent rather than io.Copy: it answers Range requests with 206
		// and conditional ones with 304, neither of which a plain copy can do.
		// The 304 is what cacheOnSuccess's comment is about — it is how a
		// browser renews the year-long directive instead of re-downloading.
		//
		// The name comes from the URL rather than the filesystem because it is
		// only used to pick a Content-Type from the extension, and the caller's
		// own string is the one that has to determine that.
		http.ServeContent(&cacheOnSuccess{ResponseWriter: w}, r, name, f.ModTime(), f)
	}
}

// cacheOnSuccess adds the long-lived Cache-Control header only once the
// wrapped handler has committed to a cacheable status, so error responses are
// not cached along with the files that do exist.
//
// A year is safe for a hit because upload filenames are generated per upload
// and never reused, so the bytes behind a URL cannot change. It is not safe for
// a miss: caching a 404 would pin it in every intermediary and hide an image
// uploaded to that path moments later.
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
