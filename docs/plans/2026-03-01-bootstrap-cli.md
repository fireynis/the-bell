# Bootstrap CLI Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a `setup` subcommand to the bell binary that creates Kratos identities for initial council members and sets bootstrap mode in the database.

**Architecture:** Convert `cmd/bell/main.go` from a single-mode binary into a subcommand-style CLI using Go's `flag` package (no external dep). The `serve` subcommand runs the existing HTTP server; the `setup` subcommand creates Kratos identities via the Admin API and inserts corresponding local users with role=council. A new `town_config` migration stores `bootstrap_mode` as a key-value row.

**Tech Stack:** Go stdlib `flag`, Ory Kratos Admin API (`github.com/ory/kratos-client-go` v1.3.8), pgx, goose migrations, sqlc

---

**Prerequisite:** Close epic `the-bell-dpv` (all 4 subtasks done) then mark `the-bell-anx.1` in_progress.

```bash
bd close the-bell-dpv --reason="All 4 subtasks completed"
bd update the-bell-anx.1 --status=in_progress
```

---

### Task 1: Migration — town_config table

**Files:**
- Create: `migrations/00009_create_town_config.sql`

**Step 1: Write the migration**

```sql
-- +goose Up
CREATE TABLE town_config (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

INSERT INTO town_config (key, value) VALUES ('bootstrap_mode', 'false');

-- +goose Down
DROP TABLE IF EXISTS town_config;
```

**Step 2: Verify migration compiles with goose**

Run: `go build ./...`
Expected: PASS (goose embeds migrations via `migrations/embed.go` which uses `//go:embed *.sql`)

**Step 3: Commit**

```bash
git add migrations/00009_create_town_config.sql
git commit -m "feat: add town_config migration for bootstrap mode"
```

---

### Task 2: sqlc queries for town_config

**Files:**
- Create: `queries/town_config.sql`
- Regenerate: `internal/repository/postgres/town_config.sql.go` (via sqlc)

**Step 1: Write the sqlc query file**

```sql
-- name: GetTownConfig :one
SELECT value FROM town_config WHERE key = $1;

-- name: SetTownConfig :exec
INSERT INTO town_config (key, value) VALUES ($1, $2)
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;
```

**Step 2: Run sqlc generate**

Run: `sqlc generate`
Expected: generates `internal/repository/postgres/town_config.sql.go` with `GetTownConfig` and `SetTownConfig` methods

**Step 3: Verify it compiles**

Run: `go build ./...`
Expected: PASS

**Step 4: Commit**

```bash
git add queries/town_config.sql internal/repository/postgres/town_config.sql.go
git commit -m "feat: add sqlc queries for town_config table"
```

---

### Task 3: Bootstrap service — tests first

**Files:**
- Create: `internal/service/bootstrap_test.go`

**Step 1: Write the failing tests**

Test cases for `BootstrapService.Setup(ctx, emails)`:
1. **Happy path** — 2 emails → creates 2 Kratos identities, 2 local users with role=council and trust 100.0, sets bootstrap_mode=true
2. **Empty emails** — returns validation error
3. **Kratos create fails** — returns wrapped error, no local user created
4. **Local user create fails** — returns wrapped error
5. **Idempotent** — email already has Kratos identity → skips creation, still sets role to council

