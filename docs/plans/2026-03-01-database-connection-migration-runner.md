# Database Connection and Migration Runner Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Create the database connection pool and migration runner, then wire both into the application entry point so the Bell service can connect to PostgreSQL and apply schema migrations on startup.

**Architecture:** Two packages under `internal/database/`: one for pgxpool connection management, one for goose-based migration running. The migration runner embeds SQL files at compile time via `//go:embed`. Main.go orchestrates: load config → connect DB → run migrations → (future: start server) → graceful shutdown.

**Tech Stack:** pgx/v5 pgxpool (connection pool), pgx/v5 stdlib (sql.DB bridge for goose), pressly/goose/v3 (migrations), Go embed (compile-time SQL embedding)

**Beads Task:** `the-bell-zhd.8`

---

### Task 1: Create database connection package

**Files:**
- Create: `internal/database/database.go`
- Test: `internal/database/database_test.go`

**Step 1: Write the failing test**

Create `internal/database/database_test.go`:

```go
package database_test

import (
	"context"
	"testing"

	"github.com/fireynis/the-bell/internal/database"
)

func TestConnect_InvalidURL(t *testing.T) {
	ctx := context.Background()
	_, err := database.Connect(ctx, "postgres://invalid:5432/nonexistent?connect_timeout=1")
	if err == nil {
		t.Fatal("expected error for unreachable database, got nil")
	}
}

func TestConnect_MalformedURL(t *testing.T) {
	ctx := context.Background()
	_, err := database.Connect(ctx, "not-a-valid-url")
	if err == nil {
		t.Fatal("expected error for malformed URL, got nil")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /home/jeremy/services/the-bell && go test ./internal/database/ -v -run TestConnect`
Expected: FAIL — `database` package doesn't exist yet.

**Step 3: Write minimal implementation**

Create `internal/database/database.go`:

```go
package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Connect creates a pgxpool connection pool and verifies connectivity.
func Connect(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parsing database config: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connecting to database: %w", err)
	}

	return pool, nil
}
```

Notes for the implementer:
- `pgxpool.New` parses the connection string and creates the pool. It does NOT connect immediately.
- `pool.Ping` forces an actual connection attempt, which is why we call it — to fail fast if the DB is unreachable.
- On ping failure, close the pool to release resources before returning the error.
- Do NOT add pool configuration (MaxConns, etc.) — the defaults are fine and YAGNI.

**Step 4: Run test to verify it passes**

Run: `cd /home/jeremy/services/the-bell && go test ./internal/database/ -v -run TestConnect`
Expected: PASS — both tests should pass (malformed URL fails at parse, invalid URL fails at ping with timeout).

Note: `TestConnect_InvalidURL` uses `connect_timeout=1` to avoid a long wait. If the test hangs, the implementer may need to adjust the timeout or use a context with deadline.

**Step 5: Commit**

```bash
cd /home/jeremy/services/the-bell
git add internal/database/database.go internal/database/database_test.go
git commit -m "feat: add database connection pool with pgxpool"
```

---

### Task 2: Create migration runner package

**Files:**
- Create: `internal/database/migrate.go`
- Test: `internal/database/migrate_test.go`

**Step 1: Write the failing test**

Create `internal/database/migrate_test.go`:

```go
package database_test

import (
	"testing"

	"github.com/fireynis/the-bell/internal/database"
)

func TestMigrationsFS_ContainsFiles(t *testing.T) {
	entries, err := database.MigrationsFS.ReadDir("migrations")
	if err != nil {
		t.Fatalf("failed to read embedded migrations: %v", err)
	}

	if len(entries) == 0 {
		t.Fatal("expected at least one migration file, got none")
	}

	// Verify the first migration exists
	found := false
	for _, e := range entries {
		if e.Name() == "00001_enable_extensions.sql" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected to find 00001_enable_extensions.sql in embedded migrations")
	}
}
```

Notes for the implementer:
- This tests that the `//go:embed` directive correctly captures migration files.
- The `MigrationsFS` must be an exported `embed.FS` so tests (in `_test` package) and main.go can access it.
- We don't test `RunMigrations` with a unit test because it requires a real Postgres instance. Integration testing is deferred to the Docker Compose environment.

**Step 2: Run test to verify it fails**

Run: `cd /home/jeremy/services/the-bell && go test ./internal/database/ -v -run TestMigrationsFS`
Expected: FAIL — `MigrationsFS` doesn't exist yet.

**Step 3: Write minimal implementation**

Create `internal/database/migrate.go`:

```go
package database

import (
	"embed"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var MigrationsFS embed.FS

// RunMigrations applies all pending database migrations.
func RunMigrations(pool *pgxpool.Pool) error {
	db := stdlib.OpenDBFromPool(pool)

	goose.SetBaseFS(MigrationsFS)

	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("setting goose dialect: %w", err)
	}

	if err := goose.Up(db, "migrations"); err != nil {
		return fmt.Errorf("running migrations: %w", err)
	}

	return nil
}
```

**CRITICAL — file relocation required:** The `//go:embed migrations/*.sql` directive resolves paths relative to the source file's directory. Since `migrate.go` lives in `internal/database/`, Go will look for `internal/database/migrations/*.sql`. The migration files currently live at the repo root `migrations/`. There are two options:

