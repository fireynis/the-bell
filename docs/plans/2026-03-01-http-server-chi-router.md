# HTTP Server and Chi Router — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Stand up the chi HTTP server with health check, request logging middleware, JSON content-type middleware, and graceful shutdown wired through main.go.

**Architecture:** A `Server` struct holds config, pgxpool, and redis client. It builds a chi router with global middleware (request ID, real IP, logging, recovery, clean path), mounts a `/health` endpoint, and stubs an `/api/v1` route group with JSON content-type. `main.go` switches from blocking on `<-ctx.Done()` to running the HTTP server with signal-driven graceful shutdown.

**Tech Stack:** Go 1.26, chi/v5, pgx/v5 (pgxpool), go-redis/v9, stdlib log/slog

**Beads task:** `the-bell-zhd.10`

---

## Task 1: ContentTypeJSON Middleware

**Files:**
- Create: `internal/middleware/json.go`
- Create: `internal/middleware/json_test.go`

**Step 1: Write the failing test**

Create `internal/middleware/json_test.go`:

```go
package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fireynis/the-bell/internal/middleware"
)

func TestContentTypeJSON_SetsHeader(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := middleware.ContentTypeJSON(inner)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	got := rec.Header().Get("Content-Type")
	want := "application/json"
	if got != want {
		t.Errorf("Content-Type = %q, want %q", got, want)
	}
}

func TestContentTypeJSON_CallsNext(t *testing.T) {
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})
	handler := middleware.ContentTypeJSON(inner)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("next handler was not called")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/middleware/ -run TestContentTypeJSON -v`
Expected: FAIL — `ContentTypeJSON` not defined.

**Step 3: Write minimal implementation**

Create `internal/middleware/json.go`:

```go
package middleware

import "net/http"

func ContentTypeJSON(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		next.ServeHTTP(w, r)
	})
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/middleware/ -run TestContentTypeJSON -v`
Expected: PASS (2 tests).

**Step 5: Commit**

```bash
git add internal/middleware/json.go internal/middleware/json_test.go
git commit -m "feat(middleware): add ContentTypeJSON middleware"
```

---

## Task 2: RequestLogger Middleware

**Files:**
- Create: `internal/middleware/logging.go`
- Modify: `internal/middleware/json_test.go` → actually create new file `internal/middleware/logging_test.go`

**Step 1: Write the failing test**

Create `internal/middleware/logging_test.go`:

```go
package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fireynis/the-bell/internal/middleware"
)

func TestRequestLogger_PassesThrough(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})
	handler := middleware.RequestLogger(inner)

	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
}

func TestRequestLogger_DefaultsTo200(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Don't call WriteHeader — should default to 200
		w.Write([]byte("ok"))
	})
	handler := middleware.RequestLogger(inner)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/middleware/ -run TestRequestLogger -v`
Expected: FAIL — `RequestLogger` not defined.

**Step 3: Write minimal implementation**

Create `internal/middleware/logging.go`:

```go
package middleware

import (
	"log/slog"
	"net/http"
	"time"
)

func RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		wrapped := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(wrapped, r)
		slog.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", wrapped.status,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/middleware/ -run TestRequestLogger -v`
Expected: PASS (2 tests).

**Step 5: Commit**

```bash
git add internal/middleware/logging.go internal/middleware/logging_test.go
git commit -m "feat(middleware): add RequestLogger middleware"
```

---

## Task 3: Health Check Handler

**Files:**
- Create: `internal/handler/health.go`
- Create: `internal/handler/health_test.go`

**Step 1: Write the failing test**

Create `internal/handler/health_test.go`:

```go
package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fireynis/the-bell/internal/handler"
)

func TestHealthCheck_ReturnsOK(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	handler.HealthCheck(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}

	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("body.status = %q, want %q", body["status"], "ok")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/handler/ -run TestHealthCheck -v`
Expected: FAIL — `HealthCheck` not defined.

**Step 3: Write minimal implementation**

Create `internal/handler/health.go`:

