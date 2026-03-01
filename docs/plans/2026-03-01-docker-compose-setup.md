# Docker Compose Setup Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Create the full Docker Compose stack (bell app, Ory Kratos, Redis) with Kratos identity config, multi-stage Dockerfile, and environment template — integrated with the home server's shared Postgres and Traefik proxy.

**Architecture:** The bell service builds from a multi-stage Dockerfile (Go API + React SPA → single image) and joins the `proxy` external Docker network alongside Kratos (identity/auth), a Kratos migration init container, and a dedicated Redis instance. Kratos uses the shared Postgres with its own `bell_kratos` database. The bell app uses the shared Postgres with a `bell` database. Traefik routes `bell.home.arpa` to the bell container on port 8080.

**Tech Stack:** Docker Compose, Ory Kratos v1.3.1, Redis 7 Alpine, Go 1.26 (multi-stage build), Node 22 (multi-stage build), shared PostgreSQL 18

**Beads Task:** `the-bell-zhd.7` — Docker Compose setup (P1)

---

## Prerequisites (manual, one-time)

Before running `docker compose up`, the shared Postgres needs two databases:

```sql
-- Connect to shared postgres (from db/ directory or psql)
CREATE DATABASE bell;
CREATE DATABASE bell_kratos;
-- AGE extension (for the bell database, needed by later migration tasks)
\c bell
CREATE EXTENSION IF NOT EXISTS age;
```

These are NOT part of this task's deliverables but are documented here for the executor.

---

## Version Decisions

| Component | Implementation Plan Says | This Plan Uses | Reason |
|-----------|------------------------|----------------|--------|
| Kratos | v1.3.0 | v1.3.1 | Bugfix release (Android passkey fix). Same config format. Compatible with `kratos-client-go v1.3.8` in go.mod. |
| Redis | 7-alpine | 7-alpine | No change needed |
| Go | 1.26-alpine | 1.26-alpine | Matches `.tool-versions` |
| Node | 22-alpine | 22-alpine | LTS, matches design doc |

---

### Task 1: Create Kratos Identity Schema

**Files:**
- Create: `kratos/identity.schema.json`

**Step 1: Remove .gitkeep sentinel**

```bash
rm kratos/.gitkeep
```

**Step 2: Create identity schema**

Create `kratos/identity.schema.json` — defines the user traits (email as credential identifier, display name):

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

**Step 3: Validate JSON syntax**

Run: `python3 -c "import json; json.load(open('kratos/identity.schema.json'))"; echo "valid"`
Expected: `valid`

**Step 4: Commit**

```bash
git add kratos/identity.schema.json
git rm --cached kratos/.gitkeep 2>/dev/null || true
git commit -m "feat: add Kratos identity schema (email + display name)"
```

---

### Task 2: Create Kratos Configuration

**Files:**
- Create: `kratos/kratos.yml`

**Step 1: Create Kratos config**

Create `kratos/kratos.yml`. Key design decisions:
- `dsn` is set to a placeholder; the `DSN` environment variable in docker-compose.yml overrides it at runtime
- CORS allows both localhost:5173 (Vite dev) and bell.home.arpa (production)
- `--dev` flag on the serve command disables HTTPS requirements (appropriate for LAN-only home server behind Traefik)
- Session cookie lifespan: 30 days
- Password minimum length: 8 characters
- Registration auto-creates a session (hook: session)

```yaml
version: v1.3.1

dsn: memory

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
  lifespan: 720h

identity:
  default_schema_id: default
  schemas:
    - id: default
      url: file:///etc/kratos/identity.schema.json

log:
  level: info
  format: json
```

**Step 2: Validate YAML syntax**

Run: `python3 -c "import yaml; yaml.safe_load(open('kratos/kratos.yml'))"; echo "valid"`
Expected: `valid`

**Step 3: Commit**

```bash
git add kratos/kratos.yml
git commit -m "feat: add Kratos server configuration (password auth, session flows)"
```

---

### Task 3: Create Environment Template

**Files:**
- Create: `.env.example`

**Step 1: Create .env.example**

