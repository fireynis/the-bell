# The Bell — Design Document

**Date:** 2026-02-28
**Status:** Approved

## Overview

The Bell is a self-hosted micro-blogging platform for municipalities. It replaces the historical tradition of ringing a town bell for good news, marriages, and important announcements with a simple, text-first digital platform.

What makes it unique is the **trust-based social reputation system**. Each town starts with a small council of admins. As community members participate positively, they gain influence and can become moderators or council members. Misbehavior costs trust — and critically, the people who vouched for you also feel the impact, creating a strong incentive to only invite trustworthy community members.

The platform is designed so that any municipality can deploy their own instance with a single `docker compose up`.

## Tech Stack

| Layer | Technology | Purpose |
|-------|-----------|---------|
| API | Go 1.24+ (chi router) | REST API serving web + future mobile apps |
| Auth/Identity | Ory Kratos | Registration, login, sessions, recovery, MFA |
| Relational DB | PostgreSQL 17 + Apache AGE extension | Users, posts, moderation, config + graph queries for trust |
| Cache | Redis | Feed caching, rate limiting, session caching |
| Migrations | goose | Versioned SQL migrations |
| DB Access | sqlc + pgx | Type-safe SQL generation, native Postgres driver |
| Object Storage | Local filesystem (S3-compatible later) | Post images |
| Frontend | React (Vite + TypeScript) | SPA consuming the API |
| Deployment | Docker Compose | Single command for municipalities |

**Key principle:** Kratos owns identity ("who are you?"), the app owns authorization and trust ("what can you do?").

## Data Model

### Core Entities

**Town** — the instance itself, configured at deployment.
- Name, description, location, timezone
- Moderation settings (auto-mute thresholds, content rules)
- Trust system config (decay rates, propagation depths per severity level)

**User** (Kratos manages identity, extended in app DB)
- `kratos_identity_id` (FK to Kratos)
- Display name, bio, avatar URL
- `trust_score` (computed, cached) — 0.0 to 100.0
- `role`: `pending`, `member`, `moderator`, `council`, `banned`
- `joined_at`, `is_active`

**Vouch** — the trust graph edge (stored in Apache AGE)
- Voucher (user A) → Vouchee (user B)
- `created_at`, `revoked_at`
- `status`: `active`, `revoked`
- One vouch per pair. Revoking breaks the trust chain.

**Post** — a bell ring
- Author (user ID), body (max 1000 chars), optional image path
- `status`: `visible`, `removed_by_author`, `removed_by_mod`
- `removal_reason` (if moderated)
- `created_at`, `edited_at`

**Reaction** — lightweight post interactions
- User ID, Post ID, reaction type (`bell`, `heart`, `celebrate`)
- Unique constraint: one reaction per type per user per post

**ModerationAction** — audit trail
- Target user, acting moderator, action type (`warn`, `mute`, `suspend`, `ban`)
- Severity (1–5), reason, duration (for mute/suspend)
- `created_at`, `expires_at`

**Report** — user-submitted flags
- Reporter, post, reason, status

## Trust Score System

### Score Composition

```
trust_score = (
    tenure_score     × 0.15 +
    activity_score   × 0.20 +
    voucher_score    × 0.35 +
    moderation_score × 0.30
)
```

All components normalize to 0.0–100.0. The final score is also 0.0–100.0.

### Tenure Score (15%)

```
tenure_score = min(100, (days_since_joined / 365) × 100)
```

Linear ramp, caps at 1 year.

### Activity Score (20%)

```
recent_posts = posts in last 90 days
recent_reactions_received = reactions received in last 90 days

post_component = min(100, (recent_posts / 90) × 100)
reaction_component = min(100, (recent_reactions_received / 270) × 100)

activity_score = (post_component × 0.5) + (reaction_component × 0.5)
```

Caps prevent gaming by spam-posting. 90-day rolling window.

### Voucher Score (35%)

```
people_i_vouched = count of active vouches I've given
their_avg_trust = average trust score of my vouchees

voucher_health = their_avg_trust / 100.0
base_voucher = min(100, people_i_vouched × 15)   # caps at ~7 vouches
voucher_score = base_voucher × voucher_health
```

Your score depends on the behavior of people you vouch for.

### Moderation Score (30%)

```
moderation_score = 100.0 - direct_penalties - propagated_penalties
```

Direct penalties by action type:

| Action | Severity | Penalty | Decay Period |
|--------|----------|---------|-------------|
| Warn (minor) | 1 | −5 | 90 days |
| Warn (moderate) | 2 | −10 | 180 days |
| Mute | 3 | −25 | 270 days |
| Suspend | 4 | −40 | 365 days |
| Ban | 5 | Score → 0 | Permanent |

Penalties decay linearly over their decay period. Old infractions fade.

### Trust Propagation

When a moderation action fires, penalties propagate backward through the vouch graph:

| Severity | Max Depth | Decay per Hop |
|----------|-----------|---------------|
| 1 (minor) | 1 level | 50% |
| 2 (moderate) | 1 level | 30% |
| 3 (serious) | 2 levels | 40% per hop |
| 4 (severe) | 2 levels | 30% per hop |
| 5 (ban) | 3 levels | 25% per hop |