```go
package handler

import (
	"encoding/json"
	"net/http"
)

func HealthCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/handler/ -run TestHealthCheck -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/handler/health.go internal/handler/health_test.go
git commit -m "feat(handler): add health check endpoint"
```

---

## Task 4: Server Struct and Routes

**Files:**
- Create: `internal/server/server.go`
- Create: `internal/server/routes.go`
- Create: `internal/server/server_test.go`

This is the core task — creating the `Server` struct that holds dependencies, building the chi router, and wiring everything together.

**Step 1: Write the failing test**

Create `internal/server/server_test.go`:

```go
package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fireynis/the-bell/internal/config"
	"github.com/fireynis/the-bell/internal/server"
)

func TestRoutes_HealthCheck(t *testing.T) {
	cfg := config.Config{Port: 8080, TownName: "Test Town"}
	srv := server.New(cfg, nil, nil)

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("body.status = %q, want %q", body["status"], "ok")
	}
}

func TestRoutes_APIv1_ContentType(t *testing.T) {
	cfg := config.Config{Port: 8080, TownName: "Test Town"}
	srv := server.New(cfg, nil, nil)

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Any /api/v1 path should return JSON content type (even 404)
	resp, err := http.Get(ts.URL + "/api/v1/nonexistent")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}
}

func TestRoutes_NotFound(t *testing.T) {
	cfg := config.Config{Port: 8080, TownName: "Test Town"}
	srv := server.New(cfg, nil, nil)

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/nonexistent")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/server/ -v`
Expected: FAIL — `server.New` and `server.Handler` not defined.

**Step 3: Write server implementation**

Create `internal/server/server.go`:

```go
package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/fireynis/the-bell/internal/config"
)

type Server struct {
	cfg   config.Config
	db    *pgxpool.Pool
	redis *redis.Client
	http  *http.Server
}

func New(cfg config.Config, db *pgxpool.Pool, rdb *redis.Client) *Server {
	s := &Server{
		cfg:   cfg,
		db:    db,
		redis: rdb,
	}

	s.http = &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      s.routes(),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	return s
}

func (s *Server) Handler() http.Handler {
	return s.http.Handler
}

func (s *Server) Start() error {
	slog.Info("server listening", "addr", s.http.Addr)
	return s.http.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.http.Shutdown(ctx)
}
```

Create `internal/server/routes.go`:

```go
package server

import (
	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"github.com/fireynis/the-bell/internal/handler"
	"github.com/fireynis/the-bell/internal/middleware"
)

func (s *Server) routes() *chi.Mux {
	r := chi.NewRouter()

	// Global middleware
	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(middleware.RequestLogger)
	r.Use(chimw.Recoverer)
	r.Use(chimw.CleanPath)

	// Health check (no auth required)
	r.Get("/health", handler.HealthCheck)

	// API v1
	r.Route("/api/v1", func(r chi.Router) {
		r.Use(middleware.ContentTypeJSON)

		// Public routes will go here (auth flows)

		// Protected routes will go here (posts, users, etc.)
	})

	return r
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/server/ -v`
Expected: PASS (3 tests).

**Note on the API v1 content-type test:** Chi returns 405 Method Not Allowed for routes that exist but don't match the method, and the default mux handler for truly unknown paths. The ContentTypeJSON middleware applies to the `/api/v1` route group. If the test fails because chi's 404 handler doesn't go through the group middleware, we may need to add a `r.NotFound()` handler inside the group that returns a JSON 404. Adjust accordingly — the key point is that any successful API response has the JSON content type.

**Step 5: Commit**

```bash
git add internal/server/server.go internal/server/routes.go internal/server/server_test.go
git commit -m "feat(server): add Server struct, chi router, and route registration"
```

---

## Task 5: Wire Graceful Shutdown in main.go

**Files:**
- Modify: `cmd/bell/main.go`

**Step 1: Update main.go**

Replace `cmd/bell/main.go` with the full server startup. Key changes:
- Switch from `log.Fatalf` to `slog` with JSON handler
- Add Redis connection
- Create and start the `Server`
- Replace `<-ctx.Done()` with goroutine-based graceful shutdown