```go
package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
)

// mockKratosAdmin implements KratosAdmin for testing.
type mockKratosAdmin struct {
	identities map[string]string // email → kratosID
	createErr  error
}

func newMockKratosAdmin() *mockKratosAdmin {
	return &mockKratosAdmin{identities: make(map[string]string)}
}

func (m *mockKratosAdmin) CreateIdentity(_ context.Context, email, displayName, password string) (string, error) {
	if m.createErr != nil {
		return "", m.createErr
	}
	id := "kratos-" + email
	m.identities[email] = id
	return id, nil
}

// mockConfigRepo implements ConfigRepository for testing.
type mockConfigRepo struct {
	config map[string]string
	setErr error
}

func newMockConfigRepo() *mockConfigRepo {
	return &mockConfigRepo{config: make(map[string]string)}
}

func (m *mockConfigRepo) SetTownConfig(_ context.Context, key, value string) error {
	if m.setErr != nil {
		return m.setErr
	}
	m.config[key] = value
	return nil
}

func (m *mockConfigRepo) GetTownConfig(_ context.Context, key string) (string, error) {
	v, ok := m.config[key]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}

func TestBootstrapService_Setup_HappyPath(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	userRepo := newMockUserRepo()
	kratosAdmin := newMockKratosAdmin()
	configRepo := newMockConfigRepo()
	svc := NewBootstrapService(userRepo, kratosAdmin, configRepo, func() time.Time { return now })

	emails := []string{"alice@town.example", "bob@town.example"}
	err := svc.Setup(context.Background(), emails)
	if err != nil {
		t.Fatalf("Setup() unexpected error: %v", err)
	}

	// Verify 2 users created
	if len(userRepo.users) != 2 {
		t.Fatalf("expected 2 users, got %d", len(userRepo.users))
	}

	for _, email := range emails {
		kratosID := "kratos-" + email
		user, ok := userRepo.byKratos[kratosID]
		if !ok {
			t.Fatalf("user with kratos ID %q not found", kratosID)
		}
		if user.Role != domain.RoleCouncil {
			t.Errorf("user %s role = %q, want %q", email, user.Role, domain.RoleCouncil)
		}
		if user.TrustScore != 100.0 {
			t.Errorf("user %s trust = %f, want 100.0", email, user.TrustScore)
		}
		if user.DisplayName != email {
			t.Errorf("user %s display name = %q, want %q", email, user.DisplayName, email)
		}
	}

	// Verify bootstrap_mode set
	if configRepo.config["bootstrap_mode"] != "true" {
		t.Errorf("bootstrap_mode = %q, want %q", configRepo.config["bootstrap_mode"], "true")
	}
}

func TestBootstrapService_Setup_EmptyEmails(t *testing.T) {
	svc := NewBootstrapService(newMockUserRepo(), newMockKratosAdmin(), newMockConfigRepo(), nil)

	err := svc.Setup(context.Background(), nil)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("Setup() error = %v, want %v", err, ErrValidation)
	}

	err = svc.Setup(context.Background(), []string{})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("Setup([]) error = %v, want %v", err, ErrValidation)
	}
}

func TestBootstrapService_Setup_KratosError(t *testing.T) {
	kratosAdmin := newMockKratosAdmin()
	kratosAdmin.createErr = errors.New("kratos unavailable")
	svc := NewBootstrapService(newMockUserRepo(), kratosAdmin, newMockConfigRepo(), nil)

	err := svc.Setup(context.Background(), []string{"alice@town.example"})
	if err == nil {
		t.Fatal("Setup() expected error, got nil")
	}
	if !errors.Is(err, kratosAdmin.createErr) {
		// Should be wrapped
		if err.Error() == "" {
			t.Fatal("expected non-empty error")
		}
	}
}

func TestBootstrapService_Setup_UserCreateError(t *testing.T) {
	userRepo := newMockUserRepo()
	userRepo.createErr = errors.New("db connection failed")
	svc := NewBootstrapService(userRepo, newMockKratosAdmin(), newMockConfigRepo(), nil)

	err := svc.Setup(context.Background(), []string{"alice@town.example"})
	if err == nil {
		t.Fatal("Setup() expected error, got nil")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/service/ -run TestBootstrap -v`
Expected: FAIL — `NewBootstrapService` undefined

**Step 3: Commit**

```bash
git add internal/service/bootstrap_test.go
git commit -m "test: add bootstrap service tests (red)"
```

---

### Task 4: Bootstrap service — implementation

**Files:**
- Create: `internal/service/bootstrap.go`

**Step 1: Write the implementation**

