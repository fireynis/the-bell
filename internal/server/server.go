package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/fireynis/the-bell/internal/config"
	"github.com/fireynis/the-bell/internal/middleware"
	"github.com/fireynis/the-bell/internal/service"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Server holds dependencies and manages the HTTP server lifecycle.
type Server struct {
	cfg                     config.Config
	db                      *pgxpool.Pool
	logger                  *slog.Logger
	srv                     *http.Server
	postService             *service.PostService
	userService             *service.UserService
	vouchService            *service.VouchService
	reportService           *service.ReportService
	moderationActionService *service.ModerationActionService
	approvalService         *service.ApprovalService
	votingService           *service.VotingService
	statsService            *service.StatsService
	authMiddleware          func(http.Handler) http.Handler
	rateLimiter             *middleware.RateLimiter
}

// Option configures the Server.
type Option func(*Server)

// WithPostService sets the PostService used by post handlers.
func WithPostService(ps *service.PostService) Option {
	return func(s *Server) { s.postService = ps }
}

// WithUserService sets the UserService used by user handlers.
func WithUserService(us *service.UserService) Option {
	return func(s *Server) { s.userService = us }
}

// WithVouchService sets the VouchService used by user profile vouch listings.
func WithVouchService(vs *service.VouchService) Option {
	return func(s *Server) { s.vouchService = vs }
}

// WithReportService sets the ReportService used by report handlers.
func WithReportService(rs *service.ReportService) Option {
	return func(s *Server) { s.reportService = rs }
}

// WithModerationActionService sets the ModerationActionService used by moderation handlers.
func WithModerationActionService(mas *service.ModerationActionService) Option {
	return func(s *Server) { s.moderationActionService = mas }
}

// WithApprovalService sets the ApprovalService used by approval handlers.
func WithApprovalService(as *service.ApprovalService) Option {
	return func(s *Server) { s.approvalService = as }
}

// WithVotingService sets the VotingService used by council vote handlers.
func WithVotingService(vs *service.VotingService) Option {
	return func(s *Server) { s.votingService = vs }
}

// WithStatsService sets the StatsService used by admin stats handlers.
func WithStatsService(ss *service.StatsService) Option {
	return func(s *Server) { s.statsService = ss }
}

// WithAuth sets the authentication middleware for protected routes.
func WithAuth(mw func(http.Handler) http.Handler) Option {
	return func(s *Server) { s.authMiddleware = mw }
}

// WithRateLimiter sets the rate limiter for request throttling.
func WithRateLimiter(rl *middleware.RateLimiter) Option {
	return func(s *Server) { s.rateLimiter = rl }
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
