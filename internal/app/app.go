// Package app builds the application's dependency graph.
//
// It exists because the wiring had drifted into four divergent copies — one in
// cmd/bell and three in the integration harness — and the copies disagreed:
// the test server silently omitted reactions, uploads, rate limiting and SSE,
// so integration tests exercised a different application than production ran.
// Build is the single definition. It performs no I/O beyond the Redis ping the
// caller has already done, installs no signal handlers, and never calls
// os.Exit, so both the CLI and the test harness can use it.
package app

import (
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	kratos "github.com/ory/kratos-client-go"
	"github.com/redis/go-redis/v9"

	"github.com/fireynis/the-bell/internal/cache"
	"github.com/fireynis/the-bell/internal/config"
	"github.com/fireynis/the-bell/internal/middleware"
	"github.com/fireynis/the-bell/internal/repository/postgres"
	"github.com/fireynis/the-bell/internal/server"
	"github.com/fireynis/the-bell/internal/service"
	"github.com/fireynis/the-bell/internal/sse"
	"github.com/fireynis/the-bell/internal/storage"
)

// Deps is the wired application graph.
//
// ServerOptions is the payload most callers want: pass it to server.New and the
// resulting server has every feature the deployment's config enables.
// TrustWorker is nil when Redis is absent — the caller decides whether to run
// it, because that means starting a goroutine.
type Deps struct {
	Config config.Config
	Logger *slog.Logger

	ServerOptions []server.Option

	// TrustWorker is nil unless Redis is configured.
	TrustWorker *cache.TrustWorker

	RoleChecker *service.RoleChecker

	UserService       *service.UserService
	PostService       *service.PostService
	ReactionService   *service.ReactionService
	ReportService     *service.ReportService
	VouchService      *service.VouchService
	ModerationService *service.ModerationActionService
	ApprovalService   *service.ApprovalService
	VotingService     *service.VotingService
	StatsService      *service.StatsService
	ConfigRepo        service.ConfigRepository

	SSEBroker *sse.Broker
}

// trustInputs assembles the repositories service.TrustInputs spans.
//
// The interface reaches across users, posts, reactions, vouches and penalties —
// the four scoring components and their decaying penalties — so no single
// repository satisfies it. Embedding promotes each repository's methods onto
// one value rather than inventing a repository type that would have to live in
// the persistence layer.
//
// Moderation actions are deliberately absent. They were briefly needed while a
// mute was enforced by clamping the trust score; now that domain.User.MutedUntil
// carries that state, the trust calculation has no business reading the
// moderation log.
type trustInputs struct {
	*postgres.UserRepo
	*postgres.PostRepo
	*postgres.ReactionRepo
	*postgres.VouchRepo
	*postgres.PenaltyRepo
}

