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

Password recovery, email verification and **invitations** all need an SMTP
relay. The first two are Kratos's; invitations are The Bell's own. Both read the
same two variables — the compose files pass `COURIER_SMTP_CONNECTION_URI` and
`COURIER_SMTP_FROM_ADDRESS` through to the app as `SMTP_CONNECTION_URI` and
`SMTP_FROM_ADDRESS` — so a relay is configured once and serves everything.

Without a relay, recovery and verification are generated and fail at send time,
which means a resident who forgets their password has no route back into their
account. Invitations degrade instead: the invitation is still created and works,
the API answers `email_sent: false` with a reason, and the member sends the link
themselves. A town can run invite-only with no relay at all — it is just more
manual.

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

Invitation mail is sent by the app rather than the courier, so it has no queue
and no worker: it goes out synchronously while the member waits, with a ten
second budget, and a failure is reported in that member's response rather than
retried. Its errors appear in `docker compose logs bell`.

STARTTLS is **required** on a plain `smtp://` URI unless you append
`?disable_starttls=true`. The app refuses to send in the clear to a relay that
does not offer it, rather than falling back silently — an invitation carries a
token that admits its holder to the town.

To read an invitation end-to-end before pointing at a real relay, use the
MailHog profile (`COMPOSE_PROFILES=mailtest` plus
`COURIER_SMTP_CONNECTION_URI=smtp://mailhog:1025/?disable_starttls=true`) and
browse the captured mail at port 8025. Never on a town with real residents: it
holds live invitation and recovery links for every account.

### Requiring verified email

By default a resident can participate as soon as they register and the council
approves them, whether or not they ever opened the verification mail. Setting
`REQUIRE_VERIFIED_EMAIL=true` changes that: a resident whose Kratos identity has
no verified address gets `403 {"error": "email not verified"}` from every
endpoint except `GET /api/v1/me`, which stays reachable so the frontend can tell
them why and point them at the verification flow.

**Do not enable this before the courier works.** The verification message is the
only way to clear the flag, so on a town with no SMTP relay — or one whose relay
is misconfigured — turning it on locks out every resident, including the council
members who would have to turn it back off. The order is: configure
`COURIER_SMTP_CONNECTION_URI`, confirm a real verification mail arrives, then
set the flag.

Two more things worth knowing before flipping it:

- **Residents who registered earlier are affected.** Verification state is read
  from Kratos on every request, so anyone who skipped the mail when they signed
  up is blocked from their next request onward, not from their next
  registration. Expect support questions from existing residents on the day you
  enable it.
- **An identity schema with no verifiable addresses counts as unverified.** This
  is deliberate — "we cannot ask" is not evidence of a confirmed address — but
  it means a customised `kratos/identity.schema.json` that drops the
  `verification` block turns the flag into a total lockout.

## Trusted proxies

`TRUSTED_PROXIES` tells The Bell which peers may speak for somebody else. It
matters for exactly one thing today: the registration rate limit, which is keyed
by client IP because registration is unauthenticated and there is no user to key
on.