**Example:** User C gets banned (severity 5). User B vouched for C. User A vouched for B.
- C: banned, score → 0
- B (1 hop): penalty = base × 75%
- A (2 hops): penalty = base × 56%
- A's voucher at 3 hops: penalty = base × 42%

### Propagation Algorithm

```
function propagate_trust_impact(target_user, action):
    severity = action.severity
    max_depth = DEPTH_MAP[severity]
    decay_rate = DECAY_MAP[severity]
    base_penalty = PENALTY_MAP[severity]

    # AGE Cypher query: walk vouch graph backward
    voucher_chains = GRAPH_QUERY("""
        MATCH path = (voucher)-[:VOUCHED_FOR*1..{max_depth}]->(target)
        WHERE target.id = $target_id
        RETURN voucher.id, length(path) as depth
    """)

    for each (voucher_id, depth) in voucher_chains:
        propagated_penalty = base_penalty × (decay_rate ^ depth)
        record_propagated_penalty(voucher_id, action.id, propagated_penalty)
        invalidate_trust_cache(voucher_id)
```

### Recalculation Strategy

Trust scores are not computed on every request:

1. **On moderation action**: synchronously propagate penalties, enqueue async recalculation for affected users
2. **On vouch/revoke**: enqueue recalculation for the voucher
3. **Nightly batch**: recalculate tenure and activity for all users
4. **Serve from Redis cache**: TTL 5 min fallback, primarily event-driven invalidation

### Anti-Gaming Protections

- **Vouch cooldown**: 3 vouches per day max
- **Minimum trust to vouch**: trust ≥ 60.0
- **Cycle detection**: AGE query checks for circular vouch chains before allowing
- **Revocation penalty**: −3 for 30 days when you revoke a vouch (prevents vouch-and-revoke gaming, but small enough that revoking a bad actor is still clearly worth it)
- **Sybil resistance**: new users need a vouch from someone with trust ≥ 60, or council approval during bootstrap

### Role Thresholds

- `pending` → `member`: vouched by a member (or council-approved during bootstrap)
- `member` → `moderator`: trust ≥ 85.0, active 90+ days, vouched by 2+ moderators
- `moderator` → `council`: trust ≥ 95.0, active 180+ days, existing council majority vote
- **Demotion**: trust below role threshold for 30 consecutive days → automatic downgrade

## API Design

REST API versioned under `/api/v1`. Kratos handles its own routes for login/registration.

### Authentication Flow

1. User registers/logs in through Kratos (frontend → Kratos directly)
2. Kratos issues session cookie or token
3. API requests include Kratos session
4. Backend middleware calls Kratos `toSession` to validate, gets `identity_id`
5. Middleware loads app user (trust score, role) from DB into request context
6. Handlers check role/trust for authorization

### Endpoints

#### Auth
```
POST   /api/v1/auth/register          # initiate Kratos registration
POST   /api/v1/auth/login             # initiate Kratos login
POST   /api/v1/auth/logout            # destroy Kratos session
GET    /api/v1/auth/session           # validate session, return profile
```

#### Users
```
GET    /api/v1/users/me               # current user profile + trust + role
PATCH  /api/v1/users/me               # update display name, bio, avatar
GET    /api/v1/users/:id              # public profile
GET    /api/v1/users/:id/vouches      # who this user vouched for
GET    /api/v1/users/:id/vouched-by   # who vouched for this user
```

#### Vouching
```
POST   /api/v1/vouches                # vouch for a user
DELETE /api/v1/vouches/:vouchee_id    # revoke vouch
GET    /api/v1/vouches/pending        # users awaiting vouches (council/mod)
POST   /api/v1/vouches/approve/:id    # council approves pending user
```

#### Posts
```
GET    /api/v1/posts                  # chronological feed (cursor-based)
POST   /api/v1/posts                  # create post (body + optional image)
GET    /api/v1/posts/:id              # single post with reactions
PATCH  /api/v1/posts/:id             # edit own post (15 min window)
DELETE /api/v1/posts/:id              # soft-delete own post
```

#### Reactions
```
POST   /api/v1/posts/:id/reactions         # add reaction
DELETE /api/v1/posts/:id/reactions/:type    # remove reaction
```

#### Moderation
```
GET    /api/v1/moderation/queue              # reported posts
POST   /api/v1/moderation/actions            # take action
GET    /api/v1/moderation/actions/:user_id   # action history
POST   /api/v1/posts/:id/report              # report a post
```

#### Admin (council)
```
GET    /api/v1/admin/trust/config     # trust system config
PATCH  /api/v1/admin/trust/config     # update config
GET    /api/v1/admin/stats            # town stats
GET    /api/v1/admin/council/votes    # pending votes
POST   /api/v1/admin/council/votes    # cast vote
```

### Authorization Matrix

| Role | Capabilities |
|------|-------------|
| pending | View feed (read-only), complete profile |
| member | Post, react, vouch, report |
| moderator | + moderation queue, warn/mute/suspend |
| council | + ban, system config, council votes, bootstrap approval |