```go
package service

import (
	"context"
	"fmt"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/google/uuid"
)

// KratosAdmin creates identities via the Kratos Admin API.
type KratosAdmin interface {
	CreateIdentity(ctx context.Context, email, displayName, password string) (kratosID string, err error)
}

// ConfigRepository reads and writes town_config key-value pairs.
type ConfigRepository interface {
	SetTownConfig(ctx context.Context, key, value string) error
	GetTownConfig(ctx context.Context, key string) (string, error)
}

// BootstrapService handles initial town setup.
type BootstrapService struct {
	users      UserRepository
	kratos     KratosAdmin
	config     ConfigRepository
	now        func() time.Time
}

func NewBootstrapService(users UserRepository, kratos KratosAdmin, config ConfigRepository, clock func() time.Time) *BootstrapService {
	if clock == nil {
		clock = time.Now
	}
	return &BootstrapService{
		users:  users,
		kratos: kratos,
		config: config,
		now:    clock,
	}
}

// Setup creates Kratos identities for the given emails, provisions local users
// as council members with max trust, and enables bootstrap mode.
func (s *BootstrapService) Setup(ctx context.Context, emails []string) error {
	if len(emails) == 0 {
		return fmt.Errorf("%w: at least one council email is required", ErrValidation)
	}

	for _, email := range emails {
		kratosID, err := s.kratos.CreateIdentity(ctx, email, email, "")
		if err != nil {
			return fmt.Errorf("creating kratos identity for %s: %w", email, err)
		}

		id, err := uuid.NewV7()
		if err != nil {
			return fmt.Errorf("generating user id: %w", err)
		}

		now := s.now()
		user := &domain.User{
			ID:               id.String(),
			KratosIdentityID: kratosID,
			DisplayName:      email,
			TrustScore:       100.0,
			Role:             domain.RoleCouncil,
			IsActive:         true,
			JoinedAt:         now,
			CreatedAt:        now,
			UpdatedAt:        now,
		}

		if err := s.users.CreateUser(ctx, user); err != nil {
			return fmt.Errorf("creating local user for %s: %w", email, err)
		}
	}

	if err := s.config.SetTownConfig(ctx, "bootstrap_mode", "true"); err != nil {
		return fmt.Errorf("setting bootstrap mode: %w", err)
	}

	return nil
}
```

**Step 2: Run the tests**

Run: `go test ./internal/service/ -run TestBootstrap -v`
Expected: PASS

**Step 3: Commit**

```bash
git add internal/service/bootstrap.go
git commit -m "feat: add bootstrap service for council setup"
```

---

### Task 5: Kratos Admin client adapter

**Files:**
- Create: `internal/kratos/admin.go`
- Create: `internal/kratos/admin_test.go`

**Step 1: Write the test (uses httptest to mock Kratos)**

```go
package kratos

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAdminClient_CreateIdentity_Success(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/admin/identities" {
			t.Errorf("path = %s, want /admin/identities", r.URL.Path)
		}

		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)

		traits := body["traits"].(map[string]interface{})
		if traits["email"] != "alice@example.com" {
			t.Errorf("email = %v, want alice@example.com", traits["email"])
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id": "kratos-identity-123",
		})
	})

	srv := httptest.NewServer(handler)
	defer srv.Close()

	client := NewAdminClient(srv.URL)
	kratosID, err := client.CreateIdentity(context.Background(), "alice@example.com", "Alice", "")
	if err != nil {
		t.Fatalf("CreateIdentity() error: %v", err)
	}
	if kratosID != "kratos-identity-123" {
		t.Errorf("kratosID = %q, want %q", kratosID, "kratos-identity-123")
	}
}

func TestAdminClient_CreateIdentity_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{"message": "identity already exists"},
		})
	}))
	defer srv.Close()

	client := NewAdminClient(srv.URL)
	_, err := client.CreateIdentity(context.Background(), "alice@example.com", "Alice", "")
	if err == nil {
		t.Fatal("CreateIdentity() expected error, got nil")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/kratos/ -run TestAdminClient -v`
Expected: FAIL — `NewAdminClient` undefined

**Step 3: Write the implementation**

```go
package kratos

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"

	client "github.com/ory/kratos-client-go"
)

// AdminClient wraps the Kratos Admin API for identity management.
type AdminClient struct {
	api client.IdentityAPI
}

// NewAdminClient creates an AdminClient pointing at the given Kratos Admin URL.
func NewAdminClient(adminURL string) *AdminClient {
	cfg := client.NewConfiguration()
	cfg.Servers = client.ServerConfigurations{{URL: adminURL}}
	apiClient := client.NewAPIClient(cfg)
	return &AdminClient{api: apiClient.IdentityAPI}
}

// CreateIdentity creates a Kratos identity with password credentials.
// If password is empty, a random 32-byte password is generated (the user
// can reset via recovery flow).
func (c *AdminClient) CreateIdentity(ctx context.Context, email, displayName, password string) (string, error) {
	if password == "" {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			return "", fmt.Errorf("generating random password: %w", err)
		}
		password = hex.EncodeToString(b)
	}

	body := client.NewCreateIdentityBody("default", map[string]interface{}{
		"email": email,
		"name":  displayName,
	})
	pwConfig := client.NewIdentityWithCredentialsPasswordConfig()
	pwConfig.SetPassword(password)
	pw := client.NewIdentityWithCredentialsPassword()
	pw.SetConfig(*pwConfig)
	creds := client.NewIdentityWithCredentials()
	creds.SetPassword(*pw)
	body.SetCredentials(*creds)

	state := "active"
	body.SetState(state)

	identity, _, err := c.api.CreateIdentity(ctx).CreateIdentityBody(*body).Execute()
	if err != nil {
		return "", fmt.Errorf("kratos create identity: %w", err)
	}

	return identity.GetId(), nil
}
```

