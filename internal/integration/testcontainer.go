//go:build integration

package integration

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/config"
	"github.com/fireynis/the-bell/internal/database"
	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/middleware"
	"github.com/fireynis/the-bell/internal/repository/postgres"
	"github.com/fireynis/the-bell/internal/server"
	"github.com/fireynis/the-bell/internal/service"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	testDBName = "thebell_test"
	testDBUser = "testuser"
	testDBPass = "testpass"
)

// testDB starts a Postgres+AGE container, runs migrations, and returns a
// connected pool. The container is destroyed when the test completes.
func testDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()

	// Use the apache/age image which bundles AGE on top of Postgres 16.
	container, err := tcpostgres.Run(ctx,
		"apache/age:PG16-latest",
		tcpostgres.WithDatabase(testDBName),
		tcpostgres.WithUsername(testDBUser),
		tcpostgres.WithPassword(testDBPass),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("starting postgres container: %v", err)
	}
	t.Cleanup(func() {
		if err := container.Terminate(ctx); err != nil {
			t.Logf("terminating container: %v", err)
		}
	})

	connStr, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("getting connection string: %v", err)
	}

	pool, err := database.Connect(ctx, connStr)
	if err != nil {
		t.Fatalf("connecting to test database: %v", err)
	}
	t.Cleanup(func() { pool.Close() })

	// AGE requires creating the extension before migrations can use it.
	// The migration files handle this, but we need to ensure the extension
	// is available. The apache/age image ships with AGE pre-installed.
	if err := database.RunMigrations(ctx, pool); err != nil {
		t.Fatalf("running migrations: %v", err)
	}

	return pool
}

// testUser is a helper to create a user directly in the database.
func testUser(t *testing.T, pool *pgxpool.Pool, kratosID string, role domain.Role, trust float64) *domain.User {
	t.Helper()
	q := postgres.New(pool)
	userRepo := postgres.NewUserRepo(q)
	svc := service.NewUserService(userRepo, time.Now)

	user, err := svc.FindOrCreate(context.Background(), kratosID)
	if err != nil {
		t.Fatalf("creating test user: %v", err)
	}

	// Update role and trust score.
	if role != domain.RolePending {
		if err := userRepo.UpdateUserRole(context.Background(), user.ID, role); err != nil {
			t.Fatalf("updating user role: %v", err)
		}
		user.Role = role
	}

	if trust != 50.0 {
		if err := userRepo.UpdateUserTrustScore(context.Background(), user.ID, trust); err != nil {
			t.Fatalf("updating user trust score: %v", err)
		}
		user.TrustScore = trust
	}

	return user
}

