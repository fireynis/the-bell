package server

import (
	"io/fs"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/handler"
	"github.com/fireynis/the-bell/internal/middleware"
)

func (s *Server) routes() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestLogger(s.logger))
	r.Get("/healthz", handler.Health)

	// SSE endpoint — registered before /api to avoid ContentTypeJSON middleware.
	if s.sseBroker != nil {
		sseH := handler.NewSSEHandler(s.sseBroker)
		r.Route("/api/v1/feed/live", func(r chi.Router) {
			if s.authMiddleware != nil {
				r.Use(s.authMiddleware)
			}
			r.Use(middleware.RequireActive)
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
		if err == nil {
			proxy := httputil.NewSingleHostReverseProxy(kratosTarget)
			proxy.ModifyResponse = func(resp *http.Response) error {
				// Strip Secure flag from cookies so they work over plain HTTP.
				if cookies := resp.Header.Values("Set-Cookie"); len(cookies) > 0 {
					resp.Header.Del("Set-Cookie")
					for _, c := range cookies {
						c = strings.Replace(c, "; Secure", "", 1)
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
	if s.imageStore != nil {
		fileServer := http.StripPrefix("/uploads/", http.FileServer(http.Dir(s.cfg.ImageStoragePath)))
		r.Get("/uploads/*", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "public, max-age=31536000")
			fileServer.ServeHTTP(w, r)
		})
	}

	// Serve SPA frontend (web/dist).
	spaDir := "web/dist"
	if _, err := os.Stat(spaDir); err == nil {
		r.Get("/*", spaHandler(spaDir))
	}

	return r
}

func (s *Server) apiRoutes(r chi.Router) {
	// GET /api/v1/me — return the authenticated user.
	// Intentionally omits RequireActive so suspended/banned users can still
	// learn their own status and role (the frontend RequireRole guard needs this).
	r.Route("/v1/me", func(r chi.Router) {
		if s.authMiddleware != nil {
			r.Use(s.authMiddleware)
		}
		uh := handler.NewUserHandler(s.userService, s.postService, s.vouchService)
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
		ph := handler.NewPostHandler(s.postService, phOpts...)
		r.Route("/v1/posts", func(r chi.Router) {
			r.Get("/", ph.ListFeed)
			r.Get("/{id}", ph.GetByID)

			r.Group(func(r chi.Router) {
				if s.authMiddleware != nil {
					r.Use(s.authMiddleware)
				}
				r.Use(middleware.RequireActive)
				r.Use(middleware.RequireRole(domain.RoleMember))
				if s.rateLimiter != nil {
					r.Use(s.rateLimiter.Limit("posts", 10, time.Hour))
				}
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
			if s.authMiddleware != nil {
				r.Use(s.authMiddleware)
			}
			r.Use(middleware.RequireActive)
			r.Use(middleware.RequireRole(domain.RoleMember))
			if s.rateLimiter != nil {
				r.Use(s.rateLimiter.Limit("reactions", 60, time.Minute))
			}
			r.Post("/", rh.Add)
			r.Delete("/{type}", rh.Remove)
		})
	}

	if s.userService != nil {
		uh := handler.NewUserHandler(s.userService, s.postService, s.vouchService)

		r.Route("/v1/users", func(r chi.Router) {
			r.Group(func(r chi.Router) {
				if s.authMiddleware != nil {
					r.Use(s.authMiddleware)
				}
				r.Use(middleware.RequireActive)
				r.Get("/me", uh.GetMe)
				r.Put("/me", uh.UpdateMe)
			})

			r.Get("/{id}", uh.GetByID)
			r.Get("/{id}/posts", uh.ListPosts)
			r.Get("/{id}/vouches", uh.ListVouches)
		})
	}

	if s.reportService != nil {
		rh := handler.NewReportHandler(s.reportService)

		r.Route("/v1/posts/{id}/report", func(r chi.Router) {
			if s.authMiddleware != nil {
				r.Use(s.authMiddleware)
			}
			r.Use(middleware.RequireActive)
			r.Use(middleware.RequireRole(domain.RoleMember))
			if s.rateLimiter != nil {
				r.Use(s.rateLimiter.Limit("reports", 5, time.Hour))
			}
			r.Post("/", rh.SubmitReport)
		})
	}

	if s.reportService != nil || s.moderationActionService != nil {
		r.Route("/v1/moderation", func(r chi.Router) {
			if s.authMiddleware != nil {
				r.Use(s.authMiddleware)
			}
			r.Use(middleware.RequireActive)
			r.Use(middleware.RequireRole(domain.RoleModerator))

			if s.reportService != nil {
				rh := handler.NewReportHandler(s.reportService)
				r.Get("/queue", rh.ListQueue)
				r.Patch("/reports/{id}", rh.UpdateReportStatus)
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
			if s.authMiddleware != nil {
				r.Use(s.authMiddleware)
			}
			r.Use(middleware.RequireActive)
			r.Use(middleware.RequireRole(domain.RoleCouncil))
			if s.rateLimiter != nil {
				r.Use(s.rateLimiter.Limit("vouches", 3, 24*time.Hour))
			}
			r.Get("/pending", ah.ListPending)
			r.Post("/approve/{id}", ah.Approve)
		})
	}

	if s.votingService != nil {
		vh := handler.NewVotingHandler(s.votingService)
		r.Route("/v1/admin/council/votes", func(r chi.Router) {
			if s.authMiddleware != nil {
				r.Use(s.authMiddleware)
			}
			r.Use(middleware.RequireActive)
			r.Use(middleware.RequireRole(domain.RoleCouncil))
			r.Post("/", vh.CastVote)
			r.Get("/", vh.ListPending)
		})
	}

	if s.statsService != nil {
		sh := handler.NewStatsHandler(s.statsService)
		r.Route("/v1/admin/stats", func(r chi.Router) {
			if s.authMiddleware != nil {
				r.Use(s.authMiddleware)
			}
			r.Use(middleware.RequireActive)
			r.Use(middleware.RequireRole(domain.RoleCouncil))
			r.Get("/", sh.GetStats)
		})
	}

	if s.configRepo != nil {
		ch := handler.NewConfigHandler(s.configRepo)
		r.Get("/v1/config", ch.GetConfig)

		r.Route("/v1/admin/config", func(r chi.Router) {
			if s.authMiddleware != nil {
				r.Use(s.authMiddleware)
			}
			r.Use(middleware.RequireActive)
			r.Use(middleware.RequireRole(domain.RoleCouncil))
			r.Put("/", ch.UpdateConfig)
		})
	}
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
