# CLAUDE.md — The Bell

## Overview

Trust-based micro-blogging platform for municipalities. Go backend (chi router, sqlc, Apache AGE graph), React 19 frontend (Vite, Tailwind), Ory Kratos auth, Redis caching, PostgreSQL with AGE extension.

## Architecture

- **Backend**: `cmd/bell/main.go` → services → repository adapters → sqlc queries
- **Frontend**: `web/src/` React SPA served by Go at `/*`, API at `/api/v1/*`
- **Auth proxy**: `/.ory/*` reverse-proxied to Kratos (browser never talks to Kratos directly)
- **Trust graph**: Apache AGE (Postgres extension) for vouch edges and penalty propagation

## Key Paths

- `internal/service/` — business logic (post, user, vouch, moderation, voting, role checker, stats)
- `internal/handler/` — HTTP handlers (thin layer, delegates to services)
- `internal/repository/postgres/` — sqlc-generated code + adapter types bridging sqlc→service interfaces
- `internal/middleware/` — Kratos auth, role checks, rate limiter, logging
- `internal/cache/` — Redis feed cache, trust score cache + background worker
- `queries/` — SQL source files for sqlc
- `migrations/` — goose migrations (important: 00001 and 00007 set AGE search_path, must reset to public)
- `kratos/` — Kratos config and identity schema
- `deploy/` — self-contained Docker Compose for fresh deployments
- `web/src/pages/` — React pages (Home, Profile, Compose, Admin, Moderation, auth screens)

## Commands

```bash
go build ./...                           # build
go test ./...                            # unit tests
go test -tags integration ./internal/integration/...  # integration tests (needs Docker)
cd web && npm run build                  # frontend build
cd web && npx tsc --noEmit               # type-check frontend
docker compose up -d --build             # deploy (from project root)
```

## CLI Subcommands

- `bell serve` — start HTTP server
- `bell setup --council=emails --town-name=Name` — bootstrap wizard
- `bell check-roles` — run promotion/demotion checks

## Production

Running at `https://bell.themacarthurs.ca` via Traefik with Cloudflare DNS-01 TLS. Uses its own `bell-postgres` (apache/age) container, not the shared Postgres.

## Test Accounts

`TEST_ACCOUNTS.md` (gitignored) contains test credentials for the running instance.

## Important Notes

- sqlc-generated files (`internal/repository/postgres/*.sql.go`) should not be edited manually — edit `queries/*.sql` and run `sqlc generate`
- AGE migrations (00001, 00007) must reset `search_path` to `public` after AGE operations or subsequent DDL lands in `ag_catalog`
- Kratos in production mode requires `secrets.cookie` and `secrets.cipher` in config
- The `profile` group must be in FlowForm's `VISIBLE_GROUPS` for Kratos v1.x two-step registration
