package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/fireynis/the-bell/internal/cache"
	"github.com/fireynis/the-bell/internal/config"
	"github.com/fireynis/the-bell/internal/database"
	kratosadmin "github.com/fireynis/the-bell/internal/kratos"
	"github.com/fireynis/the-bell/internal/middleware"
	"github.com/fireynis/the-bell/internal/repository/postgres"
	"github.com/fireynis/the-bell/internal/server"
	"github.com/fireynis/the-bell/internal/service"
	"github.com/fireynis/the-bell/internal/sse"
	"github.com/fireynis/the-bell/internal/storage"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	kratos "github.com/ory/kratos-client-go"
	"github.com/redis/go-redis/v9"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: bell <command>\n\nCommands:\n  serve         Start the HTTP server\n  setup         Bootstrap the town with initial council members\n  check-roles   Run role promotion/demotion checks\n")
		os.Exit(1)
	}

	switch os.Args[1] {
	case "serve":
		runServe(logger)
	case "setup":
		runSetup(logger)
	case "check-roles":
		runCheckRoles(logger)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		os.Exit(1)
	}
}

func runServe(logger *slog.Logger) {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, pool := mustInit(ctx, logger)
	defer pool.Close()

	queries := postgres.New(pool)

	// Repositories
	userRepo := postgres.NewUserRepo(queries)
	configRepo := postgres.NewConfigRepo(queries)
	postRepo := postgres.NewPostRepo(queries)
	reportRepo := postgres.NewReportRepo(queries)
	vouchRepo := postgres.NewVouchRepo(queries)
	modActionRepo := postgres.NewModerationActionRepo(queries)
	penaltyRepo := postgres.NewPenaltyRepo(queries)
	ageQuerier := postgres.NewAGEQuerier(pool)
	voteRepo := postgres.NewVoteRepo(queries)

	reactionRepo := postgres.NewReactionRepo(queries)

	// Services
	userSvc := service.NewUserService(userRepo, nil)
	postSvc := service.NewPostService(postRepo, nil)
	reactionSvc := service.NewReactionService(reactionRepo, nil)

	// Optional Redis feed cache + SSE broker
	var sseBroker *sse.Broker
	if cfg.RedisURL != "" {
		rdb, err := cache.NewRedisClient(cfg.RedisURL)
		if err != nil {
			logger.Error("connecting to redis", "error", err)
			os.Exit(1)
		}
		feedCache := cache.NewFeedCache(rdb, postRepo, logger)
		postSvc.SetFeedCache(feedCache)
		logger.Info("feed cache enabled", "redis", cfg.RedisURL)

		sseBroker = sse.NewBroker(rdb, logger)
		postSvc.SetPublisher(sseBroker)
		logger.Info("SSE broker enabled")
	}

	reportSvc := service.NewReportService(reportRepo, postRepo, nil)
	vouchSvc := service.NewVouchService(vouchRepo, ageQuerier, userRepo, nil)
	modSvc := service.NewModerationService(penaltyRepo, ageQuerier, nil)
	modActionSvc := service.NewModerationActionService(modActionRepo, userRepo, modSvc, userRepo, penaltyRepo, nil)
	approvalSvc := service.NewApprovalService(userRepo, configRepo)
	votingSvc := service.NewVotingService(voteRepo, nil)
	statsRepo := postgres.NewStatsRepo(queries)
	statsSvc := service.NewStatsService(statsRepo)

	// Image storage
	imageStore, err := storage.NewLocalStorage(cfg.ImageStoragePath, "/uploads/")
	if err != nil {
		logger.Error("initializing image storage", "error", err)
		os.Exit(1)
	}

	// Trust score cache + background worker (requires Redis)
	if cfg.RedisURL != "" {
		opts, err := redis.ParseURL(cfg.RedisURL)
		if err != nil {
			logger.Error("parsing redis url", "error", err)
			os.Exit(1)
		}
		rdb := redis.NewClient(opts)
		if err := rdb.Ping(ctx).Err(); err != nil {
			logger.Error("connecting to redis", "error", err)
			os.Exit(1)
		}
		logger.Info("redis connected")

		trustCache := cache.NewTrustCache(rdb)
		trustWorker := cache.NewTrustWorker(trustCache, penaltyRepo, userRepo, logger)
		go trustWorker.Run(ctx)
	}

	// Kratos auth middleware
	kratosCfg := kratos.NewConfiguration()
	kratosCfg.Servers = kratos.ServerConfigurations{{URL: cfg.KratosPublicURL}}
	kratosClient := kratos.NewAPIClient(kratosCfg)
	authMiddleware := middleware.KratosAuth(kratosClient, userSvc, logger)

	// Rate limiter (optional, requires REDIS_URL)
	var rateLimiter *middleware.RateLimiter
	if cfg.RedisURL != "" {
		opts, err := redis.ParseURL(cfg.RedisURL)
		if err != nil {
			logger.Error("parsing REDIS_URL", "error", err)
			os.Exit(1)
		}
		rdb := redis.NewClient(opts)
		if err := rdb.Ping(ctx).Err(); err != nil {
			logger.Warn("redis not reachable, rate limiting disabled", "error", err)
		} else {
			logger.Info("redis connected, rate limiting enabled")
		}
		rlClient := middleware.NewRedisRateLimiterClient(rdb)
		rateLimiter = middleware.NewRateLimiter(rlClient, logger)
	} else {
		logger.Info("REDIS_URL not set, rate limiting disabled")
	}

	var serverOpts []server.Option
	serverOpts = append(serverOpts,
		server.WithAuth(authMiddleware),
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
		server.WithRateLimiter(rateLimiter),
		server.WithImageStore(imageStore),
	)
	if sseBroker != nil {
		serverOpts = append(serverOpts, server.WithSSEBroker(sseBroker))
	}

	srv := server.New(cfg, pool, logger, serverOpts...)

	errCh := make(chan error, 1)
	go func() {
		if err := srv.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	logger.Info("the-bell: ready", "addr", fmt.Sprintf(":%d", cfg.Port))

	select {
	case err := <-errCh:
		logger.Error("server error", "error", err)
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown error", "error", err)
	}
	logger.Info("the-bell: stopped")
}

func runSetup(logger *slog.Logger) {
	fs := flag.NewFlagSet("setup", flag.ExitOnError)
	council := fs.String("council", "", "comma-separated list of council member emails")
	townName := fs.String("town-name", "", "name of the town")
	createDB := fs.Bool("create-db", false, "create bell and bell_kratos databases if they don't exist")
	fs.Parse(os.Args[2:])

	ctx := context.Background()
	scanner := bufio.NewScanner(os.Stdin)

	// Load config early to validate env vars.
	cfg, err := config.Load()
	if err != nil {
		logger.Error("loading config", "error", err)
		os.Exit(1)
	}

	fmt.Println("=== The Bell Setup Wizard ===")
	fmt.Println()

	// --- Prerequisite checks ---
	fmt.Println("Checking prerequisites...")

	// Check Postgres connectivity.
	pgOK := checkPostgres(ctx, cfg.DatabaseURL)
	if pgOK {
		fmt.Println("  [OK] Postgres is reachable")
	} else {
		fmt.Println("  [!!] Postgres is NOT reachable")
	}

	// Check Kratos health.
	kratosOK := checkKratosHealth(cfg.KratosAdminURL)
	if kratosOK {
		fmt.Println("  [OK] Kratos is reachable")
	} else {
		fmt.Println("  [!!] Kratos is NOT reachable")
	}

	// Check Redis (optional).
	if cfg.RedisURL != "" {
		redisOK := checkRedis(ctx, cfg.RedisURL)
		if redisOK {
			fmt.Println("  [OK] Redis is reachable")
		} else {
			fmt.Println("  [!!] Redis is NOT reachable (optional, continuing)")
		}
	} else {
		fmt.Println("  [--] Redis not configured (optional)")
	}
	fmt.Println()

	// Postgres must be reachable to continue (unless --create-db which connects separately).
	if !pgOK && !*createDB {
		fmt.Fprintf(os.Stderr, "error: Postgres is not reachable. Check DATABASE_URL and ensure Postgres is running.\n")
		os.Exit(1)
	}
	if !kratosOK {
		fmt.Fprintf(os.Stderr, "error: Kratos is not reachable. Check KRATOS_ADMIN_URL and ensure Kratos is running.\n")
		os.Exit(1)
	}

	// --- Create databases if requested ---
	dbsCreated := false
	if *createDB {
		fmt.Println("Creating databases...")
		if err := createDatabases(ctx, cfg.DatabaseURL); err != nil {
			logger.Error("creating databases", "error", err)
			os.Exit(1)
		}
		dbsCreated = true
		fmt.Println("  [OK] Databases verified/created")
		fmt.Println()
	}

	// --- Interactive prompts ---
	// Council emails.
	if *council == "" {
		fmt.Print("Enter council member emails (comma-separated): ")
		if scanner.Scan() {
			*council = scanner.Text()
		}
		if *council == "" {
			fmt.Fprintf(os.Stderr, "error: no council emails provided\n")
			os.Exit(1)
		}
	}

	var emails []string
	for _, e := range strings.Split(*council, ",") {
		e = strings.TrimSpace(e)
		if e != "" {
			emails = append(emails, e)
		}
	}
	if len(emails) == 0 {
		fmt.Fprintf(os.Stderr, "error: no valid emails provided\n")
		os.Exit(1)
	}

	// Town name.
	if *townName == "" {
		fmt.Printf("Enter town name [%s]: ", cfg.TownName)
		if scanner.Scan() {
			input := strings.TrimSpace(scanner.Text())
			if input != "" {
				*townName = input
			}
		}
	}

	// --- Connect and run migrations ---
	fmt.Println()
	fmt.Println("Connecting to database and running migrations...")
	pool, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("connecting to database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := database.RunMigrations(ctx, pool); err != nil {
		logger.Error("running migrations", "error", err)
		os.Exit(1)
	}
	fmt.Println("  [OK] Migrations applied")
	fmt.Println()

	// --- Bootstrap ---
	fmt.Println("Bootstrapping town...")
	queries := postgres.New(pool)
	configRepo := postgres.NewConfigRepo(queries)
	kratosClient := kratosadmin.NewAdminClient(cfg.KratosAdminURL)
	transactor := postgres.NewTransactor(pool)

	svc := service.NewBootstrapService(kratosClient, configRepo, transactor, nil)

	result, err := svc.Setup(ctx, emails, *townName)
	if err != nil {
		logger.Error("setup failed", "error", err)
		os.Exit(1)
	}

	// --- Summary ---
	fmt.Println()
	fmt.Println("=== Setup Complete ===")
	fmt.Println()
	if dbsCreated {
		fmt.Println("Databases:    created/verified")
	} else {
		fmt.Println("Databases:    existing (use --create-db to create)")
	}
	fmt.Println("Migrations:   applied")
	if *townName != "" {
		fmt.Printf("Town name:    %s\n", *townName)
	}
	fmt.Println()
	fmt.Printf("Council members created (%d):\n", len(result.Members))
	for _, m := range result.Members {
		fmt.Printf("  Email:     %s\n", m.Email)
		fmt.Printf("  Password:  %s\n", m.Password)
		fmt.Println()
	}
	fmt.Println("NOTE: Save these passwords! Users can reset them via the Kratos recovery flow.")
}

// checkPostgres verifies Postgres connectivity by parsing the DSN and
// attempting a TCP dial to the host:port.
func checkPostgres(ctx context.Context, databaseURL string) bool {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return false
	}
	host := cfg.ConnConfig.Host
	port := cfg.ConnConfig.Port
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	_ = ctx // intentionally unused here; dial is sufficient
	return true
}