**Step 4: Run tests**

Run: `go test ./internal/kratos/ -run TestAdminClient -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/kratos/admin.go internal/kratos/admin_test.go
git commit -m "feat: add Kratos Admin API client adapter"
```

---

### Task 6: Config repository adapter

**Files:**
- Create: `internal/repository/postgres/config.go`

**Step 1: Write the adapter**

This wraps the sqlc-generated `SetTownConfig` and `GetTownConfig` to satisfy the `service.ConfigRepository` interface.

```go
package postgres

import (
	"context"
	"errors"

	"github.com/fireynis/the-bell/internal/service"
	"github.com/jackc/pgx/v5"
)

// ConfigRepo adapts sqlc queries to the service.ConfigRepository interface.
type ConfigRepo struct {
	q *Queries
}

func NewConfigRepo(q *Queries) *ConfigRepo {
	return &ConfigRepo{q: q}
}

func (r *ConfigRepo) SetTownConfig(ctx context.Context, key, value string) error {
	return r.q.SetTownConfig(ctx, SetTownConfigParams{Key: key, Value: value})
}

func (r *ConfigRepo) GetTownConfig(ctx context.Context, key string) (string, error) {
	val, err := r.q.GetTownConfig(ctx, key)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", service.ErrNotFound
	}
	return val, err
}
```

**Step 2: Verify it compiles**

Run: `go build ./...`
Expected: PASS

**Step 3: Commit**

```bash
git add internal/repository/postgres/config.go
git commit -m "feat: add config repository adapter for town_config"
```

---

### Task 7: CLI subcommand structure

**Files:**
- Modify: `cmd/bell/main.go`

**Step 1: Write the failing test for the setup subcommand**

No separate test file needed — the setup command is a thin orchestrator tested via the service layer. Instead, verify it compiles and the help output works.

**Step 2: Refactor main.go to support subcommands**

```go
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
	defer pool.Close()
	logger.Info("database connected")

	if err := database.RunMigrations(ctx, pool); err != nil {
		logger.Error("running migrations", "error", err)
		os.Exit(1)
	}
	logger.Info("migrations complete")

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
	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "usage: bell setup --council email1,email2,...\n")
		os.Exit(1)
	}

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
		fmt.Fprintf(os.Stderr, "error: --council flag is required\n")
		os.Exit(1)
	}

	emails := strings.Split(councilFlag, ",")
	for i := range emails {
		emails[i] = strings.TrimSpace(emails[i])
	}

	ctx := context.Background()

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
	defer pool.Close()

	if err := database.RunMigrations(ctx, pool); err != nil {
		logger.Error("running migrations", "error", err)
		os.Exit(1)
	}

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

func connectDB(ctx context.Context, cfg config.Config, logger *slog.Logger) *pgxpool.Pool {
	pool, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("connecting to database", "error", err)
		os.Exit(1)
	}
	return pool
}
```

**Note:** This references `postgres.NewUserRepo` which needs to exist. Check if the existing codebase already has a user repository adapter, or if users are accessed differently. The existing codebase likely uses `postgres.Queries` directly wrapped in a service-level adapter. Examine `internal/repository/postgres/` for a `UserRepo` type. If it doesn't exist, create a minimal one (Task 8).

**Step 3: Verify it compiles**