// mockAuthMiddleware returns middleware that injects the given user into the
// request context, simulating a successful Kratos authentication.
func mockAuthMiddleware(user *domain.User) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := middleware.WithUser(r.Context(), user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// testServer creates a fully wired Server for integration testing, using the
// given pool and injecting the authUser via mock auth middleware.
func testServer(t *testing.T, pool *pgxpool.Pool, authUser *domain.User) *server.Server {
	t.Helper()
	q := postgres.New(pool)

	userRepo := postgres.NewUserRepo(q)
	postRepo := postgres.NewPostRepo(q)
	vouchRepo := postgres.NewVouchRepo(q)
	reportRepo := postgres.NewReportRepo(q)
	modActionRepo := postgres.NewModerationActionRepo(q)
	penaltyRepo := postgres.NewPenaltyRepo(q)
	configRepo := postgres.NewConfigRepo(q)
	statsRepo := postgres.NewStatsRepo(q)
	voteRepo := postgres.NewVoteRepo(q)
	ageQuerier := postgres.NewAGEQuerier(pool)

	userSvc := service.NewUserService(userRepo, nil)
	postSvc := service.NewPostService(postRepo, nil)
	vouchSvc := service.NewVouchService(vouchRepo, ageQuerier, userRepo, nil)
	reportSvc := service.NewReportService(reportRepo, postRepo, nil)
	moderationSvc := service.NewModerationService(penaltyRepo, ageQuerier, nil)
	modActionSvc := service.NewModerationActionService(modActionRepo, userRepo, moderationSvc, userRepo, penaltyRepo, nil)
	approvalSvc := service.NewApprovalService(userRepo, configRepo)
	votingSvc := service.NewVotingService(voteRepo, nil)
	statsSvc := service.NewStatsService(statsRepo)

	logger := slog.Default()

	cfg := config.Config{
		Port:            0,
		DatabaseURL:     "unused",
		KratosPublicURL: "http://unused",
		KratosAdminURL:  "http://unused",
	}

	opts := []server.Option{
		server.WithPostService(postSvc),
		server.WithUserService(userSvc),
		server.WithVouchService(vouchSvc),
		server.WithReportService(reportSvc),
		server.WithModerationActionService(modActionSvc),
		server.WithApprovalService(approvalSvc),
		server.WithVotingService(votingSvc),
		server.WithStatsService(statsSvc),
		server.WithAuth(mockAuthMiddleware(authUser)),
	}

	return server.New(cfg, pool, logger, opts...)
}

// testServerWithDynamicAuth creates a server where the auth user can be swapped
// at runtime by updating the pointer.
func testServerWithDynamicAuth(t *testing.T, pool *pgxpool.Pool, authUserPtr **domain.User) *server.Server {
	t.Helper()
	q := postgres.New(pool)

	userRepo := postgres.NewUserRepo(q)
	postRepo := postgres.NewPostRepo(q)
	vouchRepo := postgres.NewVouchRepo(q)
	reportRepo := postgres.NewReportRepo(q)
	modActionRepo := postgres.NewModerationActionRepo(q)
	penaltyRepo := postgres.NewPenaltyRepo(q)
	configRepo := postgres.NewConfigRepo(q)
	statsRepo := postgres.NewStatsRepo(q)
	voteRepo := postgres.NewVoteRepo(q)
	ageQuerier := postgres.NewAGEQuerier(pool)

	userSvc := service.NewUserService(userRepo, nil)
	postSvc := service.NewPostService(postRepo, nil)
	vouchSvc := service.NewVouchService(vouchRepo, ageQuerier, userRepo, nil)
	reportSvc := service.NewReportService(reportRepo, postRepo, nil)
	moderationSvc := service.NewModerationService(penaltyRepo, ageQuerier, nil)
	modActionSvc := service.NewModerationActionService(modActionRepo, userRepo, moderationSvc, userRepo, penaltyRepo, nil)
	approvalSvc := service.NewApprovalService(userRepo, configRepo)
	votingSvc := service.NewVotingService(voteRepo, nil)
	statsSvc := service.NewStatsService(statsRepo)

	logger := slog.Default()

	cfg := config.Config{
		Port:            0,
		DatabaseURL:     "unused",
		KratosPublicURL: "http://unused",
		KratosAdminURL:  "http://unused",
	}

	dynamicAuth := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if *authUserPtr != nil {
				ctx := middleware.WithUser(r.Context(), *authUserPtr)
				next.ServeHTTP(w, r.WithContext(ctx))
			} else {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			}
		})
	}

	opts := []server.Option{
		server.WithPostService(postSvc),
		server.WithUserService(userSvc),
		server.WithVouchService(vouchSvc),
		server.WithReportService(reportSvc),
		server.WithModerationActionService(modActionSvc),
		server.WithApprovalService(approvalSvc),
		server.WithVotingService(votingSvc),
		server.WithStatsService(statsSvc),
		server.WithAuth(dynamicAuth),
	}

	return server.New(cfg, pool, logger, opts...)
}

// newTestServices creates raw services for tests that operate at the service
// layer rather than via HTTP.
type testServices struct {
	UserService             *service.UserService
	PostService             *service.PostService
	VouchService            *service.VouchService
	ReportService           *service.ReportService
	ModerationService       *service.ModerationService
	ModerationActionService *service.ModerationActionService
	AGEQuerier              *postgres.AGEQuerier
	UserRepo                *postgres.UserRepo
	PenaltyRepo             *postgres.PenaltyRepo
}

func newTestServices(pool *pgxpool.Pool) *testServices {
	q := postgres.New(pool)

	userRepo := postgres.NewUserRepo(q)
	postRepo := postgres.NewPostRepo(q)
	vouchRepo := postgres.NewVouchRepo(q)
	reportRepo := postgres.NewReportRepo(q)
	modActionRepo := postgres.NewModerationActionRepo(q)
	penaltyRepo := postgres.NewPenaltyRepo(q)
	ageQuerier := postgres.NewAGEQuerier(pool)

	userSvc := service.NewUserService(userRepo, nil)
	postSvc := service.NewPostService(postRepo, nil)
	vouchSvc := service.NewVouchService(vouchRepo, ageQuerier, userRepo, nil)
	reportSvc := service.NewReportService(reportRepo, postRepo, nil)
	moderationSvc := service.NewModerationService(penaltyRepo, ageQuerier, nil)
	modActionSvc := service.NewModerationActionService(modActionRepo, userRepo, moderationSvc, userRepo, penaltyRepo, nil)

	return &testServices{
		UserService:             userSvc,
		PostService:             postSvc,
		VouchService:            vouchSvc,
		ReportService:           reportSvc,
		ModerationService:       moderationSvc,
		ModerationActionService: modActionSvc,
		AGEQuerier:              ageQuerier,
		UserRepo:                userRepo,
		PenaltyRepo:             penaltyRepo,
	}
}

// uniqueKratosID generates a unique Kratos identity ID for test users.
func uniqueKratosID(suffix string) string {
	return fmt.Sprintf("kratos-%s-%d", suffix, time.Now().UnixNano())
}

// slogDiscard returns a logger that discards all output, useful for test
// middleware that requires a logger.
func slogDiscard() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