// Build wires repositories, services and server options from an already-open
// database pool and an optional Redis client.
//
// rdb may be nil: the feed cache, SSE broker, trust worker and rate limiter are
// all Redis-backed and are simply left out, which is the documented degraded
// mode rather than an error.
func Build(cfg config.Config, pool *pgxpool.Pool, rdb *redis.Client, logger *slog.Logger) (*Deps, error) {
	if pool == nil {
		return nil, fmt.Errorf("app: database pool is required")
	}
	if logger == nil {
		return nil, fmt.Errorf("app: logger is required")
	}

	queries := postgres.New(pool)

	// Repositories
	userRepo := postgres.NewUserRepo(queries)
	configRepo := postgres.NewConfigRepo(queries)
	postRepo := postgres.NewPostRepo(queries)
	reportRepo := postgres.NewReportRepo(queries)
	vouchRepo := postgres.NewVouchRepo(queries)
	modActionRepo := postgres.NewModerationActionRepo(queries)
	reliefRepo := postgres.NewModerationReliefRepo(queries)
	penaltyRepo := postgres.NewPenaltyRepo(queries)
	reactionRepo := postgres.NewReactionRepo(queries)
	statsRepo := postgres.NewStatsRepo(queries)
	voteRepo := postgres.NewVoteRepo(queries)
	roleCheckerRepo := postgres.NewRoleCheckerRepo(queries)
	ageQuerier := postgres.NewAGEQuerier(pool)

	// Services
	userSvc := service.NewUserService(userRepo, nil)
	postSvc := service.NewPostService(postRepo, nil)
	reactionSvc := service.NewReactionService(reactionRepo, nil)
	reportSvc := service.NewReportService(reportRepo, postRepo, nil)
	vouchSvc := service.NewVouchService(vouchRepo, ageQuerier, userRepo, nil)
	// Revoking a vouch records a decaying trust penalty against the voucher.
	vouchSvc.SetPenaltyRepository(penaltyRepo)
	modSvc := service.NewModerationService(penaltyRepo, ageQuerier, nil)
	modActionSvc := service.NewModerationActionService(modActionRepo, userRepo, modSvc, userRepo, penaltyRepo, reliefRepo, nil)
	approvalSvc := service.NewApprovalService(userRepo, configRepo)
	votingSvc := service.NewVotingService(voteRepo, nil)
	statsSvc := service.NewStatsService(statsRepo)
	roleChecker := service.NewRoleChecker(roleCheckerRepo, logger, nil)
	// The role checker must never judge a user by a score nobody has computed.
	// Users are created at 50.0 and, without Redis, nothing else recalculates
	// them, so a flat 70.0 demotion threshold would take the whole town down to
	// pending thirty days after it opened. Refreshing here also makes
	// `bell check-roles` the recalculation sweep on Redis-less deployments,
	// where cache.TrustWorker does not exist to run one.
	trustInputBundle := trustInputs{userRepo, postRepo, reactionRepo, vouchRepo, penaltyRepo}
	roleChecker.SetTrustRefresher(trustInputBundle, userRepo)

	deps := &Deps{
		Config:            cfg,
		Logger:            logger,
		RoleChecker:       roleChecker,
		UserService:       userSvc,
		PostService:       postSvc,
		ReactionService:   reactionSvc,
		ReportService:     reportSvc,
		VouchService:      vouchSvc,
		ModerationService: modActionSvc,
		ApprovalService:   approvalSvc,
		VotingService:     votingSvc,
		StatsService:      statsSvc,
		ConfigRepo:        configRepo,
	}

	// Redis-backed features. One client is shared by the feed cache, the SSE
	// broker, the trust cache and the rate limiter; a client per feature would
	// open four independent pools against the same server.
	var rateLimiter *middleware.RateLimiter
	if rdb != nil {
		postSvc.SetFeedCache(cache.NewFeedCache(rdb, postRepo, logger))
		logger.Info("feed cache enabled")

		deps.SSEBroker = sse.NewBroker(rdb, logger)
		logger.Info("SSE broker enabled")

		// The queue is what makes the trust worker live: without it nothing ever
		// enqueues, the worker blocks on an empty queue forever, and no user's
		// score is ever recomputed. Moderation penalties and vouch changes are
		// the two events that invalidate a score, so both services get it.
		trustCache := cache.NewTrustCache(rdb)
		modSvc.SetTrustQueue(trustCache)
		vouchSvc.SetTrustQueue(trustCache)

		// roleCheckerRepo supplies the roster for the worker's periodic sweep.
		// Without it the worker only ever recalculates users something just
		// happened to, which is how penalties came to outlive their decay
		// windows and tenure never accrued.
		deps.TrustWorker = cache.NewTrustWorker(trustCache, trustInputBundle, userRepo, roleCheckerRepo, logger)
		// TRUST_SWEEP_INTERVAL. config.Load rejects a non-positive value, so the
		// only way to reach the worker's own guard is a hand-built Config that
		// left the field zero — the test harness does — and that guard keeps the
		// 24h default for it.
		deps.TrustWorker.SetSweepInterval(cfg.TrustSweepInterval)
		logger.Info("trust recalculation enabled", "sweep_interval", deps.TrustWorker.SweepInterval())

		rateLimiter = middleware.NewRateLimiter(middleware.NewRedisRateLimiterClient(rdb), logger)
		logger.Info("rate limiting enabled")
	} else {
		logger.Info("redis not configured: feed cache, SSE, trust worker and rate limiting disabled")
	}

	imageStore, err := storage.NewLocalStorage(cfg.ImageStoragePath, "/uploads/")
	if err != nil {
		return nil, fmt.Errorf("initializing image storage: %w", err)
	}

	kratosCfg := kratos.NewConfiguration()
	kratosCfg.Servers = kratos.ServerConfigurations{{URL: cfg.KratosPublicURL}}
	kratosClient := kratos.NewAPIClient(kratosCfg)
	authMiddleware := middleware.KratosAuth(kratosClient, userSvc, logger)
	// Public-but-personalized routes need to know who is asking without
	// requiring it; see middleware.OptionalAuth.
	optionalAuth := middleware.OptionalAuth(kratosClient, userSvc, logger)

	deps.ServerOptions = []server.Option{
		server.WithAuth(authMiddleware),
		server.WithOptionalAuth(optionalAuth),
		server.WithUserService(userSvc),
		server.WithPostService(postSvc),
		server.WithReportService(reportSvc),
		server.WithVouchService(vouchSvc),
		server.WithModerationActionService(modActionSvc),
		server.WithApprovalService(approvalSvc),
		server.WithVotingService(votingSvc),
		server.WithReactionService(reactionSvc),
		server.WithReactionRepo(reactionRepo),
		server.WithStatsService(statsSvc),
		server.WithConfigRepo(configRepo),
		server.WithTransactor(postgres.NewTransactor(pool)),
		server.WithRateLimiter(rateLimiter),
		server.WithImageStore(imageStore),
	}
	// WithSSEBroker is only appended when there is a broker: passing a typed
	// nil would make the server's `!= nil` check register a live SSE route
	// backed by nothing.
	if deps.SSEBroker != nil {
		deps.ServerOptions = append(deps.ServerOptions, server.WithSSEBroker(deps.SSEBroker))
	}

	return deps, nil
}