**Option A (recommended): Symlink.** Create a symlink `internal/database/migrations` → `../../migrations`. This keeps migration SQL files at the repo root (where goose CLI expects them) while satisfying the embed directive.

```bash
cd /home/jeremy/services/the-bell
ln -s ../../migrations internal/database/migrations
```

**Option B: Move migrations into `internal/database/migrations/`.** This changes where the goose CLI looks for migrations (need `-dir internal/database/migrations`). Less conventional.

The implementer should use **Option A** (symlink).

**Step 4: Run test to verify it passes**

Run: `cd /home/jeremy/services/the-bell && go test ./internal/database/ -v -run TestMigrationsFS`
Expected: PASS — embedded FS contains the 7 migration files.

**Step 5: Commit**

```bash
cd /home/jeremy/services/the-bell
git add internal/database/migrate.go internal/database/migrate_test.go internal/database/migrations
git commit -m "feat: add goose migration runner with embedded SQL files"
```

---

### Task 3: Wire database into main.go

**Files:**
- Modify: `cmd/bell/main.go`

**Step 1: Write the updated main.go**

Replace the contents of `cmd/bell/main.go`:

```go
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/fireynis/the-bell/internal/config"
	"github.com/fireynis/the-bell/internal/database"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("loading config: %v", err)
	}

	pool, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("connecting to database: %v", err)
	}
	defer pool.Close()

	if err := database.RunMigrations(pool); err != nil {
		log.Fatalf("running migrations: %v", err)
	}

	log.Printf("the-bell: started (town=%s)", cfg.TownName)

	<-ctx.Done()
	log.Println("the-bell: shutting down")
}
```

Notes for the implementer:
- `signal.NotifyContext` creates a context that cancels on SIGINT/SIGTERM — this gives us graceful shutdown for free.
- The deferred `pool.Close()` ensures connections are released on shutdown.
- The `<-ctx.Done()` blocks until a shutdown signal is received. Later tasks (HTTP server, task 10) will replace this with `server.Start()`.
- Use `log` (stdlib) not a third-party logger — YAGNI.

**Step 2: Verify it compiles**

Run: `cd /home/jeremy/services/the-bell && go build ./cmd/bell/`
Expected: Compiles successfully, produces `bell` binary.

**Step 3: Run all tests to verify nothing is broken**

Run: `cd /home/jeremy/services/the-bell && go test ./... -v`
Expected: All tests pass (config, domain, service, database packages).

**Step 4: Clean up build artifact and commit**

```bash
cd /home/jeremy/services/the-bell
rm -f bell
git add cmd/bell/main.go
git commit -m "feat: wire database connection and migrations into main.go"
```

---

### Task 4: Remove .gitkeep placeholder (if present)

**Files:**
- Check/Remove: `internal/database/.gitkeep` or `internal/storage/.gitkeep` (if they exist — the explore showed empty placeholder dirs)

**Step 1: Check for stale .gitkeep files**

```bash
ls -la /home/jeremy/services/the-bell/internal/database/
```

If a `.gitkeep` exists, remove it since the directory now has real files.

**Step 2: Commit if needed**

```bash
cd /home/jeremy/services/the-bell
git rm internal/database/.gitkeep 2>/dev/null || true
git diff --cached --quiet || git commit -m "chore: remove database .gitkeep placeholder"
```

---

## Edge Cases and Risks

1. **Embed symlink support**: Go's `//go:embed` follows symlinks as of Go 1.22+. Since this project uses Go 1.26, this is safe. If symlinks cause issues, fall back to Option B (move migration files).

2. **pgxpool.New with malformed URL**: `pgxpool.New` can return an error at parse time (malformed URL) or silently succeed and fail on first use. That's why we call `pool.Ping()` immediately — to fail fast.

3. **Goose and pgx stdlib bridge**: `stdlib.OpenDBFromPool(pool)` creates a `*sql.DB` that borrows connections from the pgxpool. Do NOT close this `*sql.DB` — it doesn't own the connections. Closing it would be a no-op or cause issues. The pool itself manages connection lifecycle.

4. **Migration ordering**: Goose uses the numeric prefix (00001, 00002, ...) for ordering. The existing 7 migration files follow this convention. The AGE extension (00001) must run before the trust graph (00007) which depends on it.

5. **AGE extension availability**: Migration 00001 requires the Apache AGE extension to be installed in PostgreSQL. The Docker Compose uses a standard postgres image — the implementer should verify AGE is available or the service's Dockerfile installs it. If not, the migration runner will fail at `CREATE EXTENSION age`. This is an existing concern, not introduced by this task.

6. **Context cancellation during migrations**: If the user sends SIGTERM while migrations are running, `goose.Up` may be mid-transaction. Goose wraps each migration in a transaction, so a partially-applied migration will roll back. This is safe.

## Test Strategy

| Test | Type | What it validates |
|------|------|-------------------|
| `TestConnect_InvalidURL` | Unit | Returns error for unreachable DB (times out on ping) |
| `TestConnect_MalformedURL` | Unit | Returns error for unparseable connection string |
| `TestMigrationsFS_ContainsFiles` | Unit | Embed directive captures all migration SQL files |
| `go build ./cmd/bell/` | Build | Main.go compiles with all wiring |
| `go test ./...` | All | Full test suite remains green |

Integration testing (running migrations against a real Postgres) is deferred to the Docker Compose environment where `docker compose up` will exercise the full startup path.
