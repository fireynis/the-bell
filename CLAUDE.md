# CLAUDE.md — The Bell

## Overview

Trust-based micro-blogging platform for municipalities. Go backend (chi router, sqlc, Apache AGE graph), React 19 frontend (Vite, Tailwind), Ory Kratos auth, Redis caching, PostgreSQL with AGE extension.

## Architecture

- **Backend**: `cmd/bell/main.go` → services → repository adapters → sqlc queries
- **Frontend**: `web/src/` React SPA served by Go at `/*`, API at `/api/v1/*`
- **Auth proxy**: `/.ory/*` reverse-proxied to Kratos (browser never talks to Kratos directly)
- **Trust graph**: Apache AGE (Postgres extension) for vouch edges and penalty propagation

## Key Paths

- `internal/app/` — the single dependency-wiring graph: `Build(cfg, pool, rdb, logger) (*Deps, error)`. Use it instead of hand-wiring repos and services; `cmd/bell` already does, and the integration harness is moving to it. Four divergent copies of this wiring is why the test server once silently lacked reactions, uploads, rate limiting and SSE
- `internal/service/` — business logic (post, user, vouch, moderation, voting, role checker, stats)
- `internal/handler/` — HTTP handlers (thin layer, delegates to services)
- `internal/httpjson/` — the one JSON response writer; handler and middleware both delegate to it so the error bytes on the wire cannot drift apart between layers
- `internal/repository/postgres/` — sqlc-generated code + adapter types bridging sqlc→service interfaces
- `internal/middleware/` — Kratos auth, role checks, rate limiter, logging
- `internal/cache/` — Redis feed cache, trust score cache + background worker
- `internal/testsupport/` — containerized `TestDB`/`TestRedis` so tests use real Postgres and Redis rather than mocks. The `integration` build tag goes on the **calling test files**, not on this package
- `queries/` — SQL source files for sqlc
- `migrations/` — goose migrations (important: 00001 and 00007 set AGE search_path, must reset to public)
- `kratos/` — Kratos config and identity schema
- `deploy/` — self-contained Docker Compose for fresh deployments
- `web/src/pages/` — React pages (Home, Profile, Compose, Admin, Moderation, auth screens)

## Commands

```bash
go build ./...                           # build
go test ./...                            # unit tests
go test -tags integration ./...          # integration tests (needs Docker)
cd web && npm run build                  # frontend build
cd web && npx tsc -b                     # type-check frontend (NOT --noEmit)
docker compose up -d --build             # deploy (from project root)
```

Integration tests are no longer confined to `internal/integration/` — they also
live in `internal/repository/postgres/` and `internal/middleware/`, so scope the
tag to `./...` rather than one directory or you will silently skip most of them.

**Use `tsc -b`, never `tsc --noEmit`.** `web/tsconfig.json` is a solution file
(`"files": []` plus project references), so plain `tsc` has no inputs and exits
0 even when type errors exist. Verified: a deliberate `TS2322` passes
`--noEmit` and is caught only by `-b`. `npm run build` runs `tsc -b` first, so
a green build is real type-checking; a green `--noEmit` proves nothing.

**`go test -race` needs a C toolchain** (it requires cgo). A machine without
gcc/cc/clang cannot run it at all — it fails with "requires cgo" rather than
passing, so local green does *not* mean race-free. The SSE broker, trust worker
and rate limiter are the concurrency-sensitive spots; CI is where `-race` must
run.

## CLI Subcommands

- `bell serve` — start HTTP server
- `bell setup [--council=emails] [--town-name=Name] [--create-db]` — bootstrap wizard; prompts interactively for anything not passed as a flag. `--create-db` derives both database names from `DATABASE_URL` (`<name>` and `<name>_kratos`), it does not assume `bell`
- `bell check-roles` — run promotion/demotion checks

## Production

Running at `https://bell.themacarthurs.ca` via Traefik with Cloudflare DNS-01 TLS. Uses its own `bell-postgres` (apache/age) container, not the shared Postgres.

## Test Accounts

`TEST_ACCOUNTS.md` (gitignored) contains test credentials for the running instance.

## Important Notes

- **`go test -race` does not run on a box without a C toolchain** — the race detector requires cgo, so it fails to build where gcc is absent. Local green therefore says nothing about data races. Anything concurrency-sensitive (SSE broker, trust worker, rate limiter) must be race-verified in CI, where a toolchain exists; do not treat a clean local `go test ./...` as evidence of race-freedom
- **The Postgres image pin must stay identical in three places**: `docker-compose.yml`, `deploy/docker-compose.yml`, and `internal/testsupport/testsupport.go` (`postgresImage`), currently `apache/age:release_PG18_1.7.0`. `apache/age:latest` floats across Postgres *major* versions, and Postgres refuses to start against a data directory written by a different major — so an unpinned or drifted tag breaks existing deployments and makes tests disagree with production
- sqlc-generated files (`internal/repository/postgres/*.sql.go`) should not be edited manually — edit `queries/*.sql` and run `sqlc generate`
- AGE migrations (00001, 00007) must reset `search_path` to `public` after AGE operations or subsequent DDL lands in `ag_catalog`
- Kratos in production mode requires `secrets.cookie` and `secrets.cipher` in config
- The `profile` group must be in FlowForm's `VISIBLE_GROUPS` for Kratos v1.x two-step registration