Left empty — the default — every request is attributed to the address the TCP
connection came from. Reached directly that is the resident; **behind a reverse
proxy it is the proxy**, so the whole town shares one registration budget and
the eleventh sign-up in an hour is refused no matter who is trying. The Traefik
deployment described under [Reverse Proxy Setup](#reverse-proxy-setup) is
exactly that case.

Set it to the network your proxy connects from:

```env
# The Docker network Traefik reaches the app on
TRUSTED_PROXIES=172.18.0.0/16
```

Both plain addresses and CIDR blocks are accepted, comma-separated
(`10.0.0.7,10.1.0.0/16`). A value that does not parse stops the process at
startup rather than being silently ignored.

Both compose files pass `TRUSTED_PROXIES` and `REQUIRE_VERIFIED_EMAIL` through
to the `bell` service, defaulting to the safe values (empty, and `false`).

The default is empty rather than "the usual private ranges" because the wrong
direction of error is unbounded. `X-Forwarded-For` is an ordinary request
header: if the app is reachable without going through the proxy and this is
filled in optimistically, any caller can write their own value and mint a fresh
rate-limit bucket per request, which removes the limit entirely. Trusting only
the network you actually run means a spoofed header is ignored, and the cost of
getting it wrong in the other direction is a shared bucket rather than none.

When the peer is trusted, the client is taken from the right-hand end of the
`X-Forwarded-For` chain — the end a proxy appends to and a caller cannot forge —
skipping any further trusted proxies. Addresses to the left of that hop are
whatever the caller claimed and are ignored.

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
| `TRUSTED_PROXIES` | No | (empty) | Comma-separated IP addresses and CIDR blocks whose `X-Forwarded-For` header is believed when attributing a request to a client IP. **Set this if you run behind a reverse proxy** — see [Trusted proxies](#trusted-proxies). Example: `172.18.0.0/16` |
| `REQUIRE_VERIFIED_EMAIL` | No | `false` | When `true`, a resident whose Kratos identity has no verified address may sign in and read their own status but cannot participate. **Requires a working SMTP relay** — see [Requiring verified email](#requiring-verified-email) before enabling |
| `PUBLIC_URL` | No | (empty) | The address residents type, and the only thing that tells the app its own public URL. Invitation links are built on it. Empty means invitation links come back as site-relative paths, which the app absolutizes and an inbox cannot — set it if invitations are emailed. Must be absolute (scheme and host) if set |
| `SMTP_CONNECTION_URI` | No | (empty) | Relay for **invitation** mail, in the same courier shape Kratos uses. Both composes feed it from `COURIER_SMTP_CONNECTION_URI`. Empty means invitation email is off and invitations still work — see [Email](#email-courier) |
| `SMTP_FROM_ADDRESS` | No | (empty) | From address on invitation mail. **Required when `SMTP_CONNECTION_URI` is set**, and checked at startup: a relay with nobody to send as fails at `MAIL FROM`, which would otherwise be discovered by the first member who tried to invite somebody |

### Compose variables

These are read from `.env` by Docker Compose, not by the Bell binary. They
configure Postgres, Kratos and the scheduler.

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `POSTGRES_PASSWORD` | Yes | -- | Password for the `belluser` role, used by both the app and Kratos DSNs |
| `KRATOS_SECRETS_COOKIE` | Yes | -- | Session cookie signing secret. `openssl rand -hex 32`. See [Secrets](#secrets) |
| `KRATOS_SECRETS_CIPHER` | Yes | -- | Cookie encryption secret, **exactly 32 characters**. `openssl rand -hex 16` |
| `COURIER_SMTP_CONNECTION_URI` | No | placeholder | SMTP relay for recovery and verification mail, **and passed through to the app as `SMTP_CONNECTION_URI` for invitations**. Must start with `smtp://` or `smtps://`; empty is rejected by Kratos (the app treats empty as "sending off"). See [Email](#email-courier) |
| `COURIER_SMTP_FROM_ADDRESS` | No | `noreply@localhost` | From address on outgoing mail, Kratos's and the app's alike (root compose defaults to the maintainer's domain) |
| `CHECK_ROLES_INTERVAL_SECONDS` | No | `86400` | How often the check-roles sidecar runs |
| `TOWN_NAME` | No | `My Town` | Also used as the From name on outgoing mail |
| `PUBLIC_URL` | No | see note | The address residents type. In `deploy/` every Kratos URL and the session cookie domain derive from it, and in both composes it is passed to the app for invitation links. Defaults to `http://localhost:8080` in `deploy/` and to the Traefik hostname in the root compose |
| **`deploy/` only** | | | |
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

## Registration modes

A town admits new residents in one of two ways, and it is town configuration
rather than an environment variable — the council changes it from the admin
screen, or with `PUT /api/v1/admin/config`, because it is a decision the town
makes rather than one the operator makes at deploy time.

| `registration_mode` | What happens |
|---------------------|--------------|
| `invite` (default) | Nobody can create an account without a live invitation. A member invites somebody by email; accepting makes the newcomer a member immediately. |
| `open` | Anybody may create an account and then wait to be vouched for or approved. The original behaviour. |

```bash
# Switch a town to open sign-up
curl -X PUT https://bell.example.org/api/v1/admin/config \
  -H 'Content-Type: application/json' \
  -b "$SESSION_COOKIE" \
  -d '{"registration_mode": "open"}'
```

Only `invite` and `open` are accepted, exactly and case-sensitively. That
constraint matters: the registration gate treats anything that is not `open` as
invite-only, so a typo would close the town silently rather than fail.

**Existing towns are switched to `invite` when they upgrade.** Migration 00023
seeds the setting, and it seeds `invite` — deliberately, because the alternative
(defaulting an upgrading town to `open`) leaves a town that upgraded *for* this
feature still accepting strangers until somebody noticed. If your town wants
open sign-up, set it back after upgrading.

### What invite mode changes

- The three Kratos registration paths behind `/.ory/*` require a live invitation
  in the `bell_invite` cookie, and on the submit the address being registered
  must match the invited one. Everything else under `/.ory` — login, sessions,
  recovery, settings — is untouched, so existing residents notice nothing.
- Accepting an invitation **creates the vouch**, so the newcomer lands as a
  member. There is no approval queue step for them.
- The council's approval queue and ordinary vouching both keep working. An
  invited person whose inviter has since been suspended or demoted arrives
  pending, and those paths are how they get in.

### Who can invite, and how many

Anybody who could vouch can invite: an active member, not pending or banned,
with trust >= 60. Council members always qualify.

Invitations and vouches share **one allowance of three per day** — they are the
same act, and an invitation is simply a vouch made in advance. Two invitations
and one vouch spends it.

**Council members are exempt from that allowance.** This mirrors the decision
that leaves council approvals unlimited, and in invite mode it is load-bearing:
populating a new invite-only town means the council inviting everybody they
know, and three a day would make standing a town up take a fortnight. A
sliding-window backstop of 30 invitations per day still applies to everyone,
against a compromised session rather than against ordinary use.

Accepting an invitation does not spend the inviter's allowance a second time.
It was charged when the invitation was sent, and the invitee chooses when to
accept.

### Practical notes

- Invitations expire after **14 days**. Nothing needs to run for that to happen.
- One live invitation per address at a time. A mistyped address is recoverable:
  revoke the invitation, or wait out the expiry, then send a new one.
- The invitation link is shown to the member once, in the response that creates
  it, and is emailed. It is never readable again — only a hash is stored — so a
  member who loses it revokes the invitation and sends another.
- Set `PUBLIC_URL` if invitations are emailed. Without it the API returns
  site-relative links, which the app handles fine but an inbox does not.
- A member can see and withdraw their own invitations, and nobody else's.
  There is no town-wide list of outstanding invitations, for moderators or for
  council.

## Bootstrap Mode

Bootstrap mode is the initial phase of a new town deployment. During bootstrap mode:

- Council members can directly approve pending users via `POST /api/v1/vouches/approve/{id}`
- The pending user list is available via `GET /api/v1/vouches/pending`, paged 25
  at a time and searchable by name, longest wait first
- In the app it is the **Approvals** page at `/admin/approvals`, reached from the
  Town Hall dashboard, which keeps only a count and the longest-waiting few. The
  queue has a page of its own because a town launch or a registration flood puts
  fifty applicants in it at once
- Both endpoints require the `council` role

Bootstrap mode automatically disables itself when the active member count reaches 20. After that, new users must be vouched for by existing members with a trust score >= 60 to be promoted from `pending` to `member`.

It is no longer a one-way door. The council can vote the town back into
bootstrap mode with a `bootstrap_reentry` proposal — see
[The Town Hall](#the-town-hall) — which matters for a town that grew past 20 and
then shrank, and had otherwise lost the only mechanism that admits residents
without a vouch. The auto-exit at 20 is unchanged and still applies afterwards,
so the proposal is refused while the town is at or above that count; it would
otherwise pass and be undone by the next approval.

### Reviewing residency claims

A pending resident can state where they live, in their own words, and that claim
appears against them in the approval queue as `residency_claim`. It is optional
for them and it is up to 300 characters of free text.

**The Bell does not verify it, and cannot.** There is no address service behind
this field. What the platform records is that the applicant said X and that a
particular council member approved them afterwards — the approval is already
attributed, so the pair is the audit trail. That is a real record, and it is a
different thing from a verified address; do not let a filled-in field read as a
checked one.

How hard to press on a claim is your town's decision, and deliberately so. A
village where the council recognises every street may treat a plausible address
as sufficient on its own. A commuter suburb where nobody knows their neighbours
should probably treat it as one signal among the vouches, and may want a second
one — a phone call, a known neighbour asked to vouch, a wait until somebody
does. The platform holds the claim and the approval; the standard is yours.

An empty claim is not a red flag by itself. Residents who registered before this
field existed have none, and a claim can be cleared at any time.

The claim is shown in the approval queue and on the applicant's own profile,
where they can edit it, and nowhere else. It is not on public profiles or in the
member directory, so council members should treat what they read in the queue as
they would anything else told to them in confidence.

## The Town Hall

The council changes its own membership by vote, from the Town Hall on the admin
dashboard. Any council member can raise a proposal; a simple majority of the
electorate carries it, and **a proposal that carries takes effect immediately**
— there is no separate step where an administrator applies the outcome, and
nothing for you to do afterwards.

| Proposal | Requires | Effect on passing |
|----------|----------|-------------------|
| `council_promotion` | Target is an active moderator | Target's role becomes `council` |
| `council_removal` | Target is on the council; council has more than one member | Target's role becomes `member` |
| `bootstrap_reentry` | No target; town out of bootstrap mode and below 20 active members | `bootstrap_mode` set to `true` |

On a removal the target neither votes nor counts towards the majority, which is
taken over the rest of the council. Every other proposal is decided by the whole
council.

Role changes made this way write a `role_history` row like every other role
change, naming the proposal, so `role_history` remains the complete record of
how somebody came to hold the role they hold.

Two operational notes:

- **There is no CLI equivalent and no override.** Council membership after setup
  is decided by the council, not by whoever holds the shell. `bell setup` seats
  the initial council and does not run again on a bootstrapped town.
- **A proposal can outlive the situation it was raised in.** If the target is no
  longer eligible when the vote completes — a moderator demoted by
  `bell check-roles` in the meantime, a council member who already left — the
  proposal is recorded as `rejected` rather than `passed`, because nothing
  happened. If you see a rejected proposal that clearly had the votes, that is
  what it means; the rationale and the target's `role_history` will show why.