// checkKratosHealth pings the Kratos health endpoint.
func checkKratosHealth(adminURL string) bool {
	healthURL := strings.TrimRight(adminURL, "/") + "/health/alive"
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(healthURL)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// checkRedis verifies Redis connectivity.
func checkRedis(ctx context.Context, redisURL string) bool {
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return false
	}
	rdb := redis.NewClient(opts)
	defer rdb.Close()
	tctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return rdb.Ping(tctx).Err() == nil
}

// createDatabases connects to Postgres as the user from DATABASE_URL and
// creates the bell and bell_kratos databases if they don't already exist.
func createDatabases(ctx context.Context, databaseURL string) error {
	// Parse the DATABASE_URL to get connection info, then connect to the
	// default "postgres" database for admin operations.
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		return fmt.Errorf("parsing DATABASE_URL: %w", err)
	}
	parsed.Path = "/postgres"
	adminURL := parsed.String()

	conn, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		return fmt.Errorf("connecting to postgres database: %w", err)
	}
	defer conn.Close(ctx)

	for _, dbName := range []string{"bell", "bell_kratos"} {
		var exists bool
		err := conn.QueryRow(ctx,
			"SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)", dbName,
		).Scan(&exists)
		if err != nil {
			return fmt.Errorf("checking if database %s exists: %w", dbName, err)
		}
		if !exists {
			// Database names can't be parameterized, but these are hardcoded constants.
			_, err := conn.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s", dbName))
			if err != nil {
				return fmt.Errorf("creating database %s: %w", dbName, err)
			}
			fmt.Printf("  Created database: %s\n", dbName)
		} else {
			fmt.Printf("  Database already exists: %s\n", dbName)
		}
	}
	return nil
}

