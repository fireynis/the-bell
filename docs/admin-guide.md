# Administration Guide

## Prerequisites

- **Docker** and **Docker Compose** (v2+)
- **PostgreSQL 18** with the [Apache AGE](https://age.apache.org/) graph extension enabled (used for the vouch trust graph)
- **Ory Kratos v1.3.1** for identity and session management
- **Redis 7** (optional but recommended -- enables feed caching, trust score caching, and rate limiting)
- For the root compose only: an external Docker network called `proxy`
  (`docker network create proxy`). The `deploy/` stack creates its own network

## Which compose file

Two stacks live in this repository and they are not interchangeable:

| | `deploy/docker-compose.yml` | `docker-compose.yml` (root) |
|---|---|---|
| For | **Any new town** | The maintainer's `bell.themacarthurs.ca` |
| Network | Private, one published port | External `proxy` network, Traefik labels |
| Storage | Docker volumes | Host paths under `/storage/the-bell` |
| Domain | From `PUBLIC_URL` | `bell.themacarthurs.ca`, hardcoded |

**Setting up a new deployment? Follow [deploy/README.md](../deploy/README.md).**
It is the canonical guide and covers TLS, SMTP, backups and upgrades. The rest
of this document is reference material that applies to both.

## Quick Start

1. Create `deploy/.env` from the template and generate secrets:

```bash
cd deploy
cp .env.example .env
echo "POSTGRES_PASSWORD=$(openssl rand -hex 16)"
echo "KRATOS_SECRETS_COOKIE=$(openssl rand -hex 32)"
echo "KRATOS_SECRETS_CIPHER=$(openssl rand -hex 16)"
```

Paste the three values in, then set `PUBLIC_URL` to the address residents will
type. Compose refuses to start without the two Kratos secrets — see
[Secrets](#secrets) for why they have no defaults.

2. Start all services:

```bash
docker compose up -d --build
```

This brings up six containers (names below are the root compose's; `deploy/`
uses `postgres`, `redis` and `check-roles`):

| Container | Purpose |
|-----------|---------|
| `bell` | The Bell API + SPA (port 8080) |
| `bell-postgres` | PostgreSQL with Apache AGE (pinned image, see below) |
| `kratos` | Ory Kratos identity server (ports 4433/4434) |
| `kratos-migrate` | Runs Kratos DB migrations then exits |
| `redis-bell` | Ephemeral Redis (no persistence, cache only) |
| `bell-check-roles` | Runs `bell check-roles` on a loop (daily by default) |

You do not need to create the databases by hand. On first start the Postgres
container creates `bell` from `POSTGRES_DB` and runs `deploy/init-db.sh`, which
creates `bell_kratos`. If you are pointing The Bell at a pre-existing Postgres
that never ran those init hooks, use `bell setup --create-db` instead — it
derives both names from `DATABASE_URL` (a DSN ending in `/thebell` yields
`thebell` and `thebell_kratos`), so it does not assume the names are `bell` and
`bell_kratos`.

The Postgres image is pinned to `apache/age:release_PG18_1.7.0`. Do not switch
it to `apache/age:latest`: that tag floats across Postgres *major* versions, and
Postgres refuses to start against a data directory written by a different major.
The same pin appears in `deploy/docker-compose.yml` and
`internal/testsupport/testsupport.go`, and all three must stay identical.

3. Bootstrap the town with initial council members:

```bash
docker exec -it bell ./bell setup --council=alice@example.com,bob@example.com
```

`setup` prompts for anything you do not pass as a flag, so run it with `-it` if
you omit `--council` or `--town-name`.

This creates Kratos identities and local users with the `council` role and a trust score of 100. It also enables bootstrap mode, which allows council members to directly approve pending users until 20 active members are reached.

## Secrets

Kratos signs and encrypts session cookies with two secrets, supplied through the
environment as `KRATOS_SECRETS_COOKIE` and `KRATOS_SECRETS_CIPHER` (passed into
the container as `SECRETS_COOKIE` and `SECRETS_CIPHER`). Both compose files
declare them mandatory, so `docker compose up` fails with a message naming the
missing variable rather than starting.

```bash
openssl rand -hex 32   # KRATOS_SECRETS_COOKIE
openssl rand -hex 16   # KRATOS_SECRETS_CIPHER — exactly 32 characters
```

`KRATOS_SECRETS_CIPHER` must be **exactly 32 characters**. Kratos's schema sets
both a minimum and a maximum of 32 and the process exits with `length must be
<= 32, but got 64` otherwise — so `openssl rand -hex 32` is the wrong command
for this one.

They are mandatory rather than defaulted for two reasons. A default committed to
the repository is not a secret at all: anyone with a copy of the source could
mint a valid session cookie for any account, including council members. And an
absent one fails in a way nobody notices — Kratos does not refuse to start, it
generates a random cookie secret at boot, which invalidates every session on
every restart while logging nothing that points at the cause.

Rotating `KRATOS_SECRETS_COOKIE` logs everyone out. Kratos accepts a list of
cookie secrets and verifies against all of them while signing with the first, so
a rotation without mass logout is possible by editing `secrets.cookie` in the
Kratos config directly.

`kratos/kratos.yml` and `deploy/kratos/kratos.yml.tmpl` contain no secrets, and
no value in either file should ever be a credential.

## Email (courier)

Password recovery and email verification are enabled, and both need an SMTP
relay. Without one the messages are generated and fail at send time, which means
a resident who forgets their password has no route back into their account.

```env
COURIER_SMTP_CONNECTION_URI=smtps://user:password@smtp.example.com:465/
COURIER_SMTP_FROM_ADDRESS=noreply@example.com
```

The URI must match `smtp://` or `smtps://`; URL-encode special characters in the
password (`@` becomes `%40`), and append `?disable_starttls=true` for a relay
that does not offer STARTTLS. An **empty** value is rejected by Kratos at
startup, which is why both compose files default it to a placeholder that points
nowhere instead of a blank string.

Kratos runs with `--watch-courier`, which starts the delivery worker in the same
process. Without that flag messages are queued in the database and never sent.
Delivery errors appear in `docker compose logs kratos`.

## Configuration Reference

All configuration is via environment variables. The Bell binary reads them at startup using `github.com/caarlos0/env/v11`.

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `DATABASE_URL` | Yes | -- | PostgreSQL connection string. Example: `postgres://belluser:pass@bell-postgres:5432/bell?sslmode=disable` |
| `KRATOS_PUBLIC_URL` | Yes | -- | Kratos public API URL. Example: `http://kratos:4433` |
| `KRATOS_ADMIN_URL` | Yes | -- | Kratos admin API URL. Example: `http://kratos:4434` |
| `PORT` | No | `8080` | HTTP listen port |
| `REDIS_URL` | No | (empty) | Redis connection string. When set, enables feed caching, trust score background worker, and per-user rate limiting. Example: `redis://redis-bell:6379` |
| `IMAGE_STORAGE_PATH` | No | `/storage/the-bell/images` | Filesystem path for uploaded images |
| `TRUST_SWEEP_INTERVAL` | No | `24h` | How often the in-process trust worker recalculates every active user. A **Go duration** (`24h`, `12h`, `90m`) -- not a number of seconds, unlike `CHECK_ROLES_INTERVAL_SECONDS` below. Zero or negative is refused at startup. Requires `REDIS_URL`; without Redis there is no worker and `bell check-roles` does the recalculating instead. See [Trust recalculation](#trust-recalculation) |
| `TOWN_NAME` | No | `My Town` | Display name for the municipality |
| `BELL_ENV` | No | `production` | Only `dev` or `development` (case-insensitive) select development mode. **Anything else — including unset, empty, or a typo — means production.** In development mode The Bell strips the `Secure` attribute from Kratos session cookies as they pass back through the `/.ory/*` proxy, so login works over plain-HTTP localhost. Never set this on a deployment reachable over the network: it would hand the session cookie to any downgrade attacker |

### Compose variables

These are read from `.env` by Docker Compose, not by the Bell binary. They
configure Postgres, Kratos and the scheduler.

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `POSTGRES_PASSWORD` | Yes | -- | Password for the `belluser` role, used by both the app and Kratos DSNs |
| `KRATOS_SECRETS_COOKIE` | Yes | -- | Session cookie signing secret. `openssl rand -hex 32`. See [Secrets](#secrets) |
| `KRATOS_SECRETS_CIPHER` | Yes | -- | Cookie encryption secret, **exactly 32 characters**. `openssl rand -hex 16` |
| `COURIER_SMTP_CONNECTION_URI` | No | placeholder | SMTP relay for recovery and verification mail. Must start with `smtp://` or `smtps://`; empty is rejected. See [Email](#email-courier) |
| `COURIER_SMTP_FROM_ADDRESS` | No | `noreply@localhost` | From address on outgoing mail (root compose defaults to the maintainer's domain) |
| `CHECK_ROLES_INTERVAL_SECONDS` | No | `86400` | How often the check-roles sidecar runs |
| `TOWN_NAME` | No | `My Town` | Also used as the From name on outgoing mail |
| **`deploy/` only** | | | |
| `PUBLIC_URL` | No | `http://localhost:8080` | The address residents type. Every Kratos URL and the session cookie domain derive from it |
| `BELL_PORT` | No | `8080` | Host port to publish. Must agree with `PUBLIC_URL` |
| `KRATOS_EXTRA_ARGS` | No | `--dev` | Extra Kratos flags. **Set to empty for any public deployment** — `--dev` issues non-`Secure` cookies |

### Startup Validation

Configuration is validated before the server starts, and **the process refuses
to boot** rather than starting in a broken state. A missing `DATABASE_URL`,
`KRATOS_PUBLIC_URL` or `KRATOS_ADMIN_URL` fails immediately; so does a value
that is present but unusable. The messages name the variable:

| Message | Cause |
|---------|-------|
| `PORT must be between 1 and 65535, got 0` | `PORT` outside the valid range. Port 0 is legal to the kernel — it asks for any free port — which is why it is rejected explicitly |
| `KRATOS_PUBLIC_URL must be an absolute URL with a scheme and host, got "kratos:4433"` | A URL without a scheme. Use `http://kratos:4433` |
| `KRATOS_ADMIN_URL must be an absolute URL with a scheme and host, got "kratos:4434"` | The same check on the admin URL. Use `http://kratos:4434` |
| `DATABASE_URL must not be empty` | Unset or blank. Checked before parsing, because an empty DSN would otherwise silently fall back to libpq defaults and connect to a local socket as the current user |
| `DATABASE_URL is not a usable postgres DSN: ...` | Rejected by the same parser the connection pool uses, so this cannot disagree with what happens at connect time |
| `REDIS_URL is not a usable redis URL: ...` | Only checked when set — Redis is optional, and running without it is a supported degraded mode |
| `IMAGE_STORAGE_PATH must not be empty` | Blank. The storage root would otherwise resolve against the process working directory |
| `IMAGE_STORAGE_PATH must be an absolute path, got "images"` | A relative path resolves against the process working directory, which differs between a container and a local run |
| `IMAGE_STORAGE_PATH must not be a system directory, got "/etc"` | `/uploads/*` serves this directory, so pointing it at a system tree would publish that tree |

Failing at startup is deliberate. `KRATOS_PUBLIC_URL` is the clearest case: it
is the browser's only route to Kratos, so a bad value would produce an instance
that serves the SPA and answers `/healthz` while nobody can log in — a green
health check on an unusable product.

#### Every problem is reported at once

Validation does not stop at the first failure. Every check runs and the failures
are joined into one error, printed one per line, each still naming its variable:

```
PORT must be between 1 and 65535, got 0
KRATOS_PUBLIC_URL must be an absolute URL with a scheme and host, got "kratos:4433"
IMAGE_STORAGE_PATH must be an absolute path, got "images"
```

A `.env` copied from the wrong environment is usually wrong in several places at
once. Reporting one problem per restart would make you discover them one deploy
at a time, each fix revealing the next. Read the whole list as the set of things
to fix before trying again.

The one deliberate exception is `DATABASE_URL`, whose two checks are mutually
exclusive: an empty value has one thing wrong with it, and saying so twice would
be noise rather than information.

## CLI Commands

The `bell` binary provides four commands:

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

Evaluates all active users for automatic role promotion and demotion.

Both compose files run this on a loop in a sidecar container
(`bell-check-roles` in the root compose, `check-roles` in `deploy/`), daily by
default and tunable with `CHECK_ROLES_INTERVAL_SECONDS`. No host cron is needed.
The sidecar runs one pass at startup and keeps going after a failed run, so a
brief database outage does not silently end role checking.

Promotion and demotion happen **only** when this command runs. If you remove the
sidecar, the criteria below are documented but never applied.

The command also recomputes each user's trust score before judging them by it,
which matters most where there is no Redis. The trust worker that normally
recalculates scores is Redis-backed, so on a deployment without Redis
`check-roles` is the **only** thing that recalculates trust at all: moderation
penalties decay and tenure and activity accrue on whatever schedule you run it.
Stretch the interval to a week and penalties linger roughly seven times longer
than the moderator who applied them intended. Daily is the right cadence, and it
is not only about promotions.

With Redis, the in-process trust worker sweeps every active user on its own, so
`check-roles` usually finds the score already current. See
[Trust recalculation](#trust-recalculation) for that sweep's interval.

Run a pass immediately:

```bash
docker compose exec bell ./bell check-roles
```

### `bell backfill-display-names`

Fills in display names for users who were provisioned before The Bell started
reading the Kratos `name` trait at sign-in. Those accounts have an empty
display name and no way to acquire one except the user editing their own
profile — which matters most in the council's approval queue, where a pending
user shows up as an ID and nothing else.

```bash
docker compose exec bell ./bell backfill-display-names --dry-run
docker compose exec bell ./bell backfill-display-names
```

Run it once after upgrading. It is safe to re-run: a user who already has a
display name is skipped, never overwritten — including a name they chose
themselves in-app, which always wins over the trait. `--dry-run` reports the
same counts and writes nothing.

The command reports users scanned, updated, already named, no name trait, and
errors. A user whose identity Kratos will not return, or whose write fails, is
logged and counted but does not end the run; the exit code is non-zero if any
user failed, so a scripted run can tell. Kratos allows a longer name than The
Bell does (100 characters), so an over-long trait is truncated and the
truncation noted in the output.

There is no sidecar for this one — it is a one-time repair, not a schedule.

Check the schedule is alive:

```bash
docker compose logs bell-check-roles
```

Promotion criteria (member to moderator):
- Trust score >= 85
- Member for >= 90 days
- At least 2 vouches from moderators or council members

Demotion criteria -- the threshold depends on the role, and demotion needs
**30 consecutive days** below it:

| Role | Threshold | Demoted to |
|------|-----------|------------|
| Moderator | Trust < 70 | member |
| Member | Trust < 35 | pending |
| Council | exempt | -- |

Recovering above the threshold at any point resets the 30-day clock, so one bad
month cannot cost someone a role.

The two numbers are far apart because they answer different questions. 70 sits
15 points under the 85 promotion bar: a moderator is expected to keep clearly
above-average standing, and an engaged member in good standing scores around 68,
so a moderator below 70 has slipped under the people they moderate. 35 is the
line between a quiet resident and collapsed trust: a member who reads more than
they post, has two vouches and has never been sanctioned scores about 50, and
one serving a fresh suspension still scores about 38. Crossing 35 takes more
than 50 points of live penalty -- typically the fallout of having vouched for
someone since banned, on top of a sanction of their own.

These thresholds were a flat 70 for every role, which was survivable only while
scores were rarely recalculated. Once the sweep and `check-roles` began
converging scores on their real values, a flat 70 would have demoted most
healthy members within the 30-day window, and would have cascaded a demoted
moderator straight on to pending. The derivation, with the arithmetic, is in
`internal/domain/user.go` above `MemberDemotionTrustThreshold`.

### Trust recalculation

A user's trust score is recomputed from tenure, activity, vouches and active
moderation penalties. Two things trigger it:

- **The trust worker**, in-process and Redis-backed. It recalculates a single
  user whenever something happens to them (a moderation penalty, a vouch
  change), and sweeps *every* active user on a fixed interval so that penalties
  decay and tenure accrues for people nothing is happening to. It sweeps once at
  startup and then every `TRUST_SWEEP_INTERVAL` -- default `24h`, expressed as a
  Go duration.
- **`bell check-roles`**, which refreshes each user's score before judging it.

Daily suits inputs that move with the calendar: the shortest penalty decay
window is 90 days and tenure resolves to a day, so shortening the interval
mostly buys work. Lengthening it is the change with consequences -- penalties
linger past the point the moderator who applied them intended, and role checks
judge scores that are up to one interval stale.

Without `REDIS_URL` there is no worker at all, and `check-roles` is the only
thing that recalculates anything; `TRUST_SWEEP_INTERVAL` has no effect on such a
deployment.

If you would rather schedule it on the host, delete the sidecar service first —
two schedulers would run every check twice:

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

Kratos is never exposed directly. The browser reaches it through The Bell's
`/.ory/*` proxy, so the reverse proxy needs one rule for one upstream — but
Kratos still has to know the public address, because it stamps that base into the
flow `action` URLs it hands the SPA.

In `deploy/`, that is handled by `PUBLIC_URL`: at container start the `kratos`
service derives the hostname from it and renders
`deploy/kratos/kratos.yml.tmpl` into `/tmp/kratos.yml`, filling in roughly ten
URLs plus `cookies.domain`. Edit the template, not the rendered copy, and check
what was loaded with:

```bash
docker compose exec kratos cat /tmp/kratos.yml
```

The root compose's `kratos/kratos.yml` is not templated — it is the maintainer's
deployment and has `bell.themacarthurs.ca` written into it directly.

The cookie domain is the setting most worth getting right. If it does not match
the host the browser used, the session cookie is discarded and login appears to
succeed while leaving the user signed out — with no error on either side.

## Backup Considerations

Data to back up:

- **PostgreSQL databases**: `bell` (application data) and `bell_kratos` (identity data)
- **Image uploads**: the directory configured by `IMAGE_STORAGE_PATH` (default `/storage/the-bell/images`)

Redis does not need backup -- it is configured as an ephemeral cache with no persistence (`--save "" --appendonly no`).

Example pg_dump:

```bash
pg_dump -h bell-postgres -U belluser bell > bell_backup.sql
pg_dump -h bell-postgres -U belluser bell_kratos > bell_kratos_backup.sql
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
| `bell-check-roles` | 128 MB | 0.25 |

### Logging

The Bell outputs structured JSON logs to stdout via `slog.JSONHandler`. These can be collected by any Docker log driver (e.g., Loki via Alloy, Fluentd, etc.).

## Bootstrap Mode

Bootstrap mode is the initial phase of a new town deployment. During bootstrap mode:

- Council members can directly approve pending users via `POST /api/v1/vouches/approve/{id}`
- The pending user list is available via `GET /api/v1/vouches/pending`
- Both endpoints require the `council` role

Bootstrap mode automatically disables itself when the active member count reaches 20. After that, new users must be vouched for by existing members with a trust score >= 60 to be promoted from `pending` to `member`.
