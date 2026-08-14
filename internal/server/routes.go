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
			// Registration is throttled per IP before the request reaches
			// Kratos. Everything else under /.ory passes through, including the
			// login and session paths a signed-in resident hits constantly —
			// see middleware.KratosRegistrationLimit.
			r.With(middleware.KratosRegistrationLimit(
				s.rateLimiter, s.trustedProxies, registrationMaxPerIP, registrationWindow,
			)).HandleFunc("/.ory/*", func(w http.ResponseWriter, req *http.Request) {
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

// The registration budget, per client IP.
//
// Ten an hour is not one account an hour. Kratos v1.x registration is two-step,
// so completing a sign-up costs a flow init plus two submits — three requests —
// and a resident who mistypes their password spends more. Ten leaves room for
// roughly three sign-ups an hour from one address, which covers a household or
// a library terminal, while capping a scripted flood at a couple of hundred
// pending accounts a day per address instead of an unbounded number per minute.
//
// The window is what a rejected caller is told to wait, so a longer one would
// hand a mistyped password a punishment out of proportion to it.
const (
	registrationMaxPerIP = 10
	registrationWindow   = time.Hour
)

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
// the order auth → active → verified email → role → rate limit.
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
		// Verification gates participation, not self-inspection, so it rides
		// with RequireActive: skipActive already marks the routes where a user
		// who may not participate still has to learn why — GET /v1/me and
		// GET /v1/users/me/moderation-history, and nothing else. A member who
		// has not verified their email can still be moderated, so the endpoint
		// that explains a moderation decision cannot be gated on it either.
		// Giving verification its own opt-out would leave two flags that
		// must be set together on every such route, and one of them would
		// eventually be forgotten.
		if s.cfg.RequireVerifiedEmail {
			mws = append(mws, middleware.RequireVerifiedEmail)
		}
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
	// A member's own profile is the one place a lifted mute or suspension is
	// visible to the person it released — the moderation audit trail is
	// moderator-only — so the self view reads its lifts from the moderation
	// service.
	if s.moderationActionService != nil {
		uh.SetReliefLister(s.moderationActionService)
	}
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
	// Built here for the reason postH is: the moderation handler now serves two
	// route families with different guards — the moderator-only /v1/moderation
	// group, and the member's own history under /v1/users/me, which a suspended
	// member has to be able to reach.
	var mh *handler.ModerationHandler
	if s.moderationActionService != nil {
		mh = handler.NewModerationHandler(s.moderationActionService)
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
				// The directory carries no role floor on purpose. A pending
				// resident has to be able to browse — finding somebody to vouch
				// for them is how they stop being pending — and they have to be
				// findable in it for the same reason.
				r.Get("/", uh.ListDirectory)
				r.Get("/me", uh.GetMe)
				r.Put("/me", uh.UpdateMe)
				// Stating where you live is a pending resident's half of the
				// approval conversation, so it sits with the rest of the
				// self-service profile routes under the same guard: signed in
				// and active, no role floor. A pending user is active, which
				// is what makes their application reviewable in the first
				// place.
				r.Put("/me/residency-claim", uh.UpdateResidencyClaim)
			})

			// A member's own moderation record: what was done to them, why, and
			// what it cost. It skips RequireActive on the same reasoning as
			// GET /v1/me, and here the reasoning is at its strongest — a
			// suspended or banned member is precisely the person who most needs
			// to read why, and RequireActive would answer them "account
			// suspended" and nothing else. Being told you are suspended by the
			// endpoint that exists to explain the suspension is the failure
			// mode this route was added to remove.
			//
			// It is a route group of its own rather than an addition to the
			// group above, because that group is what enforces the ordinary
			// rule; carving the exception out where it can be seen is the same
			// discipline the skipActive flag itself encodes.
			//
			// The route lives under /v1/users because that is the path it
			// belongs on, which means chi mounts one subrouter for the prefix
			// and this rides inside it. The handler comes from the moderation
			// service, so it is nil on a deployment without one; the /v1/users
			// group as a whole still needs s.userService, which app.Build
			// always wires alongside it.
			if mh != nil {
				r.Group(func(r chi.Router) {
					r.Use(s.protected(guard{skipActive: true})...)
					r.Get("/me/moderation-history", mh.OwnHistory)
				})
			}

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

	if reportH != nil || mh != nil || postH != nil {
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

			if mh != nil {
				r.Post("/actions", mh.TakeAction)
				r.Get("/actions/{user_id}", mh.ListActions)
				// Lifting a mute is a DELETE of the mute itself, not another
				// action taken against the person: it writes no
				// moderation_actions row, so it does not belong under
				// /actions. The group's moderator guard covers it, and the
				// service re-checks the role regardless.
				r.Get("/users/{user_id}/mute", mh.MuteStatus)
				r.Delete("/users/{user_id}/mute", mh.LiftMute)
				// The suspension is the same restriction one severity up, so
				// it gets the same pair rather than a bespoke shape.
				r.Get("/users/{user_id}/suspension", mh.SuspensionStatus)
				r.Delete("/users/{user_id}/suspension", mh.LiftSuspension)
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

				// The limiter keys on the endpoint NAME, not the route, so
				// "vouches" is the per-member vouching budget and nothing else
				// may reuse it. Naming it on the approval group would put a
				// member's vouches and a council member's approvals in one
				// shared bucket.
				r.Group(func(r chi.Router) {
					r.Use(s.protected(guard{
						role:  domain.RoleMember,
						limit: &rateSpec{"vouches", 3, 24 * time.Hour},
					})...)
					r.Post("/", vh.Create)
				})

				// Revocation gets its own budget for the same reason, one step
				// further out. Sharing "vouches" meant a member who had spent
				// their three vouches could not withdraw one for 24 hours —
				// and revoking is the abuse-response path: it is what you do
				// when you realise you vouched for the wrong person. It must
				// never be starved by ordinary vouching. The limiter's Lua
				// records the attempt before counting it, by design, so each
				// refused retry also pushed the window out; splitting the
				// buckets is what stops the lockout, not that behaviour.
				//
				// Same 3-per-24h shape, because the abuse it guards against is
				// the same: scripted churn against the trust graph.
				r.Group(func(r chi.Router) {
					r.Use(s.protected(guard{
						role:  domain.RoleMember,
						limit: &rateSpec{"vouch-revokes", 3, 24 * time.Hour},
					})...)
					r.Delete("/{id}", vh.Revoke)
				})
			}
		})
	}

	// The council's Town Hall. These replace GET/POST
	// /v1/admin/council/votes, which recorded votes against proposal ids that
	// referred to nothing: there was no proposal entity, so nothing could be
	// raised, read back or acted on. The routes are gone rather than deprecated
	// because nothing but the old admin screen ever called them and no data
	// they produced was ever meaningful.
	if s.proposalService != nil {
		ph := handler.NewProposalHandler(s.proposalService)
		r.Route("/v1/admin/proposals", func(r chi.Router) {
			r.Use(s.protected(guard{role: domain.RoleCouncil})...)
			r.Get("/", ph.List)
			r.Post("/", ph.Create)
			r.Post("/{id}/votes", ph.Vote)
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
