# Deploying The Bell for a new town

This directory is the self-contained stack: PostgreSQL with Apache AGE, Ory
Kratos, Redis, the application, and a scheduler for role checks. Everything runs
on a private Docker network and the app is published on one host port.

Use this, not the `docker-compose.yml` in the repository root. That one is the
maintainer's own deployment of `bell.themacarthurs.ca` — it hardcodes that
hostname in Traefik labels and expects an external `proxy` network and host
directories under `/storage/the-bell`.

## Prerequisites

- Docker and Docker Compose v2
- A machine with ~1 GB of RAM free (the containers are capped at ~1.2 GB total)
- For a public deployment: a domain name and a TLS-terminating reverse proxy

No PostgreSQL, Redis or Kratos installation is needed — the stack brings its own,
and both databases are created on first start.

## 1. Configure

```bash
cd deploy
cp .env.example .env
```

Generate the secrets and the database password:

```bash
echo "POSTGRES_PASSWORD=$(openssl rand -hex 16)"
echo "KRATOS_SECRETS_COOKIE=$(openssl rand -hex 32)"
echo "KRATOS_SECRETS_CIPHER=$(openssl rand -hex 16)"
```

Paste the three values into `.env`.

`KRATOS_SECRETS_CIPHER` must be **exactly 32 characters**. Kratos enforces a
minimum and a maximum of 32 and refuses to start otherwise, so `openssl rand
-hex 32` (which prints 64 characters) is rejected — use `-hex 16` as above.

Neither secret has a default. That is deliberate: a secret checked into a
repository is a secret everybody has, and an absent one is worse than it looks.
Kratos does not fail when the cookie secret is missing — it silently generates a
random one at every boot, so each restart invalidates every session and nothing
in the logs explains why residents keep getting logged out. Compose refuses to
start until both are set.

Then set `PUBLIC_URL` to the address residents will type. Every Kratos URL —
login and registration pages, post-login redirects, CORS origins, and the
session cookie domain — is derived from it when the container starts.

Getting it wrong has a distinctive symptom: a cookie scoped to the wrong host is
discarded by the browser, so login appears to succeed and then leaves the person
signed out, with no error anywhere. If `PUBLIC_URL` matches the address in the
address bar, this cannot happen.

## 2. Start

```bash
docker compose up -d --build
```

The first run builds the image, creates the `bell` and `bell_kratos` databases,
and applies migrations. Watch it come up with `docker compose logs -f bell`.

## 3. Bootstrap the town

```bash
docker compose exec -it bell ./bell setup --council=mayor@example.com,clerk@example.com
```

This creates the founding council: a Kratos identity and a local user with the
`council` role and a trust score of 100 for each address, and it enables
bootstrap mode so the council can approve residents directly until the town
reaches 20 active members.

`setup` prompts for anything you leave out, so keep `-it` if you omit
`--council` or `--town-name`. It runs once; it refuses to run again once
bootstrap mode is enabled.

Council members sign in through the normal registration flow using those email
addresses.

The site is now at whatever you set as `PUBLIC_URL`.

### How residents join

**A new town is invitation-only.** Nobody can create an account without a live
invitation: a member invites somebody by email, and accepting that invitation
also records the vouch that makes them a member. The founding council can invite
without limit, which is how a town gets populated in the first place; ordinary
members get three endorsements a day, invitations and vouches combined.

