# Administration Guide

## Prerequisites

- **Docker** and **Docker Compose** (v2+)
- **PostgreSQL 18** with the [Apache AGE](https://age.apache.org/) graph extension enabled (used for the vouch trust graph)
- **Ory Kratos v1.3.1** for identity and session management
- **Redis 7** (optional but recommended -- enables feed caching, trust score caching, and rate limiting)
- An external Docker network called `proxy` (`docker network create proxy`)

## Quick Start

1. Create the required PostgreSQL databases:

```sql
CREATE DATABASE bell;
CREATE DATABASE bell_kratos;
```

2. Create a `.env` file in the project root:

```env
POSTGRES_PASSWORD=your_db_password
TOWN_NAME=Springfield
```

3. Start all services:

```bash
docker compose up -d
```

This brings up four containers:

| Container | Purpose |
|-----------|---------|
| `bell` | The Bell API + SPA (port 8080) |
| `kratos` | Ory Kratos identity server (ports 4433/4434) |
| `kratos-migrate` | Runs Kratos DB migrations then exits |
| `redis-bell` | Ephemeral Redis (no persistence, cache only) |

4. Bootstrap the town with initial council members:

```bash
docker exec bell ./bell setup --council=alice@example.com,bob@example.com
```

This creates Kratos identities and local users with the `council` role and a trust score of 100. It also enables bootstrap mode, which allows council members to directly approve pending users until 20 active members are reached.

## Configuration Reference

All configuration is via environment variables. The Bell binary reads them at startup using `github.com/caarlos0/env/v11`.

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `DATABASE_URL` | Yes | -- | PostgreSQL connection string. Example: `postgres://appuser:pass@postgres:5432/bell?sslmode=disable` |
| `KRATOS_PUBLIC_URL` | Yes | -- | Kratos public API URL. Example: `http://kratos:4433` |
| `KRATOS_ADMIN_URL` | Yes | -- | Kratos admin API URL. Example: `http://kratos:4434` |
| `PORT` | No | `8080` | HTTP listen port |
| `REDIS_URL` | No | (empty) | Redis connection string. When set, enables feed caching, trust score background worker, and per-user rate limiting. Example: `redis://redis-bell:6379` |
| `IMAGE_STORAGE_PATH` | No | `/storage/the-bell/images` | Filesystem path for uploaded images |
| `TOWN_NAME` | No | `My Town` | Display name for the municipality |

## CLI Commands

The `bell` binary provides three commands:

### `bell serve`

Starts the HTTP server. Connects to the database, runs migrations automatically, and begins serving the API and SPA.

```bash
./bell serve
```

### `bell setup --council=email1,email2,...`

Bootstraps the town. Must be run exactly once before users can register. It:

1. Creates Kratos identities for each email
2. Creates local users with `council` role and trust score 100
3. Enables bootstrap mode (council can directly approve pending users)

```bash
docker exec bell ./bell setup --council=mayor@springfield.gov,clerk@springfield.gov
```

The command is idempotent -- it will refuse to run if bootstrap mode is already enabled.

### `bell check-roles`

Evaluates all active users for automatic role promotion and demotion. This should be run periodically via cron (e.g., daily).

```bash
docker exec bell ./bell check-roles
```

Promotion criteria (member to moderator):
- Trust score >= 85
- Member for >= 90 days
- At least 2 vouches from moderators or council members

Demotion criteria:
- Trust score < 70 for 30 consecutive days
- Moderator demoted to member, member demoted to pending

Example cron entry:

```cron
0 3 * * * docker exec bell ./bell check-roles >> /var/log/bell-roles.log 2>&1
```

## Database Migrations

Migrations run automatically on startup. The Bell binary calls `database.RunMigrations()` before starting the HTTP server or running any CLI command. Migration files live in the `migrations/` directory and are embedded in the binary.

## Reverse Proxy Setup

The Bell serves both the API (`/api/v1/...`, `/healthz`) and the React SPA (from `web/dist/`) on a single port.

### Traefik

The included `docker-compose.yml` has Traefik labels pre-configured:

```yaml
labels:
  - "traefik.enable=true"
  - "traefik.http.routers.bell.rule=Host(`bell.home.arpa`)"
  - "traefik.http.services.bell.loadbalancer.server.port=8080"
  - "traefik.http.routers.bell-secure.rule=Host(`bell.themacarthurs.ca`)"
  - "traefik.http.routers.bell-secure.entrypoints=websecure"
  - "traefik.http.routers.bell-secure.tls.certResolver=cloudflare"
```

### nginx

```nginx
server {
    listen 80;
    server_name bell.example.com;

    location / {
        proxy_pass http://bell:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

Kratos also needs to be accessible to the user's browser for authentication flows. Configure Kratos's `serve.public.base_url` in `kratos/kratos.yml` to point to a publicly reachable URL.

## Backup Considerations

Data to back up:

- **PostgreSQL databases**: `bell` (application data) and `bell_kratos` (identity data)
- **Image uploads**: the directory configured by `IMAGE_STORAGE_PATH` (default `/storage/the-bell/images`)

Redis does not need backup -- it is configured as an ephemeral cache with no persistence (`--save "" --appendonly no`).

Example pg_dump:

```bash
pg_dump -h postgres -U appuser bell > bell_backup.sql
pg_dump -h postgres -U appuser bell_kratos > bell_kratos_backup.sql
```

## Monitoring

### Health Check

The `/healthz` endpoint returns HTTP 200 with a JSON body when the server is running:

```bash
curl http://bell:8080/healthz
```

```json
{"status":"ok"}
```

The Docker Compose healthcheck is configured to probe this endpoint every 30 seconds:

```yaml
healthcheck:
  test: ["CMD-SHELL", "wget -q -O /dev/null http://localhost:8080/healthz || exit 1"]
  interval: 30s
  timeout: 10s
  retries: 3
  start_period: 30s
```

### Resource Limits

The default compose file sets conservative resource limits:

| Container | Memory | CPU |
|-----------|--------|-----|
| `bell` | 256 MB | 0.5 |
| `kratos` | 256 MB | 0.5 |
| `redis-bell` | 128 MB | 0.25 |

### Logging

The Bell outputs structured JSON logs to stdout via `slog.JSONHandler`. These can be collected by any Docker log driver (e.g., Loki via Alloy, Fluentd, etc.).

## Bootstrap Mode

Bootstrap mode is the initial phase of a new town deployment. During bootstrap mode:

- Council members can directly approve pending users via `POST /api/v1/vouches/approve/{id}`
- The pending user list is available via `GET /api/v1/vouches/pending`
- Both endpoints require the `council` role

Bootstrap mode automatically disables itself when the active member count reaches 20. After that, new users must be vouched for by existing members with a trust score >= 60 to be promoted from `pending` to `member`.
