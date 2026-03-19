package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/handler"
	"github.com/fireynis/the-bell/internal/middleware"
)

func (s *Server) routes() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.ContentTypeJSON)
	r.Use(middleware.RequestLogger(s.logger))
	r.Get("/healthz", handler.Health)

	// GET /api/v1/me — return the authenticated user.
	// Intentionally omits RequireActive so suspended/banned users can still
	// learn their own status and role (the frontend RequireRole guard needs this).
	r.Route("/api/v1/me", func(r chi.Router) {
		if s.authMiddleware != nil {
			r.Use(s.authMiddleware)
		}
		uh := handler.NewUserHandler(s.userService, s.postService, s.vouchService)
		r.Get("/", uh.GetMe)
	})

	if s.postService != nil {
		ph := handler.NewPostHandler(s.postService)
		r.Route("/api/v1/posts", func(r chi.Router) {
			r.Get("/", ph.ListFeed)
			r.Get("/{id}", ph.GetByID)

			r.Group(func(r chi.Router) {
				if s.authMiddleware != nil {
					r.Use(s.authMiddleware)
				}
				r.Use(middleware.RequireActive)
				r.Use(middleware.RequireRole(domain.RoleMember))
				r.Post("/", ph.Create)
				r.Patch("/{id}", ph.Update)
				r.Delete("/{id}", ph.Delete)
			})
		})
	}

	if s.userService != nil {
		uh := handler.NewUserHandler(s.userService, s.postService, s.vouchService)

		r.Route("/api/v1/users", func(r chi.Router) {
			// Authenticated endpoints for own profile
			r.Group(func(r chi.Router) {
				if s.authMiddleware != nil {
					r.Use(s.authMiddleware)
				}
				r.Use(middleware.RequireActive)
				r.Get("/me", uh.GetMe)
				r.Put("/me", uh.UpdateMe)
			})

			// Public user profile endpoints
			r.Get("/{id}", uh.GetByID)
			r.Get("/{id}/posts", uh.ListPosts)
			r.Get("/{id}/vouches", uh.ListVouches)
		})
	}

	if s.reportService != nil {
		rh := handler.NewReportHandler(s.reportService)

		// Report submission (auth + member required)
		r.Route("/api/v1/posts/{id}/report", func(r chi.Router) {
			if s.authMiddleware != nil {
				r.Use(s.authMiddleware)
			}
			r.Use(middleware.RequireActive)
			r.Use(middleware.RequireRole(domain.RoleMember))
			r.Post("/", rh.SubmitReport)
		})
	}

	if s.reportService != nil || s.moderationActionService != nil {
		r.Route("/api/v1/moderation", func(r chi.Router) {
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
		r.Route("/api/v1/vouches", func(r chi.Router) {
			if s.authMiddleware != nil {
				r.Use(s.authMiddleware)
			}
			r.Use(middleware.RequireActive)
			r.Use(middleware.RequireRole(domain.RoleCouncil))
			r.Get("/pending", ah.ListPending)
			r.Post("/approve/{id}", ah.Approve)
		})
	}

	if s.votingService != nil {
		vh := handler.NewVotingHandler(s.votingService)
		r.Route("/api/v1/admin/council/votes", func(r chi.Router) {
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
		r.Route("/api/v1/admin/stats", func(r chi.Router) {
			if s.authMiddleware != nil {
				r.Use(s.authMiddleware)
			}
			r.Use(middleware.RequireActive)
			r.Use(middleware.RequireRole(domain.RoleCouncil))
			r.Get("/", sh.GetStats)
		})
	}

	return r
}