Invitations are emailed, so set up a relay before you start inviting — see
[Email](#email) below, and set `PUBLIC_URL` to the address residents actually
type, since that is what invitation links are built on. Without a relay the
invitation still works and the API hands the member a link to pass on by hand.

If your town would rather let anybody sign up and be vouched for afterwards, the
council can switch registration to open mode from the admin screen. See
"Registration modes" in [docs/admin-guide.md](../docs/admin-guide.md). Existing
towns upgrading into this release are switched to invitation-only, deliberately;
open sign-up has to be chosen.

## Going to production: TLS

The defaults in `.env.example` are for a plain-HTTP trial on `localhost`. They
run Kratos with `--dev` and The Bell with `BELL_ENV=dev`, which together issue
session cookies **without** the `Secure` attribute — that is the only way login
works without TLS, and it is not safe on a network, where anyone able to observe
or downgrade a connection can lift a session cookie and become that user.

For a real town:

1. Point a DNS record at the host.
2. Put a TLS-terminating reverse proxy in front (examples below).
3. In `.env`, set the public URL and turn off both development modes:

```env
PUBLIC_URL=https://bell.example.com
KRATOS_EXTRA_ARGS=
BELL_ENV=
```

4. `docker compose up -d` to apply.

Leaving either development flag set on a public domain is the one configuration
mistake here with a real security cost. Everything else fails loudly; this one
works fine and quietly hands out interceptable sessions.

The app serves the API, the SPA, and the `/.ory/*` Kratos proxy on a single
port, so the proxy needs one rule for one upstream. Kratos itself is never
exposed — the browser only ever talks to The Bell.

### Caddy

Caddy obtains and renews certificates automatically:

```caddy
bell.example.com {
    reverse_proxy localhost:8080
}
```

### Traefik

With `BELL_PORT` published on the host and a `websecure` entrypoint plus a
configured certificate resolver:

```yaml
labels:
  - "traefik.enable=true"
  - "traefik.http.routers.bell.rule=Host(`bell.example.com`)"
  - "traefik.http.routers.bell.entrypoints=websecure"
  - "traefik.http.routers.bell.tls.certresolver=myresolver"
  - "traefik.http.services.bell.loadbalancer.server.port=8080"
```

Add these to the `bell` service and attach it to Traefik's network. If Traefik
reaches the container directly over the Docker network, drop the `ports:` entry
so the app is not also reachable over plain HTTP.

### nginx

```nginx
server {
    listen 443 ssl;
    server_name bell.example.com;

    ssl_certificate     /etc/letsencrypt/live/bell.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/bell.example.com/privkey.pem;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

Server-sent events power the live feed, so disable response buffering for it
(`proxy_buffering off;`) if posts are slow to appear.

## Email

Three things send mail: password recovery, email verification, and
**invitations**. The first two are Kratos's; invitations are The Bell's own. One
setting covers all three — the compose file passes
`COURIER_SMTP_CONNECTION_URI` and `COURIER_SMTP_FROM_ADDRESS` through to the app
as `SMTP_CONNECTION_URI` and `SMTP_FROM_ADDRESS`.

Until the URI points at a real relay, recovery and verification messages are
generated and then fail to send, and a resident who forgets their password has
no way back into their account. Invitations behave differently: with no relay
the invitation is still created and works, the response says
`email_sent: false`, and the member passes the link on themselves.

```env
COURIER_SMTP_CONNECTION_URI=smtps://user:password@smtp.example.com:465/
COURIER_SMTP_FROM_ADDRESS=noreply@example.com
```

The URI must begin with `smtp://` or `smtps://`, and special characters in the
password must be URL-encoded (`@` becomes `%40`). For a relay that does not
offer STARTTLS, append `?disable_starttls=true` — The Bell refuses to send
invitations in the clear to a relay that does not offer STARTTLS unless you say
so explicitly. An empty value is rejected by Kratos at startup, which is why the
compose default is a placeholder that points nowhere rather than a blank.

Set `PUBLIC_URL` too, or emailed invitation links will be relative paths that
mean nothing in an inbox.

Delivery failures are logged by whichever side sent the message:

```bash
docker compose logs kratos | grep -i courier   # recovery, verification
docker compose logs bell | grep -i invitation  # invitations
```

### Testing email with MailHog

While you are trying the stack out — before any real residents exist — you can
catch all outgoing mail instead of sending it. A [MailHog](https://github.com/mailhog/MailHog)
service ships in the compose file behind the `mailtest` profile. Enable it with
two lines in `.env`:

```env
COMPOSE_PROFILES=mailtest
COURIER_SMTP_CONNECTION_URI=smtp://mailhog:1025/?disable_starttls=true
```

Then `docker compose up -d` and open `http://localhost:8025` (tunable with
`MAILHOG_UI_PORT`). Every recovery, verification **and invitation** message the
town sends lands there instead of a real inbox, which makes all three testable
end-to-end with no relay.

Invitations are the flow most worth trying this way, because they are the one
you cannot check by reading logs: invite yourself at a second address, open the
message in MailHog, follow the link, and confirm you come out the other side as
a member rather than as a pending applicant.

**Never run this profile on a town with real residents.** The MailHog inbox is
unauthenticated and holds live recovery links and live invitation links for
every account on the instance — either one is enough to take over an account or
walk into the town. Before going live: remove both lines, set a real
`COURIER_SMTP_CONNECTION_URI`, and `docker compose up -d` again — the mailhog
container is removed with the profile.

## What each service does

| Service | Purpose |
|---------|---------|
| `bell` | API, SPA, and the `/.ory/*` proxy to Kratos — the only published port |
| `postgres` | PostgreSQL 18 with Apache AGE, holding `bell` and `bell_kratos` |
| `kratos` | Identity, sessions, recovery, verification; not exposed |
| `kratos-migrate` | Applies Kratos schema migrations, then exits |
| `redis` | Ephemeral cache: feed cache, trust scores, rate limiting |
| `check-roles` | Runs `bell check-roles` on a loop, daily by default |

`check-roles` exists because moderator promotion and demotion for sustained low
trust happen only when that command runs. Without a scheduler those rules are
documented but never applied.

It also recomputes trust scores as it goes, so its cadence decides how quickly
moderation penalties decay and how quickly tenure and activity accrue. This
stack ships with Redis, whose trust worker sweeps every active user every 24
hours independently — but if you run The Bell without Redis, `check-roles` is
the only thing that recalculates trust at all, and its interval becomes the rate
at which the whole trust system moves. Daily is the intended cadence.

Change it with `CHECK_ROLES_INTERVAL_SECONDS`, or run a pass by hand at any
time:

```bash
docker compose exec bell ./bell check-roles
```

## Kratos configuration

`kratos/kratos.yml.tmpl` is a template, not a config file. At container start
the `kratos` service derives the hostname from `PUBLIC_URL`, substitutes both
into a copy at `/tmp/kratos.yml`, and starts Kratos against that.

This is why one variable is enough for roughly ten URLs plus the cookie domain.
Edit the template; the rendered copy is disposable. To inspect what Kratos
actually loaded:

```bash
docker compose exec kratos cat /tmp/kratos.yml
```

Kratos also reads `SECRETS_COOKIE`, `SECRETS_CIPHER` and
`COURIER_SMTP_CONNECTION_URI` from the environment, which is why they appear
nowhere in the template.

## Upgrading

```bash
git pull
docker compose up -d --build
```

Application migrations run automatically at startup, and `kratos-migrate`
handles the identity schema. Take a backup first.

The PostgreSQL image is pinned to `apache/age:release_PG18_1.7.0`. Do not change
it to `apache/age:latest`: that tag moves across PostgreSQL *major* versions,
and PostgreSQL refuses to start against a data directory written by a different
major, which breaks an existing deployment on restart.

## Backups

Two databases and the uploaded images:

```bash
docker compose exec postgres pg_dump -U belluser bell        > bell.sql
docker compose exec postgres pg_dump -U belluser bell_kratos > bell_kratos.sql
docker run --rm -v deploy_bell_images:/data -v "$PWD:/backup" \
  alpine tar czf /backup/bell-images.tar.gz -C /data .
```

`bell_kratos` holds the credentials — a backup of `bell` alone restores a town
whose residents cannot log in. Redis needs no backup; it runs without
persistence and rebuilds from PostgreSQL.

## Troubleshooting

**Compose exits with `required variable KRATOS_SECRETS_COOKIE is missing a
value`** — the secrets are not set in `.env`. Generate them as in step 1.

**Kratos exits with `length must be <= 32`** — `KRATOS_SECRETS_CIPHER` is not
exactly 32 characters. Use `openssl rand -hex 16`.

**Login seems to work but the resident is still signed out** — `PUBLIC_URL` does
not match the address in the browser, so the session cookie is being dropped.
Compare it with `docker compose exec kratos cat /tmp/kratos.yml`.

**Login fails over plain HTTP** — cookies are being marked `Secure` without TLS.
Either serve over HTTPS (correct) or set `KRATOS_EXTRA_ARGS=--dev` and
`BELL_ENV=dev` for a local trial.

**Recovery email never arrives** — `COURIER_SMTP_CONNECTION_URI` is still the
placeholder. See [Email](#email).

**Everyone is logged out after a restart** — the cookie secret is changing
between boots. Confirm `KRATOS_SECRETS_COOKIE` is set and stable in `.env`.

**`bell` restarts repeatedly** — check `docker compose logs bell`. Configuration
is validated at startup and every problem is reported at once, each naming its
variable.
