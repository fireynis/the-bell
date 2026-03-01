package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/fireynis/the-bell/internal/handler"
	"github.com/fireynis/the-bell/internal/middleware"
)

func (s *Server) routes() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.ContentTypeJSON)
	r.Use(middleware.RequestLogger(s.logger))
	r.Get("/healthz", handler.Health)
	return r
}
