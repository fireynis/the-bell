# The Bell

The Bell is a trust-based micro-blogging platform designed for municipalities. It provides a local online space where residents can share short posts, vouch for one another, and collectively build community trust. Unlike open social platforms, The Bell uses a reputation system backed by a graph database to ensure that participation is earned through genuine community engagement.

New users start as "pending" and must be vouched for by trusted members before they can post. Moderation penalties propagate through the vouch graph, meaning that if someone you vouched for misbehaves, your own trust score is affected. This creates a self-regulating community where members have a stake in the behavior of those they endorse.

## Features

- **Trust-based posting**: Users must maintain a minimum trust score to post (30) and vouch (60)
- **Vouch graph**: Powered by Apache AGE (graph extension for PostgreSQL), vouches form a directed graph that enables trust propagation
- **Automatic role management**: Members are promoted to moderators based on trust score, tenure, and community endorsement; demoted if trust falls
- **Graduated moderation**: Warn, mute, suspend, and ban actions with proportional trust penalties that propagate through the vouch network
- **Council governance**: Founding council members bootstrap the community and vote on proposals using simple majority
- **Bootstrap mode**: Council-driven user approval during the early growth phase (first 20 members)
- **Image uploads**: JPEG, PNG, and WebP support with magic-byte validation (max 5 MB)
- **Cursor-based feed**: Efficient infinite-scroll pagination
- **Rate limiting**: Per-user sliding window limits via Redis
- **Feed caching**: Optional Redis-backed feed cache for performance
- **Background trust scoring**: Trust scores are recomputed by a background worker

## Quick Start

```bash
# Ensure the shared Docker network exists
docker network create proxy

# Create the required PostgreSQL databases
psql -h postgres -U appuser -c "CREATE DATABASE bell;"
psql -h postgres -U appuser -c "CREATE DATABASE bell_kratos;"

# Configure
cat > .env <<EOF
POSTGRES_PASSWORD=your_db_password
TOWN_NAME=Springfield
EOF

# Start all services
docker compose up -d

# Bootstrap with initial council members
docker exec bell ./bell setup --council=mayor@springfield.gov,clerk@springfield.gov
```

The application is then available at `http://bell.home.arpa` (or whatever domain your reverse proxy is configured with).

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