Trust score gates:
- Posting: trust ≥ 30.0
- Vouching: trust ≥ 60.0

### Pagination

Cursor-based using UUIDv7 (time-ordered):
```
GET /api/v1/posts?cursor=<last_uuid>&limit=25
→ { posts: [...], next_cursor: "..." }
```

## Caching Strategy (Redis)

**Feed cache:** sorted set of latest 100 posts, score = timestamp. TTL 60s. Invalidated on post create/remove.

**Trust score cache:** per-user. Invalidated by moderation events and vouch changes. TTL 5 min fallback.

**Rate limits (sliding window):**
- Posts: 10/hour/user
- Reactions: 60/hour/user
- Reports: 5/hour/user
- Vouches: 3/day/user

**Session cache:** Kratos session lookups cached 30s.

## Image Handling

- Upload as multipart with post creation
- Validate: max 5MB, JPEG/PNG/WebP
- Store with UUIDv7 filename under `/storage/the-bell/images/`
- Serve via static file handler with cache headers
- Storage behind interface for future S3 migration

## Project Structure

```
the-bell/
├── cmd/
│   └── bell/                  # main.go entrypoint
├── internal/
│   ├── config/                # env/config loading
│   ├── server/                # HTTP server, chi router, middleware
│   ├── middleware/             # auth, role check, rate limit, logging
│   ├── handler/               # HTTP handlers by domain
│   │   ├── post.go
│   │   ├── user.go
│   │   ├── vouch.go
│   │   ├── moderation.go
│   │   ├── admin.go
│   │   └── reaction.go
│   ├── domain/                # core types (no external deps)
│   │   ├── user.go
│   │   ├── post.go
│   │   ├── vouch.go
│   │   ├── reaction.go
│   │   └── moderation.go
│   ├── service/               # business logic
│   │   ├── post.go
│   │   ├── user.go
│   │   ├── vouch.go
│   │   ├── trust.go           # trust score engine
│   │   ├── moderation.go
│   │   └── feed.go
│   ├── repository/            # data access
│   │   ├── postgres/          # sqlc + AGE queries
│   │   └── redis/             # cache implementations
│   └── storage/               # file storage interface
├── migrations/                # goose SQL migrations
├── queries/                   # sqlc query definitions
├── sqlc.yaml
├── web/                       # React SPA (Vite)
│   ├── src/
│   ├── package.json
│   └── vite.config.ts
├── docker-compose.yml         # full stack
├── kratos/                    # Ory Kratos config
│   ├── kratos.yml
│   └── identity.schema.json
├── Dockerfile                 # multi-stage: Go + React → single image
├── go.mod
└── go.sum
```

## Deployment

### Single municipality

```yaml
services:
  bell:            # Go API + embedded React SPA
  postgres:        # PostgreSQL 17 + Apache AGE
  kratos:          # Ory Kratos
  kratos-migrate:  # Kratos DB migrations on startup
  redis:           # Cache + rate limiting
```

Traefik routes the configured domain to the bell container.

### Home server variant

Uses shared Postgres (with AGE extension installed) instead of dedicated instance, matching the existing services pattern.

### Distribution to municipalities

- Self-contained `docker-compose.yml` with own Postgres
- First-run: `bell setup --council user1@email,user2@email,...`
- Environment variables for customization (town name, domain, SMTP)

## Council Bootstrap

1. Deployer runs `bell setup --council email1,email2,...` (3–5 people)
2. Kratos accounts created, app roles set to `council`
3. Council approves new users directly (no vouch chain needed)
4. At 20+ members, normal vouch-based system activates
5. Council retains override capability, but system self-governs

## Implementation Phases

### Phase 1 — Foundation
- Project scaffold (Go module, chi, Docker Compose)
- Database migrations (goose, Postgres + AGE)
- Ory Kratos integration (identity, session middleware)
- Core post CRUD + chronological feed

### Phase 2 — Trust System
- Vouch graph (AGE edges, CRUD, cycle detection)
- Trust score engine (4-component calculation)
- Trust-based authorization middleware
- Background recalculation worker

### Phase 3 — Moderation
- Reporting system
- Graduated actions (warn → mute → suspend → ban)
- Trust penalty propagation through vouch graph
- Audit trail

### Phase 4 — Council & Governance
- Bootstrap CLI + setup flow
- Council voting system
- Automatic role promotion/demotion
- In-app notifications for role changes

### Phase 5 — Frontend (React SPA)
- Vite + React + TypeScript + Tailwind scaffold
- Feed, compose, profile screens
- Kratos login/registration UI
- Moderation and admin interfaces

### Phase 6 — Hardening
- Redis caching (feed, trust scores, sessions)
- Rate limiting
- Image upload/serving
- Structured logging (Alloy/Loki compatible)
- Integration and unit tests

### Phase 7 — Distribution
- Self-contained Docker Compose for municipalities
- Setup wizard CLI
- Documentation (admin guide, user guide, API docs)

## Future Considerations (Not Built Now)

- Federation between towns (ActivityPub or custom)
- Mobile app (React Native, same API)
- S3 image storage
- Neo4j migration if AGE hits limits
- Live feed updates (WebSocket/SSE)
- Push notifications