```bash
# Database (shared Postgres on proxy network)
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

**Step 2: Verify .env is in .gitignore**

Run: `grep '^\.env$' .gitignore`
Expected: `.env` (already present — confirmed in current .gitignore)

**Step 3: Commit**

```bash
git add .env.example
git commit -m "feat: add .env.example with all required environment variables"
```

---

### Task 4: Create Multi-Stage Dockerfile

**Files:**
- Create: `Dockerfile`

**Step 1: Create Dockerfile**

Multi-stage build: Go API binary + React SPA static assets → single minimal Alpine image.

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

**Important note:** The `web-builder` stage will fail until the React SPA is scaffolded (Phase 5). This is intentional — the Dockerfile is complete and correct for the final architecture. During Phase 1 development, use `docker compose up` with only the kratos/redis services, or build with `--target go-builder` to skip the web stage.

**Step 2: Validate Dockerfile syntax**

Run: `docker build --check . 2>&1 | head -5 || echo "docker build --check not available, syntax looks ok"`

Note: Full build will fail without `web/package.json`. That's expected and fine — this task creates the Dockerfile for the final architecture.

**Step 3: Commit**

```bash
git add Dockerfile
git commit -m "feat: add multi-stage Dockerfile (Go API + React SPA)"
```

---

### Task 5: Create Docker Compose Stack

**Files:**
- Create: `docker-compose.yml`

**Step 1: Create docker-compose.yml**

Four services:
- `bell` — the Go app (builds from Dockerfile), Traefik-labeled for `bell.home.arpa`
- `kratos-migrate` — one-shot Kratos DB migration
- `kratos` — Ory Kratos identity server (depends on successful migration)
- `redis-bell` — ephemeral Redis for caching/rate-limiting (no persistence, matching home server pattern from outline/redis-outline)

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
    image: oryd/kratos:v1.3.1
    container_name: kratos-migrate
    restart: "no"
    command: migrate sql -e --yes
    environment:
      DSN: postgres://appuser:${POSTGRES_PASSWORD}@postgres:5432/bell_kratos?sslmode=disable
    networks:
      - proxy

  kratos:
    image: oryd/kratos:v1.3.1
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

**Step 2: Validate compose syntax**

Run: `docker compose config --quiet 2>&1; echo "exit: $?"`
Expected: exit 0 (requires a `.env` file — copy from `.env.example` first)

Validation sequence:
```bash
cp .env.example .env
docker compose config --quiet
rm .env
```

**Step 3: Commit**

```bash
git add docker-compose.yml
git commit -m "feat: add Docker Compose stack (bell, kratos, redis) with Traefik routing"
```

---

### Task 6: Smoke-Test the Infrastructure Services

This task validates that Kratos and Redis start correctly (the bell service won't build yet without the React SPA).

**Step 1: Create .env from template**

```bash
cp .env.example .env
# Edit .env with the actual POSTGRES_PASSWORD from the shared Postgres
```

**Step 2: Ensure databases exist**

```bash
docker exec -i postgres psql -U appuser -c "CREATE DATABASE bell;" 2>/dev/null || echo "bell db already exists"
docker exec -i postgres psql -U appuser -c "CREATE DATABASE bell_kratos;" 2>/dev/null || echo "bell_kratos db already exists"
```

**Step 3: Start infrastructure services only**

```bash
docker compose up -d kratos-migrate kratos redis-bell
```

**Step 4: Verify services are running**

```bash
docker compose ps
```

Expected: `kratos` and `redis-bell` running, `kratos-migrate` exited 0.

**Step 5: Verify Kratos health**

```bash
docker exec kratos wget -qO- http://localhost:4434/admin/health/ready
```

Expected: `{"status":"ok"}`

**Step 6: Clean up test**

```bash
docker compose down
rm .env
```

**Step 7: Final commit (no code changes, just close the task)**

```bash
bd close the-bell-zhd.7 --reason="All 5 files created: docker-compose.yml, Dockerfile, kratos/kratos.yml, kratos/identity.schema.json, .env.example. Kratos + Redis validated."
```

---

## Edge Cases & Risks

| Risk | Mitigation |
|------|------------|
| `golang:1.26-alpine` image doesn't exist on Docker Hub yet | If build fails, check `docker search golang` and use latest available (e.g., `1.24-alpine`). Update `.tool-versions` to match. |
| Kratos v1.3.1 image pull fails | Fall back to `v1.3.0` (confirmed available). The config format is identical. |
| Shared Postgres `bell` / `bell_kratos` databases don't exist | Task 6 Step 2 handles this. Document in README or AGENTS.md. |
| `web/` directory missing breaks Dockerfile build | Expected — documented in Task 4. Use `docker compose up kratos redis-bell` until Phase 5 scaffolds the React SPA. |
| Kratos config `dsn: memory` vs `DSN` env var | Kratos prioritizes the `DSN` environment variable over config file `dsn`. The `memory` default in config is safe — it's always overridden by the compose env. |
| Redis container name collision with other services | Named `redis-bell` (not `redis`) to avoid conflict with any future redis instances, matching the pattern from `redis-outline`. |
| `--dev` flag on Kratos | Disables HTTPS enforcement. Appropriate for LAN-only deployment behind Traefik. Remove if deploying to public internet. |
| POSTGRES_PASSWORD not set | `docker compose config` will warn. `.env.example` documents the required variable. |

## Test Strategy

Since this task creates infrastructure/config files (not Go code), testing is operational:

1. **Static validation**: JSON syntax check on identity schema, YAML syntax check on kratos.yml, `docker compose config` validation
2. **Build validation**: `docker compose build` (Go stage only until web/ exists)
3. **Integration smoke test**: Start kratos-migrate + kratos + redis-bell, verify Kratos health endpoint
4. **Pattern compliance**: Verify Traefik labels match home server conventions (compare with outline, forgejo compose files)