```go
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/fireynis/the-bell/internal/config"
	"github.com/fireynis/the-bell/internal/database"
	"github.com/fireynis/the-bell/internal/server"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	ctx := context.Background()

	// Connect to database
	pool, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()
	slog.Info("database connected")

	// Run migrations
	if err := database.RunMigrations(ctx, pool); err != nil {
		slog.Error("failed to run migrations", "error", err)
		os.Exit(1)
	}
	slog.Info("migrations complete")

	// Connect to Redis
	opts, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		slog.Error("failed to parse redis URL", "error", err)
		os.Exit(1)
	}
	rdb := redis.NewClient(opts)
	defer rdb.Close()

	// Start server
	srv := server.New(cfg, pool, rdb)

	// Graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh

		slog.Info("shutting down...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Error("shutdown error", "error", err)
		}
	}()

	if err := srv.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}
}
```

**Step 2: Verify compilation**

Run: `go build ./cmd/bell/`
Expected: builds successfully.

**Step 3: Run all tests**

Run: `go test ./... -v`
Expected: all tests pass (middleware, handler, server, config, database, domain, service).

**Step 4: Clean up and commit**

```bash
rm -f bell
git add cmd/bell/main.go internal/server/ internal/middleware/json.go internal/middleware/json_test.go internal/middleware/logging.go internal/middleware/logging_test.go internal/handler/health.go internal/handler/health_test.go
git commit -m "feat: add chi HTTP server with health check, logging, and graceful shutdown"
```

---

## Edge Cases and Risks

1. **`statusWriter.Write` not intercepted**: The `statusWriter` in `logging.go` wraps `WriteHeader` but not `Write`. If a handler calls `w.Write()` without explicit `WriteHeader`, Go's default behavior sends 200 implicitly via the underlying `ResponseWriter.Write` — but our `statusWriter.status` field defaults to 200, so the logged status is still correct. No issue here.

2. **`statusWriter` and `http.Flusher`/`http.Hijacker`**: Our `statusWriter` does not implement optional interfaces like `http.Flusher`. This is fine for now — no handlers use streaming or WebSockets. If needed later, add type assertions.

3. **Chi's 404 handler vs middleware**: Middleware applied to a `r.Route("/api/v1", ...)` group only runs for requests that match a route in that group. A request to `/api/v1/nonexistent` may not trigger the `ContentTypeJSON` middleware. The test for this (`TestRoutes_APIv1_ContentType`) may need adjustment — if it fails, either:
   - Add a `r.NotFound()` handler inside the group
   - Or remove the test and rely on per-handler content types
   - The implementation plan's test is a "nice to have" — the critical behavior is that actual API endpoints return JSON

4. **Redis connection not validated at startup**: The plan connects to Redis but doesn't ping it. If Redis is down, the server starts but Redis-dependent features fail. This is acceptable for Phase 1 — Redis is optional (caching only). A health check enhancement can come later.

5. **nil db/redis in tests**: `server.New(cfg, nil, nil)` works because no routes currently query the database or Redis. As handlers are added, tests will need to inject mocks or use testcontainers.

6. **`errors.Is(err, http.ErrServerClosed)`**: After `Shutdown()` is called, `ListenAndServe` returns `http.ErrServerClosed` — this is normal and should not cause a non-zero exit.

## Test Strategy

| Component | Test Type | What It Verifies |
|-----------|-----------|-----------------|
| `ContentTypeJSON` | Unit (httptest) | Sets `Content-Type: application/json`, calls next handler |
| `RequestLogger` | Unit (httptest) | Passes through response status, calls next handler |
| `HealthCheck` | Unit (httptest) | Returns 200, JSON content type, `{"status":"ok"}` |
| Server routes | Integration (httptest.NewServer) | `/health` returns 200+JSON, `/api/v1` group has JSON content type, unknown paths return 404 |
| `main.go` | Manual | `go build` compiles, `docker compose up` starts and responds to `/health` |

All tests use stdlib `net/http/httptest` — no external test deps needed. No database or Redis required for any test.
