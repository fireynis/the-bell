package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/fireynis/the-bell/internal/config"
	"github.com/fireynis/the-bell/internal/database"
	kratosadmin "github.com/fireynis/the-bell/internal/kratos"
	"github.com/fireynis/the-bell/internal/repository/postgres"
	"github.com/fireynis/the-bell/internal/server"
	"github.com/fireynis/the-bell/internal/service"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: bell <command>\n\nCommands:\n  serve    Start the HTTP server\n  setup    Bootstrap the town with initial council members\n")
		os.Exit(1)
	}

	switch os.Args[1] {
	case "serve":
		runServe(logger)
	case "setup":
		runSetup(logger)
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

	srv := server.New(cfg, pool, logger)

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
	var councilFlag string
	for i := 2; i < len(os.Args); i++ {
		if os.Args[i] == "--council" && i+1 < len(os.Args) {
			councilFlag = os.Args[i+1]
			break
		}
		if strings.HasPrefix(os.Args[i], "--council=") {
			councilFlag = strings.TrimPrefix(os.Args[i], "--council=")
			break
		}
	}

	if councilFlag == "" {
		fmt.Fprintf(os.Stderr, "usage: bell setup --council email1,email2,...\n")
		os.Exit(1)
	}

	emails := strings.Split(councilFlag, ",")
	for i := range emails {
		emails[i] = strings.TrimSpace(emails[i])
	}

	ctx := context.Background()
	cfg, pool := mustInit(ctx, logger)
	defer pool.Close()

	queries := postgres.New(pool)
	userRepo := postgres.NewUserRepo(queries)
	configRepo := postgres.NewConfigRepo(queries)
	kratosClient := kratosadmin.NewAdminClient(cfg.KratosAdminURL)

	svc := service.NewBootstrapService(userRepo, kratosClient, configRepo, nil)

	if err := svc.Setup(ctx, emails); err != nil {
		logger.Error("setup failed", "error", err)
		os.Exit(1)
	}

	logger.Info("town bootstrapped", "council_members", len(emails))
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
