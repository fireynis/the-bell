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
			}

			if s.moderationActionService != nil {
				mh := handler.NewModerationHandler(s.moderationActionService)
				r.Post("/actions", mh.TakeAction)
				r.Get("/actions/{user_id}", mh.ListActions)
			}
		})
	}

	return r
}
