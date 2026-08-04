package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/fireynis/the-bell/internal/app"
	"github.com/fireynis/the-bell/internal/cache"
	"github.com/fireynis/the-bell/internal/config"
	"github.com/fireynis/the-bell/internal/database"
	kratosadmin "github.com/fireynis/the-bell/internal/kratos"
	"github.com/fireynis/the-bell/internal/repository/postgres"
	"github.com/fireynis/the-bell/internal/server"
	"github.com/fireynis/the-bell/internal/service"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

const usage = `usage: bell <command>

Commands:
  serve         Start the HTTP server
  setup         Bootstrap the town with initial council members
  check-roles   Run role promotion/demotion checks
`

func main() {
	os.Exit(run(os.Args, os.Stdin, os.Stdout, os.Stderr))
}

// run dispatches a subcommand and returns the process exit code. Keeping the
// streams and argv as parameters — and returning a code rather than calling
// os.Exit — is what makes the CLI testable at all.
func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	logger := slog.New(slog.NewJSONHandler(stdout, nil))

	if len(args) < 2 {
		fmt.Fprint(stderr, usage)
		return 1
	}

	switch args[1] {
	case "serve":
		return runServe(logger)
	case "setup":
		return runSetup(logger, args[2:], stdin, stdout, stderr)
	case "check-roles":
		return runCheckRoles(logger, stdout)
	default:
		fmt.Fprintf(stderr, "unknown command: %s\n", args[1])
		return 1
	}
}

