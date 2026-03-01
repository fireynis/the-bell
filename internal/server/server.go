package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/fireynis/the-bell/internal/config"
	"github.com/fireynis/the-bell/internal/service"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Server holds dependencies and manages the HTTP server lifecycle.
type Server struct {
	cfg            config.Config
	db             *pgxpool.Pool
	logger         *slog.Logger
	srv            *http.Server
	postService    *service.PostService
	reportService  *service.ReportService
	authMiddleware func(http.Handler) http.Handler
}

// Option configures the Server.
type Option func(*Server)

// WithPostService sets the PostService used by post handlers.
func WithPostService(ps *service.PostService) Option {
	return func(s *Server) { s.postService = ps }
}

// WithReportService sets the ReportService used by report handlers.
func WithReportService(rs *service.ReportService) Option {
	return func(s *Server) { s.reportService = rs }
}

// WithAuth sets the authentication middleware for protected routes.
func WithAuth(mw func(http.Handler) http.Handler) Option {
	return func(s *Server) { s.authMiddleware = mw }
}

// New creates a Server with configured routes and middleware.
func New(cfg config.Config, db *pgxpool.Pool, logger *slog.Logger, opts ...Option) *Server {
	s := &Server{
		cfg:    cfg,
		db:     db,
		logger: logger,
	}
	for _, opt := range opts {
		opt(s)
	}
	s.srv = &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      s.routes(),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	return s
}

// Handler returns the configured HTTP handler, useful for testing.
func (s *Server) Handler() http.Handler {
	return s.srv.Handler
}

// Start begins listening. It blocks until the server stops.
// Returns http.ErrServerClosed on graceful shutdown.
func (s *Server) Start() error {
	s.logger.Info("server starting", "addr", s.srv.Addr)
	return s.srv.ListenAndServe()
}

// Shutdown gracefully stops the server, waiting for in-flight requests.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.srv.Shutdown(ctx)
}
