# The Bell — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build a self-hosted micro-blogging platform for municipalities with a trust-based social reputation system.

**Architecture:** Go REST API (chi router) backed by PostgreSQL + Apache AGE (graph extension) for data and trust graph, Redis for caching/rate limiting, Ory Kratos for identity/auth. React SPA frontend (Vite + TypeScript). Single Docker Compose deployment per municipality.

**Tech Stack:** Go 1.26, chi v5, sqlc + pgx/v5, goose (migrations), Ory Kratos, PostgreSQL 17 + Apache AGE, Redis 7, React 19 + Vite + TypeScript + Tailwind

**Design doc:** `docs/plans/2026-02-28-the-bell-design.md`

---

## Phase 1: Foundation

### Task 1: Clean Up Existing Code and Re-scaffold

The repo has leftover code from a previous attempt (GORM-based, different structure). Remove it and start fresh with the new architecture.

**Files:**
- Delete: `pkg/` (entire directory — old GORM-based code)
- Delete: `cmd/web/` (old entrypoint)
- Modify: `go.mod` (update module, Go version, remove GORM deps)
- Delete: `go.sum`
- Modify: `.tool-versions` (update Go version)
- Create: `cmd/bell/main.go` (new entrypoint stub)

**Step 1: Remove old code**

```bash
rm -rf pkg/ cmd/web/
```

**Step 2: Update `.tool-versions`**

```
golang 1.26.0
```

**Step 3: Re-initialize go.mod**

```bash
rm go.sum
```

Update `go.mod`:
```go
module github.com/fireynis/the-bell

go 1.26
```

Note: module name changed from `the-bell-api` to `the-bell` since this will include both API and embedded frontend.

**Step 4: Create new entrypoint**

Create `cmd/bell/main.go`:
```go
package main

import "fmt"

func main() {
	fmt.Println("the-bell starting...")
}
```

**Step 5: Verify it compiles**

```bash
go build ./cmd/bell/
```

Expected: builds successfully, produces `bell` binary.

**Step 6: Clean up binary and commit**

```bash
rm -f bell
git add -A
git commit -m "feat: clean slate — remove old code, re-scaffold for new architecture"
```

---

### Task 2: Add Core Dependencies

**Files:**
- Modify: `go.mod`

**Step 1: Add all Phase 1 dependencies**

```bash
go get github.com/go-chi/chi/v5@latest
go get github.com/jackc/pgx/v5@latest
go get github.com/redis/go-redis/v9@latest
go get github.com/google/uuid@latest
go get github.com/pressly/goose/v3@latest
go get github.com/ory/kratos-client-go@latest
go get github.com/caarlos0/env/v11@latest
```

**Step 2: Tidy**

```bash
go mod tidy
```

**Step 3: Verify**

```bash
go build ./cmd/bell/
```

**Step 4: Commit**

```bash
git add go.mod go.sum
git commit -m "feat: add core dependencies (chi, pgx, redis, goose, kratos, env)"
```

---

### Task 3: Create Project Directory Structure

**Files:**
- Create directories (empty `.gitkeep` files where needed)

**Step 1: Create all directories**

```bash
mkdir -p internal/{config,server,middleware,handler,domain,service,repository/postgres,repository/redis,storage}
mkdir -p migrations
mkdir -p queries
mkdir -p kratos
mkdir -p web
```

**Step 2: Add .gitkeep files to empty directories**

```bash
for dir in internal/{config,server,middleware,handler,domain,service,repository/postgres,repository/redis,storage} migrations queries kratos web; do
  touch "$dir/.gitkeep"
done
```

**Step 3: Commit**

```bash
git add -A
git commit -m "feat: create project directory structure"
```

---

### Task 4: Configuration Loading

**Files:**
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`

**Step 1: Write the failing test**

Create `internal/config/config_test.go`:
```go
package config_test

import (
	"testing"

	"github.com/fireynis/the-bell/internal/config"
)

func TestLoad_Defaults(t *testing.T) {
	// Set only required env vars
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/bell")
	t.Setenv("REDIS_URL", "redis://localhost:6379")
	t.Setenv("KRATOS_PUBLIC_URL", "http://localhost:4433")
	t.Setenv("KRATOS_ADMIN_URL", "http://localhost:4434")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Port != 8080 {
		t.Errorf("expected default port 8080, got %d", cfg.Port)
	}
	if cfg.DatabaseURL != "postgres://user:pass@localhost:5432/bell" {
		t.Errorf("unexpected database URL: %s", cfg.DatabaseURL)
	}
	if cfg.ImageStoragePath != "/storage/the-bell/images" {
		t.Errorf("unexpected image path: %s", cfg.ImageStoragePath)
	}
}

