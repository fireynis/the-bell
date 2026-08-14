# The Bell

The Bell is a trust-based micro-blogging platform designed for municipalities. It provides a local online space where residents can share short posts, vouch for one another, and collectively build community trust. Unlike open social platforms, The Bell uses a reputation system backed by a graph database to ensure that participation is earned through genuine community engagement.

New users start as "pending" and must be vouched for by trusted members before they can post. Moderation penalties propagate through the vouch graph, meaning that if someone you vouched for misbehaves, your own trust score is affected. This creates a self-regulating community where members have a stake in the behavior of those they endorse.

## Features

- **Trust-based posting**: Users must maintain a minimum trust score to post (30) and vouch (60)
- **Vouch graph**: Powered by Apache AGE (graph extension for PostgreSQL), vouches form a directed graph that enables trust propagation
- **Automatic role management**: Members are promoted to moderators based on trust score, tenure, and community endorsement; demoted if trust falls
- **Graduated moderation**: Warn, mute, suspend, and ban actions with proportional trust penalties that propagate through the vouch network
- **Post removal**: Moderators can take a single post down with a recorded reason, separately from actioning its author
- **Council governance**: Founding council members bootstrap the community and vote on proposals using simple majority
- **Bootstrap mode**: Council-driven user approval during the early growth phase (first 20 members)
- **Image uploads**: JPEG, PNG, and WebP support with magic-byte validation (max 5 MB)
- **Cursor-based feed**: Efficient infinite-scroll pagination
- **Rate limiting**: Per-user sliding window limits via Redis
- **Feed caching**: Optional Redis-backed feed cache for performance
- **Background trust scoring**: Trust scores are recomputed by a background worker

## Quick Start

The `deploy/` directory is a self-contained stack — PostgreSQL with Apache AGE,
Kratos, Redis, the app, and a role-check scheduler — on a private network with
one published port.

```bash
cd deploy
cp .env.example .env

# Generate the required secrets and paste them into .env
echo "POSTGRES_PASSWORD=$(openssl rand -hex 16)"
echo "KRATOS_SECRETS_COOKIE=$(openssl rand -hex 32)"
echo "KRATOS_SECRETS_CIPHER=$(openssl rand -hex 16)"

docker compose up -d --build

# Bootstrap with initial council members
docker compose exec -it bell ./bell setup --council=mayor@springfield.gov,clerk@springfield.gov
```

The site is then at `http://localhost:8080`. Set `PUBLIC_URL` in `.env` to the
address residents will actually use — every Kratos URL and the session cookie
domain follow it.

**[deploy/README.md](deploy/README.md) is the full guide**, and it covers the
parts a real town needs: TLS (the defaults are a plain-HTTP trial and are not
safe on a public address), SMTP for password recovery, backups, and upgrades.

Databases are created for you: on first start the Postgres container creates
`bell` from `POSTGRES_DB` and runs `deploy/init-db.sh`, which creates
`bell_kratos`. Against a pre-existing Postgres that skipped those init hooks,
run `bell setup --create-db` instead — it derives both names from
`DATABASE_URL`, so a DSN ending in `/thebell` yields `thebell` and
`thebell_kratos`.

`bell setup` prompts for anything you omit, so `-it` matters if you leave off
`--council` or `--town-name`.

> The `docker-compose.yml` in the repository root is **not** the one to start
> from. It is the maintainer's own deployment of `bell.themacarthurs.ca`, with
> that hostname baked into Traefik labels, an external `proxy` network, and host
> paths under `/storage/the-bell`.

## Architecture

```
+------------------+     +------------------+     +------------------+
|   React SPA      |---->|   Go API Server  |---->|   PostgreSQL     |
|   (Vite/React)   |     |   (chi router)   |     |   + Apache AGE   |
+------------------+     +------------------+     +------------------+
                               |       |
                               v       v
                         +---------+ +----------+
                         |  Redis  | | Ory      |
                         | (cache) | | Kratos   |
                         +---------+ +----------+
```

- **Go backend**: HTTP API built with [chi](https://github.com/go-chi/chi). Handles business logic, trust computation, and moderation. Serves both the API and the SPA static files from a single binary.
- **React frontend**: Single-page application built with React, React Router, and Tailwind CSS. Compiled by Vite and served from the Go binary.
- **PostgreSQL + Apache AGE**: Primary data store. AGE provides graph queries for the vouch trust network (cycle detection, neighbor traversal for penalty propagation).
- **Ory Kratos**: Handles user registration, login, session management, password recovery, and email verification. The Bell creates a local user record linked to each Kratos identity.
- **Redis**: Optional but recommended. Enables feed caching, trust score background computation, and per-user rate limiting. Runs as an ephemeral cache (no persistence).

### Build

The Dockerfile uses a multi-stage build:

1. **Go builder**: Compiles the `bell` binary from `cmd/bell/`
2. **Node builder**: Builds the React SPA with `npm run build`
3. **Final image**: Alpine-based, contains the binary, SPA assets, and migration files

```bash
docker build -t the-bell .
```

### CLI Commands

| Command | Description |
|---------|-------------|
| `bell serve` | Start the HTTP server (runs migrations automatically) |
| `bell setup --council=emails` | Bootstrap the town with initial council members |
| `bell check-roles` | Run automatic role promotion/demotion checks |

## Documentation

- [Admin Guide](docs/admin-guide.md) -- Deployment, configuration, and operations
- [User Guide](docs/user-guide.md) -- How the platform works for end users
- [API Reference](docs/api-reference.md) -- Complete HTTP API documentation

## License

MIT