func runServe(logger *slog.Logger) int {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, pool, err := initApp(ctx, logger)
	if err != nil {
		logger.Error("startup failed", "error", err)
		return 1
	}
	defer pool.Close()

	rdb, err := connectRedis(ctx, cfg.RedisURL, logger)
	if err != nil {
		logger.Error("connecting to redis", "error", err)
		return 1
	}
	if rdb != nil {
		defer rdb.Close()
	}

	deps, err := app.Build(cfg, pool, rdb, logger)
	if err != nil {
		logger.Error("building application", "error", err)
		return 1
	}

	if deps.TrustWorker != nil {
		go deps.TrustWorker.Run(ctx)
		logger.Info("trust score worker started")
	}

	srv := server.New(cfg, pool, logger, deps.ServerOptions...)

	errCh := make(chan error, 1)
	go func() {
		if err := srv.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	logger.Info("the-bell: ready", "addr", fmt.Sprintf(":%d", cfg.Port))

	// A server error is a failed run; a signal is a clean stop. Either way the
	// shutdown below still runs so in-flight requests are not dropped.
	code := 0
	select {
	case err := <-errCh:
		logger.Error("server error", "error", err)
		code = 1
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown error", "error", err)
		code = 1
	}
	logger.Info("the-bell: stopped")
	return code
}

func runSetup(logger *slog.Logger, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	fs.SetOutput(stderr)
	council := fs.String("council", "", "comma-separated list of council member emails")
	townName := fs.String("town-name", "", "name of the town")
	createDB := fs.Bool("create-db", false, "create the application and Kratos databases if they don't exist")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	ctx := context.Background()
	scanner := bufio.NewScanner(stdin)

	// Load config early to validate env vars.
	cfg, err := config.Load()
	if err != nil {
		logger.Error("loading config", "error", err)
		return 1
	}

	fmt.Fprintln(stdout, "=== The Bell Setup Wizard ===")
	fmt.Fprintln(stdout)

	// --- Prerequisite checks ---
	fmt.Fprintln(stdout, "Checking prerequisites...")

	// Check Postgres connectivity.
	pgOK := checkPostgres(ctx, cfg.DatabaseURL)
	if pgOK {
		fmt.Fprintln(stdout, "  [OK] Postgres is reachable")
	} else {
		fmt.Fprintln(stdout, "  [!!] Postgres is NOT reachable")
	}

	// Check Kratos health.
	kratosOK := checkKratosHealth(cfg.KratosAdminURL)
	if kratosOK {
		fmt.Fprintln(stdout, "  [OK] Kratos is reachable")
	} else {
		fmt.Fprintln(stdout, "  [!!] Kratos is NOT reachable")
	}

	// Check Redis (optional).
	if cfg.RedisURL != "" {
		redisOK := checkRedis(ctx, cfg.RedisURL)
		if redisOK {
			fmt.Fprintln(stdout, "  [OK] Redis is reachable")
		} else {
			fmt.Fprintln(stdout, "  [!!] Redis is NOT reachable (optional, continuing)")
		}
	} else {
		fmt.Fprintln(stdout, "  [--] Redis not configured (optional)")
	}
	fmt.Fprintln(stdout)

	// Postgres must be reachable to continue (unless --create-db which connects separately).
	if !pgOK && !*createDB {
		fmt.Fprintf(stderr, "error: Postgres is not reachable. Check DATABASE_URL and ensure Postgres is running.\n")
		return 1
	}
	if !kratosOK {
		fmt.Fprintf(stderr, "error: Kratos is not reachable. Check KRATOS_ADMIN_URL and ensure Kratos is running.\n")
		return 1
	}

	// --- Create databases if requested ---
	dbsCreated := false
	if *createDB {
		fmt.Fprintln(stdout, "Creating databases...")
		if err := createDatabases(ctx, cfg.DatabaseURL, stdout); err != nil {
			logger.Error("creating databases", "error", err)
			return 1
		}
		dbsCreated = true
		fmt.Fprintln(stdout, "  [OK] Databases verified/created")
		fmt.Fprintln(stdout)
	}

	// --- Interactive prompts ---
	// Council emails.
	if *council == "" {
		fmt.Fprint(stdout, "Enter council member emails (comma-separated): ")
		if scanner.Scan() {
			*council = scanner.Text()
		}
		if *council == "" {
			fmt.Fprintf(stderr, "error: no council emails provided\n")
			return 1
		}
	}

	emails := parseCouncilEmails(*council)
	if len(emails) == 0 {
		fmt.Fprintf(stderr, "error: no valid emails provided\n")
		return 1
	}

	// Town name. The prompt offers cfg.TownName as the default, so an empty
	// answer has to resolve to it rather than falling through as "".
	var prompted string
	if *townName == "" {
		fmt.Fprintf(stdout, "Enter town name [%s]: ", cfg.TownName)
		if scanner.Scan() {
			prompted = scanner.Text()
		}
	}
	resolvedTownName := resolveTownName(*townName, prompted, cfg.TownName)

	// --- Connect and run migrations ---
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "Connecting to database and running migrations...")
	pool, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("connecting to database", "error", err)
		return 1
	}
	defer pool.Close()

	if err := database.RunMigrations(ctx, pool); err != nil {
		logger.Error("running migrations", "error", err)
		return 1
	}
	fmt.Fprintln(stdout, "  [OK] Migrations applied")
	fmt.Fprintln(stdout)

	// --- Bootstrap ---
	fmt.Fprintln(stdout, "Bootstrapping town...")
	queries := postgres.New(pool)
	configRepo := postgres.NewConfigRepo(queries)
	kratosClient := kratosadmin.NewAdminClient(cfg.KratosAdminURL)
	transactor := postgres.NewTransactor(pool)

	svc := service.NewBootstrapService(kratosClient, configRepo, transactor, nil)

	result, err := svc.Setup(ctx, emails, resolvedTownName)
	if err != nil {
		logger.Error("setup failed", "error", err)
		return 1
	}

	// --- Summary ---
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "=== Setup Complete ===")
	fmt.Fprintln(stdout)
	if dbsCreated {
		fmt.Fprintln(stdout, "Databases:    created/verified")
	} else {
		fmt.Fprintln(stdout, "Databases:    existing (use --create-db to create)")
	}
	fmt.Fprintln(stdout, "Migrations:   applied")
	if resolvedTownName != "" {
		fmt.Fprintf(stdout, "Town name:    %s\n", resolvedTownName)
	}
	fmt.Fprintln(stdout)
	fmt.Fprintf(stdout, "Council members created (%d):\n", len(result.Members))
	for _, m := range result.Members {
		fmt.Fprintf(stdout, "  Email:     %s\n", m.Email)
		fmt.Fprintf(stdout, "  Password:  %s\n", m.Password)
		fmt.Fprintln(stdout)
	}
	fmt.Fprintln(stdout, "NOTE: Save these passwords! Users can reset them via the Kratos recovery flow.")
	return 0
}

// parseCouncilEmails splits the --council flag into individual addresses.
//
// Blank entries are dropped so a trailing comma or a stray space does not
// become an empty council member, and duplicates are removed because the
// bootstrap creates one Kratos identity per address — the same email twice
// would fail partway through with some members already created.
// Comparison is case-insensitive since email local parts are treated as such
// in practice, but the first spelling seen is kept for display.
func parseCouncilEmails(raw string) []string {
	var emails []string
	seen := make(map[string]bool)

	for _, e := range strings.Split(raw, ",") {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		key := strings.ToLower(e)
		if seen[key] {
			continue
		}
		seen[key] = true
		emails = append(emails, e)
	}

	return emails
}