func runCheckRoles(logger *slog.Logger) {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	_, pool := mustInit(ctx, logger)
	defer pool.Close()

	queries := postgres.New(pool)
	roleCheckerRepo := postgres.NewRoleCheckerRepo(queries)
	checker := service.NewRoleChecker(roleCheckerRepo, logger, nil)

	result, err := checker.Run(ctx)
	if err != nil {
		logger.Error("role check failed", "error", err)
		os.Exit(1)
	}

	fmt.Printf("Role check complete.\n")
	fmt.Printf("  Users checked:  %d\n", result.UsersChecked)
	fmt.Printf("  Promotions:     %d\n", len(result.Promotions))
	fmt.Printf("  Demotions:      %d\n", len(result.Demotions))
	fmt.Printf("  Trust marked:   %d\n", result.Marked)
	fmt.Printf("  Trust cleared:  %d\n", result.Cleared)

	for _, p := range result.Promotions {
		fmt.Printf("  [PROMOTED] %s (%s): %s -> %s (%s)\n",
			p.DisplayName, p.UserID, p.OldRole, p.NewRole, p.Reason)
	}
	for _, d := range result.Demotions {
		fmt.Printf("  [DEMOTED]  %s (%s): %s -> %s (%s)\n",
			d.DisplayName, d.UserID, d.OldRole, d.NewRole, d.Reason)
	}
}

// mustInit loads config, connects to the database, and runs migrations.
// It exits the process on failure.
func mustInit(ctx context.Context, logger *slog.Logger) (config.Config, *pgxpool.Pool) {
	cfg, err := config.Load()
	if err != nil {
		logger.Error("loading config", "error", err)
		os.Exit(1)
	}

	pool, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("connecting to database", "error", err)
		os.Exit(1)
	}
	logger.Info("database connected")

	if err := database.RunMigrations(ctx, pool); err != nil {
		logger.Error("running migrations", "error", err)
		os.Exit(1)
	}
	logger.Info("migrations complete")

	return cfg, pool
}