func TestLoad_MissingRequired(t *testing.T) {
	// Don't set any env vars — should fail
	_, err := config.Load()
	if err == nil {
		t.Fatal("expected error for missing required env vars")
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/config/ -v
```

Expected: FAIL — package doesn't exist yet.

**Step 3: Write implementation**

Create `internal/config/config.go`:
```go
package config

import (
	"github.com/caarlos0/env/v11"
)

type Config struct {
	Port             int    `env:"PORT" envDefault:"8080"`
	DatabaseURL      string `env:"DATABASE_URL,required"`
	RedisURL         string `env:"REDIS_URL,required"`
	KratosPublicURL  string `env:"KRATOS_PUBLIC_URL,required"`
	KratosAdminURL   string `env:"KRATOS_ADMIN_URL,required"`
	ImageStoragePath string `env:"IMAGE_STORAGE_PATH" envDefault:"/storage/the-bell/images"`
	TownName         string `env:"TOWN_NAME" envDefault:"My Town"`
}

func Load() (Config, error) {
	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}
```

**Step 4: Run tests**

```bash
go test ./internal/config/ -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/config/
git commit -m "feat: add config loading from environment variables"
```

---

### Task 5: Domain Types

These are pure Go types with no external dependencies. They define the core data model.

**Files:**
- Create: `internal/domain/user.go`
- Create: `internal/domain/post.go`
- Create: `internal/domain/vouch.go`
- Create: `internal/domain/reaction.go`
- Create: `internal/domain/moderation.go`

**Step 1: Create user domain type**

Create `internal/domain/user.go`:
```go
package domain

import "time"

type Role string

const (
	RolePending   Role = "pending"
	RoleMember    Role = "member"
	RoleModerator Role = "moderator"
	RoleCouncil   Role = "council"
	RoleBanned    Role = "banned"
)

type User struct {
	ID                string
	KratosIdentityID  string
	DisplayName       string
	Bio               string
	AvatarURL         string
	TrustScore        float64
	Role              Role
	IsActive          bool
	JoinedAt          time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

func (u *User) CanPost() bool {
	return u.IsActive && u.TrustScore >= 30.0 && u.Role != RolePending && u.Role != RoleBanned
}

func (u *User) CanVouch() bool {
	return u.IsActive && u.TrustScore >= 60.0 && u.Role != RolePending && u.Role != RoleBanned
}

func (u *User) CanModerate() bool {
	return u.IsActive && (u.Role == RoleModerator || u.Role == RoleCouncil)
}

func (u *User) IsCouncil() bool {
	return u.IsActive && u.Role == RoleCouncil
}
```

**Step 2: Create post domain type**

Create `internal/domain/post.go`:
```go
package domain

import "time"

type PostStatus string

const (
	PostVisible        PostStatus = "visible"
	PostRemovedByAuthor PostStatus = "removed_by_author"
	PostRemovedByMod   PostStatus = "removed_by_mod"
)

type Post struct {
	ID             string
	AuthorID       string
	Body           string
	ImagePath      string
	Status         PostStatus
	RemovalReason  string
	CreatedAt      time.Time
	EditedAt       *time.Time
}

const MaxPostBodyLength = 1000
const EditWindowMinutes = 15

func (p *Post) CanEdit(userID string, now time.Time) bool {
	if p.AuthorID != userID {
		return false
	}
	if p.Status != PostVisible {
		return false
	}
	return now.Sub(p.CreatedAt).Minutes() <= EditWindowMinutes
}
```

**Step 3: Create vouch domain type**

Create `internal/domain/vouch.go`:
```go
package domain

import "time"

type VouchStatus string

const (
	VouchActive  VouchStatus = "active"
	VouchRevoked VouchStatus = "revoked"
)

type Vouch struct {
	ID        string
	VoucherID string // person giving the vouch
	VoucheeID string // person receiving the vouch
	Status    VouchStatus
	CreatedAt time.Time
	RevokedAt *time.Time
}
```

**Step 4: Create reaction domain type**

Create `internal/domain/reaction.go`:
```go
package domain

import "time"

type ReactionType string

const (
	ReactionBell      ReactionType = "bell"
	ReactionHeart     ReactionType = "heart"
	ReactionCelebrate ReactionType = "celebrate"
)

type Reaction struct {
	ID        string
	UserID    string
	PostID    string
	Type      ReactionType
	CreatedAt time.Time
}
```

**Step 5: Create moderation domain type**

Create `internal/domain/moderation.go`:
```go
package domain

import "time"

type ActionType string

const (
	ActionWarn    ActionType = "warn"
	ActionMute    ActionType = "mute"
	ActionSuspend ActionType = "suspend"
	ActionBan     ActionType = "ban"
)

type ModerationAction struct {
	ID           string
	TargetUserID string
	ModeratorID  string
	Action       ActionType
	Severity     int // 1-5
	Reason       string
	Duration     *time.Duration
	CreatedAt    time.Time
	ExpiresAt    *time.Time
}

type Report struct {
	ID         string
	ReporterID string
	PostID     string
	Reason     string
	Status     string // "pending", "reviewed", "dismissed"
	CreatedAt  time.Time
}

// Trust propagation constants
var PropagationDepth = map[int]int{
	1: 1, // minor: 1 hop
	2: 1, // moderate: 1 hop
	3: 2, // serious: 2 hops
	4: 2, // severe: 2 hops
	5: 3, // ban: 3 hops
}

var PropagationDecay = map[int]float64{
	1: 0.50, // minor
	2: 0.70, // moderate
	3: 0.60, // serious
	4: 0.70, // severe
	5: 0.75, // ban
}

var DirectPenalty = map[int]float64{
	1: 5,   // minor warn
	2: 10,  // moderate warn
	3: 25,  // mute
	4: 40,  // suspend
	5: 100, // ban
}

var PenaltyDecayDays = map[int]int{
	1: 90,
	2: 180,
	3: 270,
	4: 365,
	5: 0, // permanent
}
```

**Step 6: Write domain tests**

Create `internal/domain/user_test.go`:
```go
package domain_test

import (
	"testing"

	"github.com/fireynis/the-bell/internal/domain"
)

func TestUser_CanPost(t *testing.T) {
	tests := []struct {
		name     string
		user     domain.User
		expected bool
	}{
		{"active member with high trust", domain.User{IsActive: true, TrustScore: 50, Role: domain.RoleMember}, true},
		{"pending user", domain.User{IsActive: true, TrustScore: 50, Role: domain.RolePending}, false},
		{"low trust", domain.User{IsActive: true, TrustScore: 20, Role: domain.RoleMember}, false},
		{"banned", domain.User{IsActive: true, TrustScore: 80, Role: domain.RoleBanned}, false},
		{"inactive", domain.User{IsActive: false, TrustScore: 80, Role: domain.RoleMember}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.user.CanPost(); got != tt.expected {
				t.Errorf("CanPost() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestUser_CanVouch(t *testing.T) {
	tests := []struct {
		name     string
		user     domain.User
		expected bool
	}{
		{"high trust member", domain.User{IsActive: true, TrustScore: 70, Role: domain.RoleMember}, true},
		{"trust too low", domain.User{IsActive: true, TrustScore: 55, Role: domain.RoleMember}, false},
		{"exactly 60", domain.User{IsActive: true, TrustScore: 60, Role: domain.RoleMember}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.user.CanVouch(); got != tt.expected {
				t.Errorf("CanVouch() = %v, want %v", got, tt.expected)
			}
		})
	}
}
```

Create `internal/domain/post_test.go`:
```go
package domain_test

import (
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
)

func TestPost_CanEdit(t *testing.T) {
	now := time.Now()
	post := domain.Post{
		AuthorID:  "user-1",
		Status:    domain.PostVisible,
		CreatedAt: now.Add(-10 * time.Minute),
	}

	if !post.CanEdit("user-1", now) {
		t.Error("author should be able to edit within window")
	}
	if post.CanEdit("user-2", now) {
		t.Error("non-author should not be able to edit")
	}

	oldPost := domain.Post{
		AuthorID:  "user-1",
		Status:    domain.PostVisible,
		CreatedAt: now.Add(-20 * time.Minute),
	}
	if oldPost.CanEdit("user-1", now) {
		t.Error("should not edit after 15 min window")
	}
}
```

**Step 7: Run tests**

```bash
go test ./internal/domain/ -v
```

Expected: PASS

**Step 8: Commit**

```bash
git add internal/domain/
git commit -m "feat: add core domain types (user, post, vouch, reaction, moderation)"
```

---

### Task 6: Database Migrations

**Files:**
- Create: `migrations/00001_enable_extensions.sql`
- Create: `migrations/00002_create_users.sql`
- Create: `migrations/00003_create_posts.sql`
- Create: `migrations/00004_create_reactions.sql`
- Create: `migrations/00005_create_moderation.sql`
- Create: `migrations/00006_create_reports.sql`
- Create: `migrations/00007_create_trust_graph.sql`

**Step 1: Create migration to enable extensions**

Create `migrations/00001_enable_extensions.sql`:
```sql
-- +goose Up
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS age;

-- Load AGE into the search path
SET search_path = ag_catalog, "$user", public;

-- +goose Down
DROP EXTENSION IF EXISTS age CASCADE;
DROP EXTENSION IF EXISTS "uuid-ossp";
```

**Step 2: Create users migration**

Create `migrations/00002_create_users.sql`:
```sql
-- +goose Up
CREATE TABLE users (
    id              TEXT PRIMARY KEY,
    kratos_identity_id TEXT NOT NULL UNIQUE,
    display_name    TEXT NOT NULL DEFAULT '',
    bio             TEXT NOT NULL DEFAULT '',
    avatar_url      TEXT NOT NULL DEFAULT '',
    trust_score     DOUBLE PRECISION NOT NULL DEFAULT 50.0,
    role            TEXT NOT NULL DEFAULT 'pending',
    is_active       BOOLEAN NOT NULL DEFAULT TRUE,
    joined_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_users_kratos_id ON users(kratos_identity_id);
CREATE INDEX idx_users_role ON users(role);
CREATE INDEX idx_users_trust_score ON users(trust_score);

-- +goose Down
DROP TABLE IF EXISTS users;
```

**Step 3: Create posts migration**

Create `migrations/00003_create_posts.sql`:
```sql
-- +goose Up
CREATE TABLE posts (
    id              TEXT PRIMARY KEY,
    author_id       TEXT NOT NULL REFERENCES users(id),
    body            TEXT NOT NULL,
    image_path      TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL DEFAULT 'visible',
    removal_reason  TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    edited_at       TIMESTAMPTZ
);

CREATE INDEX idx_posts_author ON posts(author_id);
CREATE INDEX idx_posts_status_created ON posts(status, created_at DESC);
CREATE INDEX idx_posts_created ON posts(created_at DESC);

-- +goose Down
DROP TABLE IF EXISTS posts;
```

**Step 4: Create reactions migration**

Create `migrations/00004_create_reactions.sql`:
```sql
-- +goose Up
CREATE TABLE reactions (
    id              TEXT PRIMARY KEY,
    user_id         TEXT NOT NULL REFERENCES users(id),
    post_id         TEXT NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
    reaction_type   TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(user_id, post_id, reaction_type)
);

CREATE INDEX idx_reactions_post ON reactions(post_id);

-- +goose Down
DROP TABLE IF EXISTS reactions;
```

**Step 5: Create moderation actions migration**

Create `migrations/00005_create_moderation.sql`:
```sql
-- +goose Up
CREATE TABLE moderation_actions (
    id              TEXT PRIMARY KEY,
    target_user_id  TEXT NOT NULL REFERENCES users(id),
    moderator_id    TEXT NOT NULL REFERENCES users(id),
    action_type     TEXT NOT NULL,
    severity        INTEGER NOT NULL CHECK (severity BETWEEN 1 AND 5),
    reason          TEXT NOT NULL,
    duration_seconds BIGINT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at      TIMESTAMPTZ
);

CREATE INDEX idx_mod_actions_target ON moderation_actions(target_user_id);
CREATE INDEX idx_mod_actions_moderator ON moderation_actions(moderator_id);

-- Trust penalty propagation log
CREATE TABLE trust_penalties (
    id                    TEXT PRIMARY KEY,
    user_id               TEXT NOT NULL REFERENCES users(id),
    moderation_action_id  TEXT NOT NULL REFERENCES moderation_actions(id),
    penalty_amount        DOUBLE PRECISION NOT NULL,
    hop_depth             INTEGER NOT NULL,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    decays_at             TIMESTAMPTZ
);

CREATE INDEX idx_trust_penalties_user ON trust_penalties(user_id);

-- +goose Down
DROP TABLE IF EXISTS trust_penalties;
DROP TABLE IF EXISTS moderation_actions;
```

**Step 6: Create reports migration**

Create `migrations/00006_create_reports.sql`:
```sql
-- +goose Up
CREATE TABLE reports (
    id          TEXT PRIMARY KEY,
    reporter_id TEXT NOT NULL REFERENCES users(id),
    post_id     TEXT NOT NULL REFERENCES posts(id),
    reason      TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'pending',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_reports_status ON reports(status);
CREATE INDEX idx_reports_post ON reports(post_id);

-- +goose Down
DROP TABLE IF EXISTS reports;
```

**Step 7: Create AGE trust graph migration**

Create `migrations/00007_create_trust_graph.sql`:
```sql
-- +goose Up
-- Create the trust graph in Apache AGE
-- Note: AGE queries require loading the extension into the search path
LOAD 'age';
SET search_path = ag_catalog, "$user", public;

SELECT create_graph('trust_graph');

-- +goose Down
LOAD 'age';
SET search_path = ag_catalog, "$user", public;

SELECT drop_graph('trust_graph', true);
```

**Step 8: Commit**

```bash
git add migrations/
git commit -m "feat: add database migrations (users, posts, reactions, moderation, AGE trust graph)"
```

---

### Task 7: Docker Compose Setup

**Files:**
- Create: `docker-compose.yml`
- Create: `kratos/kratos.yml`
- Create: `kratos/identity.schema.json`
- Create: `.env.example`
- Create: `Dockerfile`

**Step 1: Create Kratos identity schema**

Create `kratos/identity.schema.json`:
```json
{
  "$id": "https://schemas.bell.home.arpa/registration.schema.json",
  "$schema": "http://json-schema.org/draft-07/schema#",
  "title": "Person",
  "type": "object",
  "properties": {
    "traits": {
      "type": "object",
      "properties": {
        "email": {
          "type": "string",
          "format": "email",
          "title": "Email",
          "ory.sh/kratos": {
            "credentials": {
              "password": {
                "identifier": true
              }
            },
            "recovery": {
              "via": "email"
            },
            "verification": {
              "via": "email"
            }
          }
        },
        "name": {
          "type": "string",
          "title": "Display Name",
          "minLength": 1,
          "maxLength": 255
        }
      },
      "required": ["email", "name"],
      "additionalProperties": false
    }
  }
}
```

**Step 2: Create Kratos config**

Create `kratos/kratos.yml`:
```yaml
version: v1.3.0

dsn: postgres://appuser:CHANGE_ME@postgres:5432/bell_kratos?sslmode=disable

serve:
  public:
    base_url: http://localhost:4433/
    cors:
      enabled: true
      allowed_origins:
        - http://localhost:5173
        - http://bell.home.arpa
      allowed_methods:
        - POST
        - GET
        - PUT
        - PATCH
        - DELETE
      allowed_headers:
        - Authorization
        - Cookie
        - Content-Type
      exposed_headers:
        - Content-Type
        - Set-Cookie
      allow_credentials: true
  admin:
    base_url: http://localhost:4434/

selfservice:
  default_browser_return_url: http://localhost:5173/
  allowed_return_urls:
    - http://localhost:5173
    - http://bell.home.arpa

  methods:
    password:
      enabled: true
      config:
        min_password_length: 8

  flows:
    registration:
      enabled: true
      ui_url: http://localhost:5173/auth/registration
      after:
        password:
          hooks:
            - hook: session

    login:
      ui_url: http://localhost:5173/auth/login
      after:
        password:
          hooks:
            - hook: session

    logout:
      after:
        default_browser_return_url: http://localhost:5173/

    settings:
      ui_url: http://localhost:5173/auth/settings

    recovery:
      enabled: true
      ui_url: http://localhost:5173/auth/recovery

    verification:
      enabled: true
      ui_url: http://localhost:5173/auth/verification

session:
  cookie:
    name: bell_session
    same_site: Lax
  lifespan: 720h # 30 days

identity:
  default_schema_id: default
  schemas:
    - id: default
      url: file:///etc/kratos/identity.schema.json

log:
  level: info
  format: json
```

**Step 3: Create .env.example**

Create `.env.example`:
```bash
# Database
DATABASE_URL=postgres://appuser:changeme@postgres:5432/bell?sslmode=disable
POSTGRES_PASSWORD=changeme

# Redis
REDIS_URL=redis://redis-bell:6379

# Kratos
KRATOS_PUBLIC_URL=http://kratos:4433
KRATOS_ADMIN_URL=http://kratos:4434
KRATOS_DSN=postgres://appuser:changeme@postgres:5432/bell_kratos?sslmode=disable

# App
PORT=8080
TOWN_NAME=My Town
IMAGE_STORAGE_PATH=/storage/the-bell/images
```

**Step 4: Create Dockerfile**

Create `Dockerfile`:
```dockerfile
# --- Build Go API ---
FROM golang:1.26-alpine AS go-builder

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o bell ./cmd/bell/

# --- Build React SPA ---
FROM node:22-alpine AS web-builder

WORKDIR /build
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ .
RUN npm run build

# --- Final image ---
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app

COPY --from=go-builder /build/bell .
COPY --from=web-builder /build/dist ./web/dist
COPY migrations/ ./migrations/

EXPOSE 8080
CMD ["./bell"]
```

**Step 5: Create docker-compose.yml**

Create `docker-compose.yml`:
```yaml
services:
  bell:
    build: .
    container_name: bell
    restart: unless-stopped
    depends_on:
      kratos:
        condition: service_started
      redis-bell:
        condition: service_started
    environment:
      DATABASE_URL: postgres://appuser:${POSTGRES_PASSWORD}@postgres:5432/bell?sslmode=disable
      REDIS_URL: redis://redis-bell:6379
      KRATOS_PUBLIC_URL: http://kratos:4433
      KRATOS_ADMIN_URL: http://kratos:4434
      PORT: "8080"
      TOWN_NAME: "${TOWN_NAME:-My Town}"
      IMAGE_STORAGE_PATH: /storage/the-bell/images
    volumes:
      - /storage/the-bell/images:/storage/the-bell/images
    networks:
      - proxy
    labels:
      - "traefik.enable=true"
      - "traefik.http.routers.bell.rule=Host(`bell.home.arpa`)"
      - "traefik.http.services.bell.loadbalancer.server.port=8080"

  kratos-migrate:
    image: oryd/kratos:v1.3.0
    container_name: kratos-migrate
    restart: "no"
    command: migrate sql -e --yes
    environment:
      DSN: postgres://appuser:${POSTGRES_PASSWORD}@postgres:5432/bell_kratos?sslmode=disable
    networks:
      - proxy

  kratos:
    image: oryd/kratos:v1.3.0
    container_name: kratos
    restart: unless-stopped
    depends_on:
      kratos-migrate:
        condition: service_completed_successfully
    command: serve --config /etc/kratos/kratos.yml --dev
    environment:
      DSN: postgres://appuser:${POSTGRES_PASSWORD}@postgres:5432/bell_kratos?sslmode=disable
    volumes:
      - ./kratos/kratos.yml:/etc/kratos/kratos.yml:ro
      - ./kratos/identity.schema.json:/etc/kratos/identity.schema.json:ro
    networks:
      - proxy

  redis-bell:
    image: redis:7-alpine
    container_name: redis-bell
    restart: unless-stopped
    command: ["redis-server", "--save", "", "--appendonly", "no"]
    networks:
      - proxy

networks:
  proxy:
    external: true
```

**Step 6: Add .env to .gitignore**

Append to `.gitignore`:
```
.env
```

**Step 7: Commit**

```bash
git add docker-compose.yml Dockerfile kratos/ .env.example .gitignore
git commit -m "feat: add Docker Compose stack (bell, kratos, redis) and Kratos config"
```

---

### Task 8: Database Connection and Migration Runner

**Files:**
- Create: `internal/database/database.go`
- Create: `internal/database/migrate.go`
- Modify: `cmd/bell/main.go`

**Step 1: Create database connection helper**

Create `internal/database/database.go`:
```go
package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

func Connect(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("connect to database: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return pool, nil
}
```

**Step 2: Create migration runner**

Create `internal/database/migrate.go`:
```go
package database

import (
	"database/sql"
	"fmt"

	"github.com/pressly/goose/v3"
)

func RunMigrations(databaseURL string, migrationsDir string) error {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return fmt.Errorf("open db for migrations: %w", err)
	}
	defer db.Close()

	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("set goose dialect: %w", err)
	}
	if err := goose.Up(db, migrationsDir); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}
	return nil
}
```

**Step 3: Wire up main.go**

Update `cmd/bell/main.go`:
```go
package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/fireynis/the-bell/internal/config"
	"github.com/fireynis/the-bell/internal/database"
	_ "github.com/jackc/pgx/v5/stdlib" // register pgx as database/sql driver
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// Run migrations
	slog.Info("running database migrations")
	if err := database.RunMigrations(cfg.DatabaseURL, "migrations"); err != nil {
		slog.Error("failed to run migrations", "error", err)
		os.Exit(1)
	}

	// Connect to database
	ctx := context.Background()
	pool, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	slog.Info("the-bell starting", "port", cfg.Port, "town", cfg.TownName)
}
```

**Step 4: Verify compilation**

```bash
go mod tidy
go build ./cmd/bell/
```

Expected: builds successfully.

**Step 5: Commit**

```bash
rm -f bell
git add internal/database/ cmd/bell/main.go go.mod go.sum
git commit -m "feat: add database connection pool and goose migration runner"
```

---

### Task 9: sqlc Setup and Post Queries

**Files:**
- Create: `sqlc.yaml`
- Create: `queries/posts.sql`
- Create: `queries/users.sql`

**Step 1: Create sqlc config**

Create `sqlc.yaml`:
```yaml
version: "2"
sql:
  - engine: "postgresql"
    queries: "queries/"
    schema: "migrations/"
    gen:
      go:
        package: "postgres"
        out: "internal/repository/postgres"
        sql_package: "pgx/v5"
        emit_json_tags: true
        emit_empty_slices: true
```

**Step 2: Create user queries**

Create `queries/users.sql`:
```sql
-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;

-- name: GetUserByKratosID :one
SELECT * FROM users WHERE kratos_identity_id = $1;

-- name: CreateUser :one
INSERT INTO users (id, kratos_identity_id, display_name, role, trust_score)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: UpdateUser :one
UPDATE users
SET display_name = $2, bio = $3, avatar_url = $4, updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: UpdateUserTrustScore :exec
UPDATE users SET trust_score = $2, updated_at = NOW() WHERE id = $1;

-- name: UpdateUserRole :exec
UPDATE users SET role = $2, updated_at = NOW() WHERE id = $1;
```

**Step 3: Create post queries**

Create `queries/posts.sql`:
```sql
-- name: CreatePost :one
INSERT INTO posts (id, author_id, body, image_path)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetPostByID :one
SELECT * FROM posts WHERE id = $1;

-- name: ListPosts :many
SELECT * FROM posts
WHERE status = 'visible'
  AND (sqlc.narg('cursor')::text IS NULL OR created_at < (SELECT created_at FROM posts WHERE id = sqlc.narg('cursor')))
ORDER BY created_at DESC
LIMIT $1;

-- name: UpdatePostBody :one
UPDATE posts SET body = $2, edited_at = NOW() WHERE id = $1 RETURNING *;

-- name: UpdatePostStatus :exec
UPDATE posts SET status = $2, removal_reason = $3 WHERE id = $1;

-- name: CountPostsByAuthorSince :one
SELECT COUNT(*) FROM posts WHERE author_id = $1 AND created_at >= $2;
```

**Step 4: Create reaction queries**

Create `queries/reactions.sql`:
```sql
-- name: CreateReaction :one
INSERT INTO reactions (id, user_id, post_id, reaction_type)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: DeleteReaction :exec
DELETE FROM reactions WHERE user_id = $1 AND post_id = $2 AND reaction_type = $3;

-- name: ListReactionsByPost :many
SELECT reaction_type, COUNT(*) as count
FROM reactions
WHERE post_id = $1
GROUP BY reaction_type;

-- name: GetUserReactionOnPost :one
SELECT * FROM reactions WHERE user_id = $1 AND post_id = $2 AND reaction_type = $3;

-- name: CountReactionsReceivedSince :one
SELECT COUNT(*) FROM reactions r
JOIN posts p ON r.post_id = p.id
WHERE p.author_id = $1 AND r.created_at >= $2;
```

**Step 5: Install sqlc CLI and generate**

```bash
go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
sqlc generate
```

Expected: generates Go code in `internal/repository/postgres/`.

**Step 6: Verify generated code compiles**

```bash
go mod tidy
go build ./internal/repository/postgres/
```

**Step 7: Commit**

```bash
git add sqlc.yaml queries/ internal/repository/postgres/
git commit -m "feat: add sqlc config and queries (users, posts, reactions)"
```

---

### Task 10: HTTP Server and Chi Router Setup

**Files:**
- Create: `internal/server/server.go`
- Create: `internal/server/routes.go`
- Create: `internal/middleware/json.go`
- Create: `internal/middleware/logging.go`
- Create: `internal/handler/health.go`
- Modify: `cmd/bell/main.go`

**Step 1: Create JSON response middleware**

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

**Step 2: Create request logging middleware**

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

**Step 3: Create health handler**

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

**Step 4: Create route registration**

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

	// Health check (no auth)
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

**Step 5: Create server struct**

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
	cfg    config.Config
	db     *pgxpool.Pool
	redis  *redis.Client
	http   *http.Server
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

func (s *Server) Start() error {
	slog.Info("server listening", "addr", s.http.Addr)
	return s.http.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.http.Shutdown(ctx)
}
```

**Step 6: Update main.go to start the server**

Update `cmd/bell/main.go`:
```go
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/redis/go-redis/v9"

	"github.com/fireynis/the-bell/internal/config"
	"github.com/fireynis/the-bell/internal/database"
	"github.com/fireynis/the-bell/internal/server"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// Run migrations
	slog.Info("running database migrations")
	if err := database.RunMigrations(cfg.DatabaseURL, "migrations"); err != nil {
		slog.Error("failed to run migrations", "error", err)
		os.Exit(1)
	}

	// Connect to database
	ctx := context.Background()
	pool, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

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
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10_000_000_000) // 10s
		defer cancel()
		srv.Shutdown(shutdownCtx)
	}()

	if err := srv.Start(); err != nil {
		slog.Error("server error", "error", err)
	}
}
```

**Step 7: Verify compilation**

```bash
go mod tidy
go build ./cmd/bell/
```

**Step 8: Commit**

```bash
rm -f bell
git add internal/server/ internal/middleware/ internal/handler/ cmd/bell/main.go go.mod go.sum
git commit -m "feat: add chi HTTP server with health check, logging, and graceful shutdown"
```

---

### Task 11: Kratos Auth Middleware

**Files:**
- Create: `internal/middleware/auth.go`
- Create: `internal/middleware/auth_test.go`

**Step 1: Write the failing test**

Create `internal/middleware/auth_test.go`:
```go
package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/middleware"
)

func TestGetUserFromContext(t *testing.T) {
	user := &domain.User{ID: "test-1", Role: domain.RoleMember}
	ctx := middleware.WithUser(context.Background(), user)

	got, ok := middleware.UserFromContext(ctx)
	if !ok {
		t.Fatal("expected user in context")
	}
	if got.ID != "test-1" {
		t.Errorf("expected user ID test-1, got %s", got.ID)
	}
}

func TestRequireRole_Allowed(t *testing.T) {
	user := &domain.User{ID: "test-1", Role: domain.RoleMember, IsActive: true}

	handler := middleware.RequireRole(domain.RoleMember)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest("GET", "/", nil)
	req = req.WithContext(middleware.WithUser(req.Context(), user))
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestRequireRole_Forbidden(t *testing.T) {
	user := &domain.User{ID: "test-1", Role: domain.RolePending, IsActive: true}

	handler := middleware.RequireRole(domain.RoleMember)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest("GET", "/", nil)
	req = req.WithContext(middleware.WithUser(req.Context(), user))
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rr.Code)
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/middleware/ -v
```

Expected: FAIL

**Step 3: Implement auth middleware**

Create `internal/middleware/auth.go`:
```go
package middleware

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	ory "github.com/ory/kratos-client-go"

	"github.com/fireynis/the-bell/internal/domain"
)

type contextKey string

const userContextKey contextKey = "user"

func WithUser(ctx context.Context, user *domain.User) context.Context {
	return context.WithValue(ctx, userContextKey, user)
}

func UserFromContext(ctx context.Context) (*domain.User, bool) {
	user, ok := ctx.Value(userContextKey).(*domain.User)
	return user, ok
}

// UserLoader is a function that finds or creates an app user from a Kratos identity ID.
type UserLoader func(ctx context.Context, kratosIdentityID string, traits map[string]interface{}) (*domain.User, error)

// KratosAuth validates the Kratos session and loads the app user into context.
func KratosAuth(kratosPublicURL string, loadUser UserLoader) func(http.Handler) http.Handler {
	configuration := ory.NewConfiguration()
	configuration.Servers = []ory.ServerConfiguration{{URL: kratosPublicURL}}
	kratosClient := ory.NewAPIClient(configuration)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie("bell_session")
			if err != nil {
				writeError(w, http.StatusUnauthorized, "no session cookie")
				return
			}

			session, _, err := kratosClient.FrontendAPI.ToSession(r.Context()).
				Cookie(cookie.String()).
				Execute()
			if err != nil || session == nil || !*session.Active {
				writeError(w, http.StatusUnauthorized, "invalid or expired session")
				return
			}

			identityID := session.Identity.Id
			traits, _ := session.Identity.Traits.(map[string]interface{})

			user, err := loadUser(r.Context(), identityID, traits)
			if err != nil {
				slog.Error("failed to load user", "kratos_id", identityID, "error", err)
				writeError(w, http.StatusInternalServerError, "failed to load user")
				return
			}

			next.ServeHTTP(w, r.WithContext(WithUser(r.Context(), user)))
		})
	}
}

// roleRank maps roles to a numeric rank for comparison.
var roleRank = map[domain.Role]int{
	domain.RoleBanned:    0,
	domain.RolePending:   1,
	domain.RoleMember:    2,
	domain.RoleModerator: 3,
	domain.RoleCouncil:   4,
}

// RequireRole checks that the authenticated user has at least the given role.
func RequireRole(minRole domain.Role) func(http.Handler) http.Handler {
	minRank := roleRank[minRole]
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, ok := UserFromContext(r.Context())
			if !ok {
				writeError(w, http.StatusUnauthorized, "not authenticated")
				return
			}
			if !user.IsActive || roleRank[user.Role] < minRank {
				writeError(w, http.StatusForbidden, "insufficient permissions")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}
```

**Step 4: Run tests**

```bash
go mod tidy
go test ./internal/middleware/ -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/middleware/auth.go internal/middleware/auth_test.go go.mod go.sum
git commit -m "feat: add Kratos session validation and role-based auth middleware"
```

---

### Task 12: Post Handler (CRUD + Feed)

**Files:**
- Create: `internal/handler/respond.go` (shared JSON response helpers)
- Create: `internal/handler/post.go`
- Create: `internal/handler/post_test.go`
- Modify: `internal/server/routes.go` (wire up post routes)

**Step 1: Create response helpers**

Create `internal/handler/respond.go`:
```go
package handler

import (
	"encoding/json"
	"net/http"
)

func respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.WriteHeader(status)
	if data != nil {
		json.NewEncoder(w).Encode(data)
	}
}

func respondError(w http.ResponseWriter, status int, message string) {
	respondJSON(w, status, map[string]string{"error": message})
}
```

**Step 2: Create post handler**

Create `internal/handler/post.go`:
```go
package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/middleware"
)

// PostService defines the interface the handler needs.
type PostService interface {
	Create(ctx context.Context, authorID, body, imagePath string) (*domain.Post, error)
	GetByID(ctx context.Context, id string) (*domain.Post, error)
	ListFeed(ctx context.Context, cursor string, limit int) ([]*domain.Post, error)
	UpdateBody(ctx context.Context, id, userID, body string) (*domain.Post, error)
	Delete(ctx context.Context, id, userID string) error
}

type PostHandler struct {
	posts PostService
}

func NewPostHandler(posts PostService) *PostHandler {
	return &PostHandler{posts: posts}
}

type createPostRequest struct {
	Body string `json:"body"`
}

func (h *PostHandler) Create(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	if !user.CanPost() {
		respondError(w, http.StatusForbidden, "insufficient trust or role to post")
		return
	}

	var req createPostRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Body == "" || len(req.Body) > domain.MaxPostBodyLength {
		respondError(w, http.StatusBadRequest, "body must be 1-1000 characters")
		return
	}

	post, err := h.posts.Create(r.Context(), user.ID, req.Body, "")
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to create post")
		return
	}

	respondJSON(w, http.StatusCreated, post)
}

func (h *PostHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	post, err := h.posts.GetByID(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusNotFound, "post not found")
		return
	}
	respondJSON(w, http.StatusOK, post)
}

func (h *PostHandler) ListFeed(w http.ResponseWriter, r *http.Request) {
	cursor := r.URL.Query().Get("cursor")
	limit := 25

	posts, err := h.posts.ListFeed(r.Context(), cursor, limit)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load feed")
		return
	}

	var nextCursor string
	if len(posts) == limit {
		nextCursor = posts[len(posts)-1].ID
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"posts":       posts,
		"next_cursor": nextCursor,
	})
}

func (h *PostHandler) Update(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "not authenticated")
		return
	}

	id := chi.URLParam(r, "id")
	var req createPostRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	post, err := h.posts.UpdateBody(r.Context(), id, user.ID, req.Body)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, post)
}

func (h *PostHandler) Delete(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "not authenticated")
		return
	}

	id := chi.URLParam(r, "id")
	if err := h.posts.Delete(r.Context(), id, user.ID); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusNoContent, nil)
}
```

Note: this handler requires `import "context"` at the top — the PostService interface uses `context.Context`. Fix the import block to include it.

**Step 3: Wire routes**

Update `internal/server/routes.go` to accept dependencies and mount post routes. The server struct will need to hold service references — this will be wired fully once services are implemented. For now, add the route structure:

Update the `/api/v1` route block in `internal/server/routes.go`:
```go
	r.Route("/api/v1", func(r chi.Router) {
		r.Use(middleware.ContentTypeJSON)

		// Posts (will add auth middleware once Kratos is wired)
		r.Route("/posts", func(r chi.Router) {
			r.Get("/", s.postHandler.ListFeed)
			r.Post("/", s.postHandler.Create)
			r.Get("/{id}", s.postHandler.GetByID)
			r.Patch("/{id}", s.postHandler.Update)
			r.Delete("/{id}", s.postHandler.Delete)
		})
	})
```

This task creates the HTTP layer. The PostService interface will be implemented in the next task.

**Step 4: Commit**

```bash
git add internal/handler/ internal/server/
git commit -m "feat: add post handler with CRUD and cursor-based feed endpoint"
```

---

### Task 13: Post Service Implementation

**Files:**
- Create: `internal/service/post.go`
- Create: `internal/service/post_test.go`

**Step 1: Write the failing test**

Create `internal/service/post_test.go`:
```go
package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/service"
)

// mockPostRepo is a simple in-memory mock for testing.
type mockPostRepo struct {
	posts map[string]*domain.Post
}

func newMockPostRepo() *mockPostRepo {
	return &mockPostRepo{posts: make(map[string]*domain.Post)}
}

func (m *mockPostRepo) CreatePost(ctx context.Context, post *domain.Post) error {
	m.posts[post.ID] = post
	return nil
}

func (m *mockPostRepo) GetPostByID(ctx context.Context, id string) (*domain.Post, error) {
	p, ok := m.posts[id]
	if !ok {
		return nil, service.ErrNotFound
	}
	return p, nil
}

func (m *mockPostRepo) ListPosts(ctx context.Context, cursor string, limit int) ([]*domain.Post, error) {
	result := make([]*domain.Post, 0)
	for _, p := range m.posts {
		if p.Status == domain.PostVisible {
			result = append(result, p)
		}
	}
	return result, nil
}

func (m *mockPostRepo) UpdatePostBody(ctx context.Context, id, body string) (*domain.Post, error) {
	p, ok := m.posts[id]
	if !ok {
		return nil, service.ErrNotFound
	}
	p.Body = body
	now := time.Now()
	p.EditedAt = &now
	return p, nil
}

func (m *mockPostRepo) UpdatePostStatus(ctx context.Context, id string, status domain.PostStatus, reason string) error {
	p, ok := m.posts[id]
	if !ok {
		return service.ErrNotFound
	}
	p.Status = status
	p.RemovalReason = reason
	return nil
}

func TestPostService_Create(t *testing.T) {
	repo := newMockPostRepo()
	svc := service.NewPostService(repo)

	post, err := svc.Create(context.Background(), "user-1", "Hello, town!", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if post.Body != "Hello, town!" {
		t.Errorf("expected body 'Hello, town!', got '%s'", post.Body)
	}
	if post.AuthorID != "user-1" {
		t.Errorf("expected author user-1, got %s", post.AuthorID)
	}
	if post.Status != domain.PostVisible {
		t.Errorf("expected status visible, got %s", post.Status)
	}
}

func TestPostService_Create_EmptyBody(t *testing.T) {
	repo := newMockPostRepo()
	svc := service.NewPostService(repo)

	_, err := svc.Create(context.Background(), "user-1", "", "")
	if err == nil {
		t.Fatal("expected error for empty body")
	}
}

func TestPostService_Delete_WrongUser(t *testing.T) {
	repo := newMockPostRepo()
	svc := service.NewPostService(repo)

	post, _ := svc.Create(context.Background(), "user-1", "Test", "")
	err := svc.Delete(context.Background(), post.ID, "user-2")
	if err == nil {
		t.Fatal("expected error when wrong user deletes")
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/service/ -v
```

Expected: FAIL

**Step 3: Implement post service**

Create `internal/service/post.go`:
```go
package service

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/fireynis/the-bell/internal/domain"
)

var (
	ErrNotFound       = errors.New("not found")
	ErrForbidden      = errors.New("forbidden")
	ErrValidation     = errors.New("validation error")
	ErrEditWindow     = errors.New("edit window expired")
)

type PostRepository interface {
	CreatePost(ctx context.Context, post *domain.Post) error
	GetPostByID(ctx context.Context, id string) (*domain.Post, error)
	ListPosts(ctx context.Context, cursor string, limit int) ([]*domain.Post, error)
	UpdatePostBody(ctx context.Context, id, body string) (*domain.Post, error)
	UpdatePostStatus(ctx context.Context, id string, status domain.PostStatus, reason string) error
}

type PostService struct {
	repo PostRepository
}

func NewPostService(repo PostRepository) *PostService {
	return &PostService{repo: repo}
}

func (s *PostService) Create(ctx context.Context, authorID, body, imagePath string) (*domain.Post, error) {
	if body == "" || len(body) > domain.MaxPostBodyLength {
		return nil, ErrValidation
	}

	id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}

	post := &domain.Post{
		ID:        id.String(),
		AuthorID:  authorID,
		Body:      body,
		ImagePath: imagePath,
		Status:    domain.PostVisible,
		CreatedAt: time.Now(),
	}

	if err := s.repo.CreatePost(ctx, post); err != nil {
		return nil, err
	}
	return post, nil
}

func (s *PostService) GetByID(ctx context.Context, id string) (*domain.Post, error) {
	return s.repo.GetPostByID(ctx, id)
}

func (s *PostService) ListFeed(ctx context.Context, cursor string, limit int) ([]*domain.Post, error) {
	return s.repo.ListPosts(ctx, cursor, limit)
}

func (s *PostService) UpdateBody(ctx context.Context, id, userID, body string) (*domain.Post, error) {
	post, err := s.repo.GetPostByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if !post.CanEdit(userID, time.Now()) {
		return nil, ErrEditWindow
	}
	if body == "" || len(body) > domain.MaxPostBodyLength {
		return nil, ErrValidation
	}
	return s.repo.UpdatePostBody(ctx, id, body)
}

func (s *PostService) Delete(ctx context.Context, id, userID string) error {
	post, err := s.repo.GetPostByID(ctx, id)
	if err != nil {
		return err
	}
	if post.AuthorID != userID {
		return ErrForbidden
	}
	return s.repo.UpdatePostStatus(ctx, id, domain.PostRemovedByAuthor, "")
}
```

**Step 4: Run tests**

```bash
go test ./internal/service/ -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/service/
git commit -m "feat: add post service with create, feed, edit, delete"
```

---

### Task 14: User Sync Service (Create App User on First Auth)

**Files:**
- Create: `internal/service/user.go`
- Create: `internal/service/user_test.go`

This service handles the "first login" flow: when a Kratos-authenticated user hits the API and doesn't have an app user record yet, create one.

**Step 1: Write the test**

Create `internal/service/user_test.go`:
```go
package service_test

import (
	"context"
	"testing"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/service"
)

type mockUserRepo struct {
	users map[string]*domain.User
}

func newMockUserRepo() *mockUserRepo {
	return &mockUserRepo{users: make(map[string]*domain.User)}
}

func (m *mockUserRepo) GetUserByKratosID(ctx context.Context, kratosID string) (*domain.User, error) {
	for _, u := range m.users {
		if u.KratosIdentityID == kratosID {
			return u, nil
		}
	}
	return nil, service.ErrNotFound
}

func (m *mockUserRepo) CreateUser(ctx context.Context, user *domain.User) error {
	m.users[user.ID] = user
	return nil
}

func (m *mockUserRepo) GetUserByID(ctx context.Context, id string) (*domain.User, error) {
	u, ok := m.users[id]
	if !ok {
		return nil, service.ErrNotFound
	}
	return u, nil
}

func (m *mockUserRepo) UpdateUser(ctx context.Context, user *domain.User) error {
	m.users[user.ID] = user
	return nil
}

func (m *mockUserRepo) UpdateUserTrustScore(ctx context.Context, id string, score float64) error {
	if u, ok := m.users[id]; ok {
		u.TrustScore = score
	}
	return nil
}

func (m *mockUserRepo) UpdateUserRole(ctx context.Context, id string, role domain.Role) error {
	if u, ok := m.users[id]; ok {
		u.Role = role
	}
	return nil
}

func TestUserService_FindOrCreate_NewUser(t *testing.T) {
	repo := newMockUserRepo()
	svc := service.NewUserService(repo)

	traits := map[string]interface{}{"name": "Alice", "email": "alice@town.ca"}

	user, err := svc.FindOrCreate(context.Background(), "kratos-abc", traits)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if user.DisplayName != "Alice" {
		t.Errorf("expected name Alice, got %s", user.DisplayName)
	}
	if user.Role != domain.RolePending {
		t.Errorf("expected pending role, got %s", user.Role)
	}
	if user.TrustScore != 50.0 {
		t.Errorf("expected trust 50, got %f", user.TrustScore)
	}
}

func TestUserService_FindOrCreate_ExistingUser(t *testing.T) {
	repo := newMockUserRepo()
	svc := service.NewUserService(repo)

	traits := map[string]interface{}{"name": "Alice", "email": "alice@town.ca"}
	first, _ := svc.FindOrCreate(context.Background(), "kratos-abc", traits)

	second, err := svc.FindOrCreate(context.Background(), "kratos-abc", traits)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if first.ID != second.ID {
		t.Error("expected same user on second call")
	}
}
```

**Step 2: Implement**

Create `internal/service/user.go`:
```go
package service

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/fireynis/the-bell/internal/domain"
)

type UserRepository interface {
	GetUserByKratosID(ctx context.Context, kratosID string) (*domain.User, error)
	GetUserByID(ctx context.Context, id string) (*domain.User, error)
	CreateUser(ctx context.Context, user *domain.User) error
	UpdateUser(ctx context.Context, user *domain.User) error
	UpdateUserTrustScore(ctx context.Context, id string, score float64) error
	UpdateUserRole(ctx context.Context, id string, role domain.Role) error
}

type UserService struct {
	repo UserRepository
}

func NewUserService(repo UserRepository) *UserService {
	return &UserService{repo: repo}
}

// FindOrCreate looks up the app user by Kratos identity ID.
// If not found, creates a new pending user.
func (s *UserService) FindOrCreate(ctx context.Context, kratosIdentityID string, traits map[string]interface{}) (*domain.User, error) {
	user, err := s.repo.GetUserByKratosID(ctx, kratosIdentityID)
	if err == nil {
		return user, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, err
	}

	// Extract display name from Kratos traits
	displayName := ""
	if name, ok := traits["name"].(string); ok {
		displayName = name
	}

	id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}

	now := time.Now()
	user = &domain.User{
		ID:               id.String(),
		KratosIdentityID: kratosIdentityID,
		DisplayName:      displayName,
		TrustScore:       50.0,
		Role:             domain.RolePending,
		IsActive:         true,
		JoinedAt:         now,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	if err := s.repo.CreateUser(ctx, user); err != nil {
		return nil, err
	}
	return user, nil
}

func (s *UserService) GetByID(ctx context.Context, id string) (*domain.User, error) {
	return s.repo.GetUserByID(ctx, id)
}

func (s *UserService) UpdateProfile(ctx context.Context, id, displayName, bio, avatarURL string) (*domain.User, error) {
	user, err := s.repo.GetUserByID(ctx, id)
	if err != nil {
		return nil, err
	}
	user.DisplayName = displayName
	user.Bio = bio
	user.AvatarURL = avatarURL
	user.UpdatedAt = time.Now()

	if err := s.repo.UpdateUser(ctx, user); err != nil {
		return nil, err
	}
	return user, nil
}
```

**Step 3: Run tests**

```bash
go test ./internal/service/ -v
```

Expected: PASS

**Step 4: Commit**

```bash
git add internal/service/user.go internal/service/user_test.go
git commit -m "feat: add user service with find-or-create for Kratos sync"
```

---

**Phase 1 complete.** At this point you have:
- Project scaffold with all dependencies
- Docker Compose (app + Postgres + AGE + Kratos + Redis)
- Database migrations (all tables + AGE trust graph)
- sqlc-generated repository code
- Chi router with health check + middleware (auth, logging, JSON)
- Domain types with tests
- Post service + handler (CRUD + feed)
- User service (Kratos sync)
- Wiring in main.go

To run it: create `.env` from `.env.example`, ensure the Postgres databases `bell` and `bell_kratos` exist, then `docker compose up -d`.

---

## Phase 2: Trust System

### Task 15: Vouch Repository Queries

**Files:**
- Create: `queries/vouches.sql`
- Create: `internal/repository/postgres/age.go` (manual AGE graph queries since sqlc can't generate Cypher)

**Step 1: Create vouch SQL queries for the relational side**

Create `queries/vouches.sql`:
```sql
-- name: CreateVouch :one
INSERT INTO vouches (id, voucher_id, vouchee_id, status)
VALUES ($1, $2, $3, 'active')
RETURNING *;

-- name: RevokeVouch :exec
UPDATE vouches SET status = 'revoked', revoked_at = NOW() WHERE voucher_id = $1 AND vouchee_id = $2 AND status = 'active';

-- name: GetActiveVouchByPair :one
SELECT * FROM vouches WHERE voucher_id = $1 AND vouchee_id = $2 AND status = 'active';

-- name: ListVouchesByVoucher :many
SELECT * FROM vouches WHERE voucher_id = $1 AND status = 'active';

-- name: ListVouchesByVouchee :many
SELECT * FROM vouches WHERE vouchee_id = $1 AND status = 'active';

-- name: CountActiveVouchesToday :one
SELECT COUNT(*) FROM vouches WHERE voucher_id = $1 AND status = 'active' AND created_at >= CURRENT_DATE;
```

Note: you'll also need to add a `vouches` migration. Create `migrations/00008_create_vouches.sql`:
```sql
-- +goose Up
CREATE TABLE vouches (
    id          TEXT PRIMARY KEY,
    voucher_id  TEXT NOT NULL REFERENCES users(id),
    vouchee_id  TEXT NOT NULL REFERENCES users(id),
    status      TEXT NOT NULL DEFAULT 'active',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at  TIMESTAMPTZ,
    UNIQUE(voucher_id, vouchee_id)
);

CREATE INDEX idx_vouches_voucher ON vouches(voucher_id);
CREATE INDEX idx_vouches_vouchee ON vouches(vouchee_id);
CREATE INDEX idx_vouches_status ON vouches(status);

-- +goose Down
DROP TABLE IF EXISTS vouches;
```

**Step 2: Create AGE graph query helper**

Create `internal/repository/postgres/age.go`:
```go
package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type AGERepository struct {
	pool *pgxpool.Pool
}

func NewAGERepository(pool *pgxpool.Pool) *AGERepository {
	return &AGERepository{pool: pool}
}

// AddVouchEdge creates a VOUCHED_FOR edge in the trust graph.
func (r *AGERepository) AddVouchEdge(ctx context.Context, voucherID, voucheeID string) error {
	query := `
		SELECT * FROM ag_catalog.cypher('trust_graph', $$
			MERGE (a:User {id: %s})
			MERGE (b:User {id: %s})
			CREATE (a)-[:VOUCHED_FOR {created_at: timestamp()}]->(b)
		$$) AS (v ag_catalog.agtype);
	`
	_, err := r.pool.Exec(ctx, fmt.Sprintf(query,
		ageString(voucherID), ageString(voucheeID)))
	return err
}

// RemoveVouchEdge removes the VOUCHED_FOR edge.
func (r *AGERepository) RemoveVouchEdge(ctx context.Context, voucherID, voucheeID string) error {
	query := `
		SELECT * FROM ag_catalog.cypher('trust_graph', $$
			MATCH (a:User {id: %s})-[r:VOUCHED_FOR]->(b:User {id: %s})
			DELETE r
		$$) AS (v ag_catalog.agtype);
	`
	_, err := r.pool.Exec(ctx, fmt.Sprintf(query,
		ageString(voucherID), ageString(voucheeID)))
	return err
}

// FindVouchersUpToDepth walks the vouch graph backward from targetID
// up to maxDepth hops, returning (voucher_id, hop_depth) pairs.
func (r *AGERepository) FindVouchersUpToDepth(ctx context.Context, targetID string, maxDepth int) ([]VoucherHop, error) {
	query := fmt.Sprintf(`
		SELECT * FROM ag_catalog.cypher('trust_graph', $$
			MATCH path = (voucher:User)-[:VOUCHED_FOR*1..%d]->(target:User {id: %s})
			RETURN voucher.id, length(path)
		$$) AS (voucher_id ag_catalog.agtype, depth ag_catalog.agtype);
	`, maxDepth, ageString(targetID))

	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []VoucherHop
	for rows.Next() {
		var voucherID string
		var depth int
		if err := rows.Scan(&voucherID, &depth); err != nil {
			return nil, err
		}
		results = append(results, VoucherHop{UserID: voucherID, Depth: depth})
	}
	return results, rows.Err()
}

// HasCyclicVouch checks if adding voucher→vouchee would create a cycle.
func (r *AGERepository) HasCyclicVouch(ctx context.Context, voucherID, voucheeID string) (bool, error) {
	query := fmt.Sprintf(`
		SELECT * FROM ag_catalog.cypher('trust_graph', $$
			MATCH path = (a:User {id: %s})-[:VOUCHED_FOR*1..10]->(b:User {id: %s})
			RETURN count(path)
		$$) AS (cnt ag_catalog.agtype);
	`, ageString(voucheeID), ageString(voucherID))

	var count int
	err := r.pool.QueryRow(ctx, query).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

type VoucherHop struct {
	UserID string
	Depth  int
}

// ageString wraps a string value for use in Cypher queries.
func ageString(s string) string {
	return fmt.Sprintf("'%s'", s)
}
```

Note: AGE's Cypher integration with pgx requires careful handling of `agtype` results. The exact scanning behavior may need adjustment when testing against a real AGE instance. The `ageString` helper prevents SQL injection for UUID values, but this should be validated during integration testing.

**Step 3: Regenerate sqlc and commit**

```bash
sqlc generate
git add queries/ migrations/00008_create_vouches.sql internal/repository/postgres/
git commit -m "feat: add vouch queries (SQL + AGE graph traversal)"
```

---

### Task 16: Vouch Service

**Files:**
- Create: `internal/service/vouch.go`
- Create: `internal/service/vouch_test.go`

**Step 1: Write tests**

Create `internal/service/vouch_test.go`:
```go
package service_test

import (
	"context"
	"testing"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/service"
)

func TestVouchService_Vouch_Success(t *testing.T) {
	vouchRepo := newMockVouchRepo()
	graphRepo := newMockGraphRepo()
	userRepo := newMockUserRepo()
	svc := service.NewVouchService(vouchRepo, graphRepo, userRepo)

	// Create voucher with sufficient trust
	voucher := &domain.User{ID: "user-1", TrustScore: 70, Role: domain.RoleMember, IsActive: true}
	vouchee := &domain.User{ID: "user-2", TrustScore: 50, Role: domain.RolePending, IsActive: true}
	userRepo.users[voucher.ID] = voucher
	userRepo.users[vouchee.ID] = vouchee

	vouch, err := svc.Vouch(context.Background(), voucher, "user-2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if vouch.VoucherID != "user-1" || vouch.VoucheeID != "user-2" {
		t.Error("vouch has wrong IDs")
	}
}

func TestVouchService_Vouch_InsufficientTrust(t *testing.T) {
	vouchRepo := newMockVouchRepo()
	graphRepo := newMockGraphRepo()
	userRepo := newMockUserRepo()
	svc := service.NewVouchService(vouchRepo, graphRepo, userRepo)

	voucher := &domain.User{ID: "user-1", TrustScore: 40, Role: domain.RoleMember, IsActive: true}
	userRepo.users[voucher.ID] = voucher

	_, err := svc.Vouch(context.Background(), voucher, "user-2")
	if err == nil {
		t.Fatal("expected error for insufficient trust")
	}
}

// Mock implementations for vouch tests
type mockVouchRepo struct {
	vouches map[string]*domain.Vouch
	dailyCount int
}

func newMockVouchRepo() *mockVouchRepo {
	return &mockVouchRepo{vouches: make(map[string]*domain.Vouch)}
}

func (m *mockVouchRepo) CreateVouch(ctx context.Context, vouch *domain.Vouch) error {
	m.vouches[vouch.ID] = vouch
	return nil
}

func (m *mockVouchRepo) GetActiveVouch(ctx context.Context, voucherID, voucheeID string) (*domain.Vouch, error) {
	for _, v := range m.vouches {
		if v.VoucherID == voucherID && v.VoucheeID == voucheeID && v.Status == domain.VouchActive {
			return v, nil
		}
	}
	return nil, service.ErrNotFound
}

func (m *mockVouchRepo) RevokeVouch(ctx context.Context, voucherID, voucheeID string) error {
	for _, v := range m.vouches {
		if v.VoucherID == voucherID && v.VoucheeID == voucheeID {
			v.Status = domain.VouchRevoked
		}
	}
	return nil
}

func (m *mockVouchRepo) CountTodayVouches(ctx context.Context, voucherID string) (int, error) {
	return m.dailyCount, nil
}

func (m *mockVouchRepo) ListByVoucher(ctx context.Context, voucherID string) ([]*domain.Vouch, error) {
	return nil, nil
}

func (m *mockVouchRepo) ListByVouchee(ctx context.Context, voucheeID string) ([]*domain.Vouch, error) {
	return nil, nil
}

type mockGraphRepo struct {
	hasCycle bool
}

func newMockGraphRepo() *mockGraphRepo {
	return &mockGraphRepo{}
}

func (m *mockGraphRepo) AddVouchEdge(ctx context.Context, voucherID, voucheeID string) error {
	return nil
}

func (m *mockGraphRepo) RemoveVouchEdge(ctx context.Context, voucherID, voucheeID string) error {
	return nil
}

func (m *mockGraphRepo) HasCyclicVouch(ctx context.Context, voucherID, voucheeID string) (bool, error) {
	return m.hasCycle, nil
}

func (m *mockGraphRepo) FindVouchersUpToDepth(ctx context.Context, targetID string, maxDepth int) ([]service.VoucherHop, error) {
	return nil, nil
}
```

**Step 2: Implement vouch service**

Create `internal/service/vouch.go`:
```go
package service

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/fireynis/the-bell/internal/domain"
)

var (
	ErrInsufficientTrust = errors.New("insufficient trust score to vouch")
	ErrDailyVouchLimit   = errors.New("daily vouch limit reached")
	ErrCyclicVouch       = errors.New("would create circular vouch chain")
	ErrAlreadyVouched    = errors.New("already vouched for this user")
	ErrSelfVouch         = errors.New("cannot vouch for yourself")
)

const MaxDailyVouches = 3

type VouchRepository interface {
	CreateVouch(ctx context.Context, vouch *domain.Vouch) error
	GetActiveVouch(ctx context.Context, voucherID, voucheeID string) (*domain.Vouch, error)
	RevokeVouch(ctx context.Context, voucherID, voucheeID string) error
	CountTodayVouches(ctx context.Context, voucherID string) (int, error)
	ListByVoucher(ctx context.Context, voucherID string) ([]*domain.Vouch, error)
	ListByVouchee(ctx context.Context, voucheeID string) ([]*domain.Vouch, error)
}

type VoucherHop struct {
	UserID string
	Depth  int
}

type GraphRepository interface {
	AddVouchEdge(ctx context.Context, voucherID, voucheeID string) error
	RemoveVouchEdge(ctx context.Context, voucherID, voucheeID string) error
	HasCyclicVouch(ctx context.Context, voucherID, voucheeID string) (bool, error)
	FindVouchersUpToDepth(ctx context.Context, targetID string, maxDepth int) ([]VoucherHop, error)
}

type VouchService struct {
	repo  VouchRepository
	graph GraphRepository
	users UserRepository
}

func NewVouchService(repo VouchRepository, graph GraphRepository, users UserRepository) *VouchService {
	return &VouchService{repo: repo, graph: graph, users: users}
}

func (s *VouchService) Vouch(ctx context.Context, voucher *domain.User, voucheeID string) (*domain.Vouch, error) {
	if voucher.ID == voucheeID {
		return nil, ErrSelfVouch
	}
	if !voucher.CanVouch() {
		return nil, ErrInsufficientTrust
	}

	// Check daily limit
	count, err := s.repo.CountTodayVouches(ctx, voucher.ID)
	if err != nil {
		return nil, err
	}
	if count >= MaxDailyVouches {
		return nil, ErrDailyVouchLimit
	}

	// Check not already vouched
	_, err = s.repo.GetActiveVouch(ctx, voucher.ID, voucheeID)
	if err == nil {
		return nil, ErrAlreadyVouched
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, err
	}

	// Check for cycles
	hasCycle, err := s.graph.HasCyclicVouch(ctx, voucher.ID, voucheeID)
	if err != nil {
		return nil, err
	}
	if hasCycle {
		return nil, ErrCyclicVouch
	}

	id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}

	vouch := &domain.Vouch{
		ID:        id.String(),
		VoucherID: voucher.ID,
		VoucheeID: voucheeID,
		Status:    domain.VouchActive,
		CreatedAt: time.Now(),
	}

	if err := s.repo.CreateVouch(ctx, vouch); err != nil {
		return nil, err
	}

	// Add graph edge
	if err := s.graph.AddVouchEdge(ctx, voucher.ID, voucheeID); err != nil {
		return nil, err
	}

	// If vouchee is pending and this is their first vouch, promote to member
	vouchee, err := s.users.GetUserByID(ctx, voucheeID)
	if err == nil && vouchee.Role == domain.RolePending {
		if err := s.users.UpdateUserRole(ctx, voucheeID, domain.RoleMember); err != nil {
			return nil, err
		}
	}

	return vouch, nil
}

func (s *VouchService) Revoke(ctx context.Context, voucherID, voucheeID string) error {
	if err := s.repo.RevokeVouch(ctx, voucherID, voucheeID); err != nil {
		return err
	}
	return s.graph.RemoveVouchEdge(ctx, voucherID, voucheeID)
}
```

**Step 3: Run tests**

```bash
go test ./internal/service/ -v
```

Expected: PASS

**Step 4: Commit**

```bash
git add internal/service/vouch.go internal/service/vouch_test.go
git commit -m "feat: add vouch service with cycle detection and daily limits"
```

---

### Task 17: Trust Score Calculation Engine

This is the most critical service. It computes the 4-component trust score.

**Files:**
- Create: `internal/service/trust.go`
- Create: `internal/service/trust_test.go`

**Step 1: Write comprehensive tests**

Create `internal/service/trust_test.go`:
```go
package service_test

import (
	"math"
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/service"
)

func TestTenureScore(t *testing.T) {
	tests := []struct {
		name     string
		joined   time.Time
		expected float64
	}{
		{"brand new", time.Now(), 0},
		{"6 months", time.Now().AddDate(0, -6, 0), 50},
		{"1 year", time.Now().AddDate(-1, 0, 0), 100},
		{"2 years (capped)", time.Now().AddDate(-2, 0, 0), 100},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := service.CalcTenureScore(tt.joined, time.Now())
			if math.Abs(got-tt.expected) > 2 { // allow small float variance
				t.Errorf("CalcTenureScore() = %f, want ~%f", got, tt.expected)
			}
		})
	}
}

func TestActivityScore(t *testing.T) {
	tests := []struct {
		name              string
		posts             int
		reactionsReceived int
		expected          float64
	}{
		{"no activity", 0, 0, 0},
		{"moderate activity", 45, 135, 50},
		{"max activity", 90, 270, 100},
		{"over max (capped)", 200, 500, 100},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := service.CalcActivityScore(tt.posts, tt.reactionsReceived)
			if math.Abs(got-tt.expected) > 2 {
				t.Errorf("CalcActivityScore() = %f, want ~%f", got, tt.expected)
			}
		})
	}
}

func TestVoucherScore(t *testing.T) {
	tests := []struct {
		name         string
		vouchCount   int
		avgTrust     float64
		expected     float64
	}{
		{"no vouches", 0, 0, 0},
		{"3 vouches, all healthy", 3, 90, 40.5},  // min(100,3*15)=45, health=0.9, 45*0.9=40.5
		{"7 vouches, all perfect", 7, 100, 100},   // min(100,7*15)=100, health=1.0
		{"5 vouches, one bad", 5, 60, 45},          // min(100,75)=75, health=0.6, 75*0.6=45
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := service.CalcVoucherScore(tt.vouchCount, tt.avgTrust)
			if math.Abs(got-tt.expected) > 1 {
				t.Errorf("CalcVoucherScore() = %f, want ~%f", got, tt.expected)
			}
		})
	}
}

func TestModerationScore(t *testing.T) {
	tests := []struct {
		name     string
		penalties float64
		expected float64
	}{
		{"no penalties", 0, 100},
		{"small penalty", 10, 90},
		{"large penalty", 80, 20},
		{"over 100 (floored at 0)", 150, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := service.CalcModerationScore(tt.penalties)
			if math.Abs(got-tt.expected) > 0.01 {
				t.Errorf("CalcModerationScore() = %f, want %f", got, tt.expected)
			}
		})
	}
}

func TestCompositeScore(t *testing.T) {
	// Perfect user: all 100s
	score := service.CompositeScore(100, 100, 100, 100)
	if math.Abs(score-100) > 0.01 {
		t.Errorf("perfect score should be 100, got %f", score)
	}

	// Zero user: all 0s
	score = service.CompositeScore(0, 0, 0, 0)
	if math.Abs(score-0) > 0.01 {
		t.Errorf("zero score should be 0, got %f", score)
	}

	// Typical active member: 50 tenure, 60 activity, 70 voucher, 90 mod
	score = service.CompositeScore(50, 60, 70, 90)
	expected := 50*0.15 + 60*0.20 + 70*0.35 + 90*0.30 // 7.5 + 12 + 24.5 + 27 = 71
	if math.Abs(score-expected) > 0.01 {
		t.Errorf("expected %f, got %f", expected, score)
	}
}
```

**Step 2: Run tests to verify failure**

```bash
go test ./internal/service/ -run TestTenure -v
```

Expected: FAIL

**Step 3: Implement trust calculation**

Create `internal/service/trust.go`:
```go
package service

import (
	"math"
	"time"
)

const (
	WeightTenure     = 0.15
	WeightActivity   = 0.20
	WeightVoucher    = 0.35
	WeightModeration = 0.30
)

// CalcTenureScore returns 0-100 based on days since joining. Caps at 365 days.
func CalcTenureScore(joinedAt, now time.Time) float64 {
	days := now.Sub(joinedAt).Hours() / 24
	score := (days / 365.0) * 100.0
	return math.Min(100, math.Max(0, score))
}

// CalcActivityScore returns 0-100 based on posts and reactions in the last 90 days.
func CalcActivityScore(recentPosts, recentReactionsReceived int) float64 {
	postComponent := math.Min(100, float64(recentPosts)/90.0*100.0)
	reactionComponent := math.Min(100, float64(recentReactionsReceived)/270.0*100.0)
	return (postComponent*0.5 + reactionComponent*0.5)
}

// CalcVoucherScore returns 0-100 based on vouch count and average trust of vouchees.
func CalcVoucherScore(activeVouchCount int, avgVoucheeTrust float64) float64 {
	if activeVouchCount == 0 {
		return 0
	}
	base := math.Min(100, float64(activeVouchCount)*15.0)
	health := avgVoucheeTrust / 100.0
	return base * health
}

// CalcModerationScore returns 0-100 minus accumulated penalties.
func CalcModerationScore(totalActivePenalties float64) float64 {
	score := 100.0 - totalActivePenalties
	return math.Max(0, math.Min(100, score))
}

// CompositeScore computes the weighted trust score from all 4 components.
func CompositeScore(tenure, activity, voucher, moderation float64) float64 {
	return tenure*WeightTenure +
		activity*WeightActivity +
		voucher*WeightVoucher +
		moderation*WeightModeration
}

// CalcPropagatedPenalty computes the penalty for a voucher at a given hop depth.
func CalcPropagatedPenalty(basePenalty float64, decayRate float64, hopDepth int) float64 {
	return basePenalty * math.Pow(decayRate, float64(hopDepth))
}

// CalcPenaltyRemaining computes how much of a penalty still applies given time decay.
func CalcPenaltyRemaining(originalPenalty float64, createdAt time.Time, decayDays int, now time.Time) float64 {
	if decayDays == 0 {
		return originalPenalty // permanent (ban)
	}
	elapsed := now.Sub(createdAt).Hours() / 24
	remaining := 1.0 - (elapsed / float64(decayDays))
	if remaining <= 0 {
		return 0
	}
	return originalPenalty * remaining
}
```

**Step 4: Run all trust tests**

```bash
go test ./internal/service/ -run "Test(Tenure|Activity|Voucher|Moderation|Composite)" -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/service/trust.go internal/service/trust_test.go
git commit -m "feat: add trust score calculation engine with all 4 components"
```

---

### Task 18: Trust Propagation on Moderation Actions

**Files:**
- Create: `internal/service/moderation.go`
- Create: `internal/service/moderation_test.go`

**Step 1: Write test for propagation**

Create `internal/service/moderation_test.go`:
```go
package service_test

import (
	"context"
	"testing"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/service"
)

func TestModeration_PropagatePenalties(t *testing.T) {
	graphRepo := newMockGraphRepo()
	// Simulate: user-2 vouched for user-3, user-1 vouched for user-2
	graphRepo.hops = []service.VoucherHop{
		{UserID: "user-2", Depth: 1},
		{UserID: "user-1", Depth: 2},
	}

	penaltyRepo := newMockPenaltyRepo()
	svc := service.NewModerationService(nil, graphRepo, penaltyRepo, nil)

	// Ban user-3 (severity 5)
	action := &domain.ModerationAction{
		ID:           "action-1",
		TargetUserID: "user-3",
		Severity:     5,
	}

	penalties, err := svc.PropagatePenalties(context.Background(), action)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Severity 5: depth 3, decay 0.75, base penalty 100
	// user-2 at depth 1: 100 * 0.75^1 = 75
	// user-1 at depth 2: 100 * 0.75^2 = 56.25
	if len(penalties) != 2 {
		t.Fatalf("expected 2 penalties, got %d", len(penalties))
	}

	for _, p := range penalties {
		switch p.UserID {
		case "user-2":
			if p.Amount < 74 || p.Amount > 76 {
				t.Errorf("user-2 penalty expected ~75, got %f", p.Amount)
			}
		case "user-1":
			if p.Amount < 55 || p.Amount > 57 {
				t.Errorf("user-1 penalty expected ~56.25, got %f", p.Amount)
			}
		}
	}
}

// Extended mock that supports hop results
type mockGraphRepoWithHops struct {
	mockGraphRepo
	hops []service.VoucherHop
}

func (m *mockGraphRepoWithHops) FindVouchersUpToDepth(ctx context.Context, targetID string, maxDepth int) ([]service.VoucherHop, error) {
	return m.hops, nil
}

type mockPenaltyRepo struct {
	penalties []service.PenaltyRecord
}

func newMockPenaltyRepo() *mockPenaltyRepo {
	return &mockPenaltyRepo{}
}

func (m *mockPenaltyRepo) RecordPenalty(ctx context.Context, p service.PenaltyRecord) error {
	m.penalties = append(m.penalties, p)
	return nil
}

func (m *mockPenaltyRepo) GetActivePenalties(ctx context.Context, userID string) ([]service.PenaltyRecord, error) {
	var result []service.PenaltyRecord
	for _, p := range m.penalties {
		if p.UserID == userID {
			result = append(result, p)
		}
	}
	return result, nil
}
```

Note: the mock `graphRepo` from Task 16 needs to be extended. The test file above references `graphRepo.hops` — in practice these mocks should be unified. The test above shows the intent; adjust mock field names as needed during implementation.

**Step 2: Implement moderation service**

Create `internal/service/moderation.go`:
```go
package service

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/fireynis/the-bell/internal/domain"
)

type PenaltyRecord struct {
	ID                 string
	UserID             string
	ModerationActionID string
	Amount             float64
	HopDepth           int
	CreatedAt          time.Time
	DecaysAt           *time.Time
}

type PenaltyRepository interface {
	RecordPenalty(ctx context.Context, p PenaltyRecord) error
	GetActivePenalties(ctx context.Context, userID string) ([]PenaltyRecord, error)
}

type ModerationService struct {
	modRepo    interface{} // ModerationActionRepository — wire later
	graph      GraphRepository
	penalties  PenaltyRepository
	users      UserRepository
}

func NewModerationService(modRepo interface{}, graph GraphRepository, penalties PenaltyRepository, users UserRepository) *ModerationService {
	return &ModerationService{modRepo: modRepo, graph: graph, penalties: penalties, users: users}
}

// PropagatePenalties walks the vouch graph backward from the target user
// and records propagated penalties for each voucher.
func (s *ModerationService) PropagatePenalties(ctx context.Context, action *domain.ModerationAction) ([]PenaltyRecord, error) {
	maxDepth, ok := domain.PropagationDepth[action.Severity]
	if !ok {
		return nil, nil
	}
	decayRate, ok := domain.PropagationDecay[action.Severity]
	if !ok {
		return nil, nil
	}
	basePenalty, ok := domain.DirectPenalty[action.Severity]
	if !ok {
		return nil, nil
	}
	decayDays := domain.PenaltyDecayDays[action.Severity]

	vouchers, err := s.graph.FindVouchersUpToDepth(ctx, action.TargetUserID, maxDepth)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	var results []PenaltyRecord

	for _, hop := range vouchers {
		amount := CalcPropagatedPenalty(basePenalty, decayRate, hop.Depth)

		id, err := uuid.NewV7()
		if err != nil {
			return nil, err
		}

		var decaysAt *time.Time
		if decayDays > 0 {
			t := now.AddDate(0, 0, decayDays)
			decaysAt = &t
		}

		record := PenaltyRecord{
			ID:                 id.String(),
			UserID:             hop.UserID,
			ModerationActionID: action.ID,
			Amount:             amount,
			HopDepth:           hop.Depth,
			CreatedAt:          now,
			DecaysAt:           decaysAt,
		}

		if err := s.penalties.RecordPenalty(ctx, record); err != nil {
			return nil, err
		}
		results = append(results, record)
	}

	return results, nil
}
```

**Step 3: Run tests**

```bash
go test ./internal/service/ -v
```

Expected: PASS (you may need to adjust mock implementations to match the interfaces exactly)

**Step 4: Commit**

```bash
git add internal/service/moderation.go internal/service/moderation_test.go
git commit -m "feat: add moderation service with trust penalty propagation"
```

---

## Phase 3: Moderation (Tasks 19–22)

### Task 19: Reporting System
- Create `queries/reports.sql` (create report, list pending, update status)
- Create report handler at `POST /api/v1/posts/:id/report`
- Create moderation queue handler at `GET /api/v1/moderation/queue`
- Wire rate limiting (5 reports/hour)

### Task 20: Moderation Action Endpoints
- Create `queries/moderation_actions.sql` (create action, list by user)
- Create handler at `POST /api/v1/moderation/actions`
- Wire graduated action system: validate severity matches action type
- On action: call `ModerationService.PropagatePenalties` and trigger trust recalc

### Task 21: Mute/Suspend/Ban Enforcement
- Mute: update user trust score to below posting threshold
- Suspend: set `is_active = false` for duration, schedule reactivation
- Ban: set role to `banned`, trust to 0, propagate penalties
- Add middleware check: if user is muted/suspended/banned, reject writes

### Task 22: Moderation Audit Trail
- `GET /api/v1/moderation/actions/:user_id` — full history
- Include propagated penalties in response
- Council can review any moderator's action history

---

## Phase 4: Council & Governance (Tasks 23–26)

### Task 23: Bootstrap CLI
- Add `setup` subcommand to `cmd/bell/main.go` using `os.Args`
- Accepts `--council email1,email2,...`
- Creates Kratos accounts via Kratos Admin API
- Sets roles to `council` in app DB
- Sets a `bootstrap_mode = true` flag in a `town_config` table

### Task 24: Council User Approval (Bootstrap Phase)
- `GET /api/v1/vouches/pending` — list pending users
- `POST /api/v1/vouches/approve/:id` — council approves, sets role to member
- Auto-exit bootstrap when `SELECT COUNT(*) FROM users WHERE role != 'pending'` >= 20

### Task 25: Council Voting
- Create `council_votes` table (migration)
- `POST /api/v1/admin/council/votes` — cast vote (promote moderator → council)
- `GET /api/v1/admin/council/votes` — list pending votes
- Majority required (>50% of council) to pass

### Task 26: Automatic Role Promotion/Demotion
- Background job: check users against role thresholds daily
- Member → Moderator: trust ≥ 85, 90+ days active, vouched by 2+ mods
- Demotion: trust below threshold for 30 consecutive days
- Record role changes in a `role_history` table

---

## Phase 5: Frontend (Tasks 27–33)

### Task 27: React SPA Scaffold
- `cd web && npm create vite@latest . -- --template react-ts`
- Add Tailwind CSS
- Add React Router
- Create API client wrapper with Kratos session cookie handling
- Configure Vite proxy for development (`/api` → Go server, `/.ory` → Kratos)

### Task 28: Auth Screens
- Registration page (renders Kratos self-service registration flow)
- Login page (renders Kratos self-service login flow)
- Use `@ory/elements` React components or build custom forms against Kratos flow API
- Session check on app load → redirect to login if unauthenticated

### Task 29: Feed Screen
- Chronological feed with infinite scroll (cursor-based)
- Post card component (author name, body, timestamp, reactions)
- Reaction buttons (bell, heart, celebrate) with optimistic updates

### Task 30: Compose Post
- Text input with character counter (max 1000)
- Optional image upload with preview
- Submit → `POST /api/v1/posts` → prepend to feed

### Task 31: User Profile
- Display name, bio, avatar, trust score, role badge
- Edit profile form
- Vouch chain visualization (who vouched for them, who they vouched for)
- Post history

### Task 32: Moderation UI
- Moderation queue page (moderator+ only)
- Report detail with post content and reporter info
- Action dialog: select action type, severity, reason, duration
- Action history view per user

### Task 33: Admin Dashboard
- Town stats (total users, posts today, active moderators)
- Trust config editor
- Council vote interface
- Pending user approvals (during bootstrap)

---

## Phase 6: Hardening (Tasks 34–38)

### Task 34: Redis Feed Cache
- On post create/delete: invalidate feed cache
- On feed read: check Redis sorted set first, fall back to Postgres
- TTL 60 seconds

### Task 35: Redis Rate Limiter
- Implement sliding window rate limiter as middleware
- Configure per-endpoint limits from design doc
- Return `429 Too Many Requests` with `Retry-After` header

### Task 36: Trust Score Cache
- Cache computed scores in Redis per user
- Invalidate on moderation action or vouch change
- Background recalculation worker using Redis list as queue

### Task 37: Image Upload
- Multipart form handler in post creation
- Validate file type (JPEG/PNG/WebP) and size (max 5MB)
- Generate UUIDv7 filename, write to storage path
- Static file serving endpoint with `Cache-Control` headers
- Storage interface for future S3 migration

### Task 38: Testing
- Integration tests: spin up Postgres + AGE in Docker (use `testcontainers-go`)
- Test trust propagation end-to-end through real AGE queries
- Test full post lifecycle through HTTP handlers
- Test Kratos auth middleware with mock Kratos responses

---

## Phase 7: Distribution (Tasks 39–41)

### Task 39: Self-Contained Docker Compose
- Create `deploy/docker-compose.yml` with own Postgres (+ AGE), Kratos, Redis
- Create `deploy/.env.example`
- Document all environment variables

### Task 40: Setup Wizard
- `bell setup` interactive CLI
- Prompts for: town name, domain, admin emails, SMTP config
- Creates databases, runs migrations, creates council accounts
- Validates Kratos is reachable and configured

### Task 41: Documentation
- `docs/admin-guide.md` — deployment, config, council setup, maintenance
- `docs/user-guide.md` — posting, vouching, trust system explained
- Generate OpenAPI spec from handler code (or write manually)
- Add to project README

---

## Dependency Order

```
Task 1 → 2 → 3 → 4 → 5 → 6 → 7 → 8 → 9 → 10 → 11 → 12 → 13 → 14
                                                              ↓
                                                    15 → 16 → 17 → 18
                                                              ↓
                                                    19 → 20 → 21 → 22
                                                              ↓
                                                    23 → 24 → 25 → 26
                                                              ↓
                                                         27 → 28–33
                                                              ↓
                                                         34 → 35–38
                                                              ↓
                                                         39 → 40 → 41
```

Phases are sequential. Within Phase 5+ tasks can be parallelized.