// resolveTownName picks the town name setup will persist, preferring the
// --town-name flag, then what the operator typed at the prompt, then the
// configured default.
//
// The prompt displays the default in brackets, so pressing Enter has to resolve
// to that default. Returning "" instead would reach BootstrapService.Setup,
// which reads an empty name as "leave it unset" — the town would come out
// unnamed even though the operator was shown a name and accepted it.
func resolveTownName(flagValue, prompted, cfgDefault string) string {
	for _, candidate := range []string{flagValue, prompted, cfgDefault} {
		if name := strings.TrimSpace(candidate); name != "" {
			return name
		}
	}
	return ""
}

// dialAddr returns the host:port to probe for a Postgres DSN.
//
// A host that is a filesystem path means a unix socket, which cannot be
// TCP-dialled. pgx also defaults an empty DSN to the socket directory "/tmp",
// so rejecting path hosts turns a probe of the meaningless address "/tmp:5432"
// into an error that says what is actually wrong.
func dialAddr(dsn string) (string, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return "", fmt.Errorf("parsing DATABASE_URL: %w", err)
	}
	host := cfg.ConnConfig.Host
	if host == "" || strings.HasPrefix(host, "/") {
		return "", fmt.Errorf("DATABASE_URL has no TCP host to probe (got %q)", host)
	}
	return net.JoinHostPort(host, strconv.Itoa(int(cfg.ConnConfig.Port))), nil
}

// adminDSN rewrites a DSN to point at the default "postgres" database.
//
// CREATE DATABASE cannot run from inside the database being created, so the
// admin connection has to land somewhere that always exists.
func adminDSN(dsn string) (string, error) {
	parsed, err := parsePostgresDSN(dsn)
	if err != nil {
		return "", err
	}
	parsed.Path = "/postgres"
	return parsed.String(), nil
}

// databaseNames derives the databases setup must create from the DSN, keeping
// the "<primary>_kratos" sibling convention.
//
// These were hardcoded to {"bell", "bell_kratos"} while the connection came
// from DATABASE_URL, so pointing DATABASE_URL at .../thebell created "bell" and
// then failed to migrate anything.
func databaseNames(dsn string) (primary, kratos string, err error) {
	parsed, err := parsePostgresDSN(dsn)
	if err != nil {
		return "", "", err
	}

	primary = strings.TrimPrefix(parsed.Path, "/")
	if primary == "" {
		return "", "", fmt.Errorf("DATABASE_URL has no database name: %s", parsed.Redacted())
	}
	if strings.ContainsRune(primary, 0) {
		return "", "", fmt.Errorf("database name contains a NUL byte")
	}

	kratos = primary + "_kratos"
	// Postgres silently truncates identifiers at 63 bytes, which would let two
	// different DSNs collide on the same Kratos database.
	if len(kratos) > 63 {
		return "", "", fmt.Errorf("database name %q is too long: %q exceeds the 63-byte identifier limit", primary, kratos)
	}
	return primary, kratos, nil
}

// parsePostgresDSN parses a DSN and insists on a postgres:// URL. The keyword
// form ("host=... dbname=...") parses as a URL without erroring but yields
// nothing usable, so rejecting it here turns a confusing downstream failure
// into a clear one.
func parsePostgresDSN(dsn string) (*url.URL, error) {
	parsed, err := url.Parse(dsn)
	if err != nil {
		return nil, fmt.Errorf("parsing DATABASE_URL: %w", err)
	}
	switch parsed.Scheme {
	case "postgres", "postgresql":
		return parsed, nil
	default:
		return nil, fmt.Errorf("DATABASE_URL must be a postgres:// URL, got scheme %q", parsed.Scheme)
	}
}