Run: `go build ./cmd/bell/`
Expected: PASS (may fail if `NewUserRepo` doesn't exist — see Task 8)

**Step 4: Commit**

```bash
git add cmd/bell/main.go
git commit -m "feat: add CLI subcommand structure with serve and setup"
```

---

### Task 8: User repository adapter (if needed)

Check if `internal/repository/postgres/` already has a `UserRepo` that satisfies `service.UserRepository`. If not:

**Files:**
- Create: `internal/repository/postgres/user_repo.go`

**Step 1: Write the adapter**

```go
package postgres

import (
	"context"
	"errors"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/service"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// UserRepo adapts sqlc queries to the service.UserRepository interface.
type UserRepo struct {
	q *Queries
}

func NewUserRepo(q *Queries) *UserRepo {
	return &UserRepo{q: q}
}

func (r *UserRepo) CreateUser(ctx context.Context, user *domain.User) error {
	_, err := r.q.CreateUser(ctx, CreateUserParams{
		ID:               user.ID,
		KratosIdentityID: user.KratosIdentityID,
		DisplayName:      user.DisplayName,
		Bio:              user.Bio,
		AvatarUrl:        user.AvatarURL,
		TrustScore:       user.TrustScore,
		Role:             string(user.Role),
		IsActive:         user.IsActive,
		JoinedAt:         pgtype.Timestamptz{Time: user.JoinedAt, Valid: true},
		CreatedAt:        pgtype.Timestamptz{Time: user.CreatedAt, Valid: true},
		UpdatedAt:        pgtype.Timestamptz{Time: user.UpdatedAt, Valid: true},
	})
	return err
}

func (r *UserRepo) GetUserByID(ctx context.Context, id string) (*domain.User, error) {
	row, err := r.q.GetUserByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, service.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return rowToUser(row), nil
}

func (r *UserRepo) GetUserByKratosID(ctx context.Context, kratosID string) (*domain.User, error) {
	row, err := r.q.GetUserByKratosID(ctx, kratosID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, service.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return rowToUser(row), nil
}

func (r *UserRepo) UpdateUserProfile(ctx context.Context, id, displayName, bio, avatarURL string) (*domain.User, error) {
	row, err := r.q.UpdateUserProfile(ctx, UpdateUserProfileParams{
		ID:          id,
		DisplayName: displayName,
		Bio:         bio,
		AvatarUrl:   avatarURL,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, service.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return rowToUser(row), nil
}

func rowToUser(row User) *domain.User {
	return &domain.User{
		ID:               row.ID,
		KratosIdentityID: row.KratosIdentityID,
		DisplayName:      row.DisplayName,
		Bio:              row.Bio,
		AvatarURL:        row.AvatarUrl,
		TrustScore:       row.TrustScore,
		Role:             domain.Role(row.Role),
		IsActive:         row.IsActive,
		JoinedAt:         row.JoinedAt.Time,
		CreatedAt:        row.CreatedAt.Time,
		UpdatedAt:        row.UpdatedAt.Time,
	}
}
```

**Step 2: Verify it compiles**

Run: `go build ./...`
Expected: PASS

**Step 3: Commit**

```bash
git add internal/repository/postgres/user_repo.go
git commit -m "feat: add user repository adapter for service.UserRepository"
```

---

### Task 9: Update Dockerfile (if needed)

Check if the Dockerfile uses `CMD ["./bell"]` or similar. It needs to become `CMD ["./bell", "serve"]` for backward compatibility.

**Files:**
- Modify: `Dockerfile` (add `serve` as default command)

**Step 1: Check and update**

If Dockerfile has `CMD ["./bell"]`, change to `CMD ["./bell", "serve"]`.
If docker-compose.yml overrides the command, update there instead.

**Step 2: Commit**

```bash
git add Dockerfile
git commit -m "fix: update CMD to use 'serve' subcommand"
```

---

### Task 10: Run all tests and verify

**Step 1: Run the full test suite**

Run: `go test ./... -v`
Expected: ALL PASS

**Step 2: Run the build**

Run: `go build ./cmd/bell/`
Expected: PASS

**Step 3: Final commit if any fixups needed**

---

## Edge Cases

1. **Duplicate emails in --council flag** — The Kratos API will reject the second creation with a 409 Conflict. The service should handle this gracefully (currently wraps the error). Consider: should we skip duplicates silently?
2. **Kratos unavailable** — The setup command fails with a clear error message. The user retries.
3. **Database already has bootstrap_mode=true** — The upsert (`ON CONFLICT DO UPDATE`) is idempotent. Running setup twice re-creates users (Kratos will 409 on duplicate email). Consider adding a check: if bootstrap_mode is already true, warn and ask for `--force`.
4. **Empty password** — Random password generated. User must use recovery flow to set their real password.
5. **Invalid email format** — Kratos validates email format via the identity schema. Our service propagates the error.
6. **No existing `postgres.NewUserRepo`** — Task 8 covers this. Check first — the existing server wiring in `server.New()` may already have a different pattern for user repo construction. Match existing patterns.

## Test Strategy

- **Unit tests (Task 3):** Bootstrap service with mocked dependencies — covers business logic
- **Unit tests (Task 5):** Kratos admin client with httptest — covers API integration
- **Integration test (manual):** `docker compose up` then `docker compose exec bell ./bell setup --council admin@example.com` — verifies end-to-end
- **All existing tests:** `go test ./...` must still pass — no regressions