// checkPostgres verifies Postgres connectivity by parsing the DSN and
// attempting a TCP dial to the host:port.
func checkPostgres(ctx context.Context, databaseURL string) bool {
	addr, err := dialAddr(databaseURL)
	if err != nil {
		return false
	}
	dialCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var d net.Dialer
	conn, err := d.DialContext(dialCtx, "tcp", addr)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// kratosHealthURL builds the liveness endpoint for a Kratos admin base URL.
// The base comes from config, where a trailing slash is easy to leave on and
// would otherwise produce a "//health/alive" that Kratos does not serve.
func kratosHealthURL(adminURL string) string {
	return strings.TrimRight(adminURL, "/") + "/health/alive"
}

// checkKratosHealth pings the Kratos health endpoint.
func checkKratosHealth(adminURL string) bool {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(kratosHealthURL(adminURL))
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
// creates the application database and its Kratos sibling if they don't
// already exist. Both names come from the DSN, not from constants.
func createDatabases(ctx context.Context, databaseURL string, out io.Writer) error {
	primary, kratosDB, err := databaseNames(databaseURL)
	if err != nil {
		return err
	}

	// Admin operations run against the default "postgres" database.
	adminURL, err := adminDSN(databaseURL)
	if err != nil {
		return err
	}

	conn, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		return fmt.Errorf("connecting to postgres database: %w", err)
	}
	defer conn.Close(ctx)

	for _, dbName := range []string{primary, kratosDB} {
		var exists bool
		err := conn.QueryRow(ctx,
			"SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)", dbName,
		).Scan(&exists)
		if err != nil {
			return fmt.Errorf("checking if database %s exists: %w", dbName, err)
		}
		if !exists {
			// CREATE DATABASE cannot take a bind parameter. The name now comes
			// from operator-supplied config rather than a constant, so it is
			// quoted as an identifier instead of interpolated raw.
			_, err := conn.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{dbName}.Sanitize())
			if err != nil {
				return fmt.Errorf("creating database %s: %w", dbName, err)
			}
			fmt.Fprintf(out, "  Created database: %s\n", dbName)
		} else {
			fmt.Fprintf(out, "  Database already exists: %s\n", dbName)
		}
	}
	return nil
}

func runCheckRoles(logger *slog.Logger, stdout io.Writer) int {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, pool, err := initApp(ctx, logger)
	if err != nil {
		logger.Error("startup failed", "error", err)
		return 1
	}
	defer pool.Close()

	// check-roles needs no Redis-backed feature, so the graph is built without
	// a client rather than opening one just to throw it away.
	deps, err := app.Build(cfg, pool, nil, logger)
	if err != nil {
		logger.Error("building application", "error", err)
		return 1
	}

	result, err := deps.RoleChecker.Run(ctx)
	if err != nil {
		logger.Error("role check failed", "error", err)
		return 1
	}

	fmt.Fprintf(stdout, "Role check complete.\n")
	fmt.Fprintf(stdout, "  Users checked:  %d\n", result.UsersChecked)
	fmt.Fprintf(stdout, "  Promotions:     %d\n", len(result.Promotions))
	fmt.Fprintf(stdout, "  Demotions:      %d\n", len(result.Demotions))
	fmt.Fprintf(stdout, "  Trust marked:   %d\n", result.Marked)
	fmt.Fprintf(stdout, "  Trust cleared:  %d\n", result.Cleared)

	for _, p := range result.Promotions {
		fmt.Fprintf(stdout, "  [PROMOTED] %s (%s): %s -> %s (%s)\n",
			p.DisplayName, p.UserID, p.OldRole, p.NewRole, p.Reason)
	}
	for _, d := range result.Demotions {
		fmt.Fprintf(stdout, "  [DEMOTED]  %s (%s): %s -> %s (%s)\n",
			d.DisplayName, d.UserID, d.OldRole, d.NewRole, d.Reason)
	}
	return 0
}

// connectRedis opens and pings a Redis client, returning (nil, nil) when no
// URL is configured. Redis is optional — the feed cache, SSE, trust worker and
// rate limiter degrade to off — but a URL that is set and unreachable is a
// misconfiguration worth failing on rather than silently running degraded.
func connectRedis(ctx context.Context, redisURL string, logger *slog.Logger) (*redis.Client, error) {
	if redisURL == "" {
		return nil, nil
	}
	client, err := cache.NewRedisClient(redisURL)
	if err != nil {
		return nil, fmt.Errorf("parsing REDIS_URL: %w", err)
	}
	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		return nil, fmt.Errorf("pinging redis: %w", err)
	}
	logger.Info("redis connected")
	return client, nil
}

// initApp loads config, connects to the database, and runs migrations. It
// returns errors rather than exiting so callers own the exit code.
func initApp(ctx context.Context, logger *slog.Logger) (config.Config, *pgxpool.Pool, error) {
	cfg, err := config.Load()
	if err != nil {
		return config.Config{}, nil, fmt.Errorf("loading config: %w", err)
	}

	pool, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return config.Config{}, nil, fmt.Errorf("connecting to database: %w", err)
	}
	logger.Info("database connected")

	if err := database.RunMigrations(ctx, pool); err != nil {
		pool.Close()
		return config.Config{}, nil, fmt.Errorf("running migrations: %w", err)
	}
	logger.Info("migrations complete")

	return cfg, pool, nil
}
