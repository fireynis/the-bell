# API Reference

## Base URL

All API endpoints are prefixed with `/api/v1`. For example:

```
GET https://bell.example.com/api/v1/posts
```

## Authentication

The Bell uses [Ory Kratos](https://www.ory.sh/kratos/) for authentication. Clients must include a valid Kratos session cookie in requests to authenticated endpoints.

Kratos session cookies are obtained by completing the Kratos login flow. The middleware validates the cookie by calling `FrontendAPI.ToSession()` on the Kratos public API. On success, the corresponding local user is loaded into the request context.

Unauthenticated requests to protected endpoints receive:

```json
{"error": "unauthorized"}
```

## Error Format

All errors are returned as JSON with a single `error` field:

```json
{"error": "description of the problem"}
```

Standard HTTP status codes:

| Status | Meaning |
|--------|---------|
| 400 | Validation error (bad input) |
| 401 | Unauthorized (missing or invalid session) |
| 403 | Forbidden (insufficient role or inactive account) |
| 404 | Not found |
| 409 | Conflict (e.g., edit window expired) |
| 429 | Rate limit exceeded |
| 500 | Internal server error |

## Rate Limiting

Rate limiting requires Redis (`REDIS_URL` must be set). Limits are per-user, per-endpoint using a sliding window. When rate limited, the response includes a `Retry-After` header with the window duration in seconds.

| Endpoint | Limit | Window |
|----------|-------|--------|
| `POST /api/v1/posts` | 10 requests | 1 hour |
| `POST /api/v1/posts/{postId}/reactions` | 60 requests | 1 minute |
| `POST /api/v1/posts/{id}/report` | 5 requests | 1 hour |
| `POST /api/v1/vouches` and `DELETE /api/v1/vouches/{id}` | 3 requests | 24 hours |

The council approval endpoints (`GET /api/v1/vouches/pending`,
`POST /api/v1/vouches/approve/{id}`) are **not** rate limited, even though they
share the `/api/v1/vouches` prefix with the member vouching endpoints above.
They once carried this limiter by accident of that shared prefix, which capped
council approvals at 3 per day — and since bootstrap mode does not end until 20
active members exist, standing up a town could not finish in under a week. The
budget belongs to members vouching, not to council members approving.

Two endpoints carry a second limit enforced in the service layer, independently
of Redis:

- **Reports**: 5 per hour per reporter.
- **Vouches**: 3 per calendar day per voucher, counted from local midnight
  rather than over a rolling window. This is a separate ceiling from the
  sliding-window limiter above, so a member can meet either one first.

If Redis is unavailable, rate limiting fails open (requests are allowed through).

## Pagination

### Feed Pagination (Cursor-Based)

The post feed uses cursor-based pagination for efficient scrolling:

```
GET /api/v1/posts?cursor={last_post_id}&limit=20
```

- `cursor`: The ID of the last post from the previous page. Omit for the first page.
- `limit`: Number of posts to return. Default: 20, max: 100.

The response includes a `next_cursor` field when there are more results:

```json
{
  "posts": [...],
  "next_cursor": "0193a7b2-..."
}
```

### Offset-Based Pagination

The moderation queue and action history use offset-based pagination:

```
GET /api/v1/moderation/queue?limit=20&offset=0
```

---

## Endpoints

### Health

#### `GET /healthz`

Health check endpoint. No authentication required.

**Response** `200 OK`:

```json
{"status": "ok"}
```

---

### Current User

#### `GET /api/v1/me`

Returns the authenticated user's profile. Does not require the user to be active -- suspended and banned users can still call this endpoint to check their status.

**Auth**: Required
**Role**: Any (including suspended/banned)

**Response** `200 OK`:

```json
{
  "id": "0193a7b2-1234-7000-8000-000000000001",
  "display_name": "Alice",
  "bio": "Town enthusiast",
  "avatar_url": "https://example.com/alice.jpg",
  "trust_score": 85.5,
  "role": "member",
  "is_active": true,
  "joined_at": "2025-06-15T10:30:00Z",
  "muted_until": "2025-07-02T16:00:00Z"
}
```

##### `muted_until`

`muted_until` appears **only on the endpoints that return your own profile** —
this one, `GET /api/v1/users/me`, and the response to `PUT /api/v1/users/me`. It
is never present on another user's profile: a mute is between that user and the
moderators.

The field is **omitted entirely** when there is no mute in force, so its
presence is the whole answer to "am I muted?". A mute whose time has already
passed is reported the same way as no mute at all, so a client needs no clock of
its own to interpret the field.

---

### Users

#### `GET /api/v1/users/me`

Returns the authenticated user's profile. Requires the user to be active.

**Auth**: Required
**Role**: Any active user

**Response** `200 OK`: Same shape as `GET /api/v1/me`, including `muted_until`.

---

#### `PUT /api/v1/users/me`

Updates the authenticated user's profile.

**Auth**: Required
**Role**: Any active user

**Request**:

```json
{
  "display_name": "Alice Smith",
  "bio": "Longtime Springfield resident",
  "avatar_url": "https://example.com/alice-new.jpg"
}
```

| Field | Type | Required | Constraints |
|-------|------|----------|-------------|
| `display_name` | string | Yes | Non-empty, max 100 characters |
| `bio` | string | No | Max 500 characters |
| `avatar_url` | string | No | URL string |

**Response** `200 OK`: Updated user profile, including `muted_until` — this is
a self view, so it carries the same fields `GET /api/v1/users/me` does.

```bash
curl -X PUT https://bell.example.com/api/v1/users/me \
  -H "Content-Type: application/json" \
  -H "Cookie: ory_kratos_session=..." \
  -d '{"display_name":"Alice Smith","bio":"Hello!","avatar_url":""}'
```

---

#### `GET /api/v1/users/{id}`

Returns a public user profile by ID.

**Auth**: None
**Role**: None

**Response** `200 OK`: User profile object. This response **never** includes
`muted_until`, whether or not the user is muted — see the note under
`GET /api/v1/me`.

---

#### `GET /api/v1/users/{id}/posts`

Returns a user's **visible** posts. Posts the author deleted
(`removed_by_author`) and posts a moderator removed (`removed_by_mod`) are
excluded, the same way they are excluded from the feed.

**Auth**: None
**Role**: None

**Query Parameters**:

| Param | Type | Default | Max |
|-------|------|---------|-----|
| `limit` | int | 20 | 100 |

**Response** `200 OK`:

```json
{
  "posts": [
    {
      "id": "0193a7b2-...",
      "author_id": "0193a7b2-...",
      "body": "Hello, Springfield!",
      "image_path": "/uploads/0193a7b2-xxxx.jpg",
      "status": "visible",
      "created_at": "2025-07-01T12:00:00Z",
      "edited_at": null
    }
  ]
}
```

---

#### `GET /api/v1/users/{id}/vouches`

Returns vouches given and received by a user.

**Auth**: None
**Role**: None

**Response** `200 OK`:

```json
{
  "received": [
    {
      "id": "0193a7b2-...",
      "voucher_id": "0193a7b2-...",
      "vouchee_id": "0193a7b2-...",
      "status": "active",
      "created_at": "2025-07-01T12:00:00Z"
    }
  ],
  "given": []
}
```

---

### Posts

#### `GET /api/v1/posts`

Returns the public post feed (visible posts in reverse chronological order).

**Auth**: None, but honoured when present (see below)
**Role**: None

**Query Parameters**:

| Param | Type | Default | Max |
|-------|------|---------|-----|
| `cursor` | string | (none) | -- |
| `limit` | int | 20 | 100 |

**Response** `200 OK`:

```json
{
  "posts": [
    {
      "id": "0193a7b2-...",
      "author_id": "0193a7b2-...",
      "body": "Beautiful day in Springfield!",
      "image_path": "",
      "status": "visible",
      "created_at": "2025-07-01T14:30:00Z",
      "edited_at": null,
      "author_display_name": "Alice",
      "author_avatar_url": "https://example.com/alice.jpg",
      "reaction_counts": {"bell": 3, "heart": 1},
      "user_reactions": ["bell"]
    }
  ],
  "next_cursor": "0193a7b2-..."
}
```

```bash
# First page
curl https://bell.example.com/api/v1/posts?limit=20

# Next page
curl https://bell.example.com/api/v1/posts?cursor=0193a7b2-...&limit=20
```

##### Sending a session cookie changes the response

This endpoint stays fully public — an anonymous request is served normally — but
a Kratos session cookie, if you send one, is read rather than ignored:

| Field | Anonymous caller | Authenticated caller |
|-------|------------------|----------------------|
| `reaction_counts` | Present | Present |
| `user_reactions` | **Always absent** | The caller's own reactions on that post |

`user_reactions` is the caller's own reactions and nobody else's, so without an
identity there is no answer to give. Both fields are omitted entirely when
empty, so a post with no reactions carries neither, and neither does a post the
caller has not reacted to.

Enrichment is best-effort: if the reaction lookup fails, the posts are still
returned without these fields rather than the request failing.

---

#### `GET /api/v1/posts/{id}`

Returns a single post by ID.

**Auth**: None, but honoured when present — it decides whether a removed post is
visible to you, and populates `user_reactions` exactly as on `GET /api/v1/posts`
**Role**: None

**Response** `200 OK`: Post object.
**Response** `404 Not Found`: No post with that ID, **or** a removed post you are
not entitled to see.

##### Who can see a removed post

A post whose `status` is `removed_by_author` or `removed_by_mod` is returned
only to:

- the **author** of the post, or
- an **active** `moderator` or `council` member.

Everyone else — including anonymous callers, and including a *suspended*
moderator, who gets the ordinary reader's view — receives `404`.

That `404` is **byte-identical** to the one for an id that never existed, so the
two cases cannot be told apart from outside. This is deliberate: post ids are
public while a post is live, so the people holding one are exactly the people
who saw it before it was taken down, and a distinguishable response would
confirm to them that the post was removed rather than deleted.

An entitled caller gets the post with its `status` set to `removed_by_author` or
`removed_by_mod`, which is how an author or moderator can still retrieve one.

A moderator's `removal_reason` and the `removed_by` moderator's id are **never
serialized on any response**. They are moderation metadata, not part of the post
object on the wire, on this endpoint or any other.

---

#### `POST /api/v1/posts`

Creates a new post. Accepts either `application/json` or `multipart/form-data`.

**Auth**: Required
**Role**: `member` or higher
**Rate Limit**: 10/hour
**Trust**: >= 30

**JSON Request**:

```json
{
  "body": "Hello, Springfield!",
  "image_path": ""
}
```

**Multipart Request**:

```bash
curl -X POST https://bell.example.com/api/v1/posts \
  -H "Cookie: ory_kratos_session=..." \
  -F "body=Check out this sunset!" \
  -F "image=@sunset.jpg"
```

| Field | Type | Required | Constraints |
|-------|------|----------|-------------|
| `body` | string | Yes | Non-empty, max 1,000 characters |
| `image` | file | No | JPEG, PNG, or WebP. Max 5 MB |

**Response** `201 Created`: The created post object.
**Response** `403 Forbidden`: `posting not allowed`.

A `403` means the author failed at least one posting gate. All of these produce
it, and **no post is written** in any case:

- an active **mute** (see `muted_until` under `GET /api/v1/me`)
- role `pending` or `banned`
- a deactivated account (suspended)
- trust score below 30

The check runs twice: once in the handler, so a rejected request is turned away
before its upload is parsed, and once inside the post service, which is the
authoritative one.

Both return `403`, but the bodies differ: the handler's early rejection sends
`{"error": "posting not allowed"}`, while the service's sends the generic
`{"error": "forbidden"}`. Neither says which gate you failed. Treat any `403`
here as "you may not post right now" and check `muted_until`, `role`,
`is_active` and `trust_score` on `GET /api/v1/me` to find out why.

---

#### `PATCH /api/v1/posts/{id}`

Updates a post's body text. Only the author can edit, and only within 15 minutes of creation.

**Auth**: Required
**Role**: `member` or higher

**Request**:

```json
{
  "body": "Updated text here"
}
```

**Response** `200 OK`: Updated post object.
**Response** `409 Conflict`: Edit window expired.

```bash
curl -X PATCH https://bell.example.com/api/v1/posts/0193a7b2-... \
  -H "Content-Type: application/json" \
  -H "Cookie: ory_kratos_session=..." \
  -d '{"body":"Fixed a typo"}'
```

---

#### `DELETE /api/v1/posts/{id}`

Deletes a post (marks it as `removed_by_author`). Only the author can delete their own posts.

**Auth**: Required
**Role**: `member` or higher

**Response** `204 No Content`

```bash
curl -X DELETE https://bell.example.com/api/v1/posts/0193a7b2-... \
  -H "Cookie: ory_kratos_session=..."
```

---

### Reactions

Valid reaction types are `bell`, `heart`, and `celebrate`. Anything else is
rejected with `400`, and the response message echoes the type you sent.

#### `POST /api/v1/posts/{postId}/reactions`

Adds a reaction to a post.

**Auth**: Required
**Role**: `member` or higher
**Rate Limit**: 60/minute

**Request**:

```json
{"type": "bell"}
```

**Response** `200 OK`: The reaction object.
**Response** `404 Not Found`: No post with that ID.

Adding a reaction you already have is **idempotent**, not an error: the request
succeeds and the original reaction — including its original timestamp — is left
in place. The underlying insert is an upsert, so a double-tap or a retried
request needs no special handling from clients.

Reacting to a post that does not exist returns `404 not found`. It is worth
expecting: a feed card can outlive the post behind it, so a reaction can arrive
for a post deleted between render and tap. This case previously returned `500`.

Note the deliberate asymmetry with `DELETE` below — adding a reaction to a
missing post is a `404`, removing one is a `204`. Adding names a post that must
exist; removing only asserts that a reaction is not there afterwards, which is
true either way.

```bash
curl -X POST https://bell.example.com/api/v1/posts/0193a7b2-.../reactions \
  -H "Cookie: ory_kratos_session=..." \
  -H "Content-Type: application/json" \
  -d '{"type":"heart"}'
```

---

#### `DELETE /api/v1/posts/{postId}/reactions/{type}`

Removes one of your reactions from a post.

**Auth**: Required
**Role**: `member` or higher

**Response** `204 No Content`

Removing a reaction that is not there also returns `204` — the delete matches no
rows and reports no error. The endpoint is idempotent: a reaction is a toggle,
so a retry or a double-tap is ordinary use, and there is **no `404`** for a
reaction you never left.

---

### Town Configuration

#### `GET /api/v1/config`

Returns the town's public configuration as a flat string map (for example
`town_name`, `primary_color`, `accent_color`).

**Auth**: Not required

`bootstrap_mode` is deliberately withheld from this response, so an
unauthenticated visitor cannot tell whether the town has been claimed yet.

**Response** `200 OK`

---

#### `PUT /api/v1/admin/config`

Updates town configuration.

**Auth**: Required
**Role**: `council`

**Request**: a flat string map. Only `town_name`, `primary_color`, and
`accent_color` may be written; every other key — `bootstrap_mode` in particular
— is owned by the server.

```json
{"town_name": "Springfield", "accent_color": "#c62828"}
```

**Response** `204 No Content`

**Response** `400 Bad Request`: `key not allowed: <key>`. The **whole request is
rejected and nothing is written** if any key is disallowed. Validation completes
before the first write on purpose: map iteration order is random, so validating
as it wrote would apply a random subset of a rejected request.

---

### Reports

#### `POST /api/v1/posts/{id}/report`

Reports a post for moderator review.

**Auth**: Required
**Role**: `member` or higher
**Rate Limit**: 5/hour

**Request**:

```json
{
  "reason": "This post contains misinformation about the water supply"
}
```

| Field | Type | Required | Constraints |
|-------|------|----------|-------------|
| `reason` | string | Yes | Non-empty, max 1,000 characters |

Validation rules:
- Cannot report your own post
- Cannot report the same post twice
- Post must be visible
- Max 5 reports per hour per reporter

**Response** `201 Created`:

```json
{
  "id": "0193a7b2-...",
  "reporter_id": "0193a7b2-...",
  "post_id": "0193a7b2-...",
  "reason": "This post contains misinformation about the water supply",
  "status": "pending",
  "created_at": "2025-07-01T15:00:00Z"
}
```

---

### Moderation

All moderation endpoints require `moderator` role or higher and an active account.

#### `GET /api/v1/moderation/queue`

Returns pending reports for moderator review.

**Auth**: Required
**Role**: `moderator` or higher

**Query Parameters**:

| Param | Type | Default | Max |
|-------|------|---------|-----|
| `limit` | int | 20 | 100 |
| `offset` | int | 0 | -- |

**Response** `200 OK`:

```json
{
  "reports": [
    {
      "id": "0193a7b2-...",
      "reporter_id": "0193a7b2-...",
      "post_id": "0193a7b2-...",
      "reason": "Spam content",
      "status": "pending",
      "created_at": "2025-07-01T15:00:00Z"
    }
  ]
}
```

```bash
curl https://bell.example.com/api/v1/moderation/queue \
  -H "Cookie: ory_kratos_session=..."
```

---

#### `PATCH /api/v1/moderation/reports/{id}`

Updates a report's status.

**Auth**: Required
**Role**: `moderator` or higher

**Request**:

```json
{
  "status": "reviewed"
}
```

| Field | Type | Required | Values |
|-------|------|----------|--------|
| `status` | string | Yes | `reviewed` or `dismissed` |

**Response** `200 OK`: Updated report object.

---

#### `POST /api/v1/moderation/posts/{id}/remove`

Takes a post down. The post's `status` becomes `removed_by_mod`, and the
moderator's id is recorded in `removed_by` alongside the `reason`.

**Auth**: Required
**Role**: `moderator` or higher

**Request**:

```json
{
  "reason": "Repeated misinformation about the water supply"
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `reason` | string | Yes | Why the post was removed |

**Response** `204 No Content`
**Response** `400 Bad Request`: `validation error: reason must not be empty`,
`validation error: reason exceeds 1000 characters`, or
`validation error: post is already removed` — only a `visible` post can be
removed, so re-removing one rewrites nothing.
**Response** `403 Forbidden`: not an active moderator or council member.
**Response** `404 Not Found`: No post with that ID.

The `reason` and the recording moderator are stored but **never returned by any
endpoint** — see `GET /api/v1/posts/{id}`. They exist for the audit trail, not
for the author.

Removal writes **no `moderation_actions` row** and therefore costs the author no
trust. That is deliberate: every moderation action propagates a trust penalty
through the vouch graph, so making removal an action would dock the author and
everyone who ever vouched for them each time a post came down. Removal acts on
the content; warn, mute, suspend and ban act on the person.

This is distinct from `DELETE /api/v1/posts/{id}`, which is the author removing
their own post and sets `removed_by_author` with no moderator recorded. Both
take the post out of the feed and out of profile listings.

```bash
curl -X POST https://bell.example.com/api/v1/moderation/posts/0193a7b2-.../remove \
  -H "Content-Type: application/json" \
  -H "Cookie: ory_kratos_session=..." \
  -d '{"reason":"Repeated misinformation"}'
```

---

#### `POST /api/v1/moderation/actions`

Takes a moderation action against a user. Creates the action record and propagates trust penalties through the vouch graph.

**Auth**: Required
**Role**: `moderator` or higher

**Request**:

```json
{
  "target_user_id": "0193a7b2-...",
  "action_type": "warn",
  "severity": 1,
  "reason": "Posting misleading information",
  "duration_seconds": null
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `target_user_id` | string | Yes | User to action |
| `action_type` | string | Yes | `warn`, `mute`, `suspend`, or `ban` |
| `severity` | int | Yes | Must match action type (see below) |
| `reason` | string | Yes | Non-empty, max 1,000 characters |
| `duration_seconds` | int/null | Depends | Required for `mute` and `suspend`; rejected for `ban`. Must be positive when given |

Action type to severity mapping:

| Action | Valid Severities |
|--------|-----------------|
| `warn` | 1, 2 |
| `mute` | 3 |
| `suspend` | 4 |
| `ban` | 5 |

Validation rules:
- Cannot moderate yourself
- Target user must exist
- Reason is required, max 1,000 characters
- Bans cannot have a duration
- Mutes and suspends require a duration
- A duration, where allowed, must be greater than zero

A `warn` is the one action with no rule either way: it neither requires
`duration_seconds` nor rejects it. A duration sent with a warn is stored on the
action's `expires_at` and has no effect on the user, since a warn imposes no
restriction to expire. Send `null`.

A mute sent without `duration_seconds` is rejected with
`mute requires a duration; use suspend for an indefinite restriction` — an
indefinite mute is a suspension, and suspend is the action for that.

**What each action does to the target**, beyond the trust penalty it propagates:

| Action | Immediate effect |
|--------|------------------|
| `warn` | None. The penalty is the whole action |
| `mute` | Sets `muted_until` to the action's `expires_at`, blocking `POST /api/v1/posts` until then. **Does not change the trust score** |
| `suspend` | Deactivates the account (`is_active` becomes false) |
| `ban` | Sets the role to `banned` and the trust score to 0 |

The mute's end time is the one the moderator chose: `users.muted_until` and the
action's `expires_at` are the same instant, so the two cannot disagree. A mute
applies even to a user already below the posting threshold — their score may
recover before the mute expires.

Mutes cannot currently be lifted early through the API; a mute runs for the
duration it was given.

**Response** `201 Created`:

```json
{
  "action": {
    "id": "0193a7b2-...",
    "target_user_id": "0193a7b2-...",
    "moderator_id": "0193a7b2-...",
    "action": "warn",
    "severity": 1,
    "reason": "Posting misleading information",
    "duration": null,
    "created_at": "2025-07-01T16:00:00Z",
    "expires_at": null
  },
  "penalties": [
    {
      "id": "0193a7b2-...",
      "user_id": "0193a7b2-...",
      "moderation_action_id": "0193a7b2-...",
      "penalty_amount": 5.0,
      "hop_depth": 0,
      "created_at": "2025-07-01T16:00:00Z",
      "decays_at": "2025-10-01T16:00:00Z"
    }
  ]
}
```

```bash
curl -X POST https://bell.example.com/api/v1/moderation/actions \
  -H "Content-Type: application/json" \
  -H "Cookie: ory_kratos_session=..." \
  -d '{
    "target_user_id": "0193a7b2-...",
    "action_type": "mute",
    "severity": 3,
    "reason": "Repeated spam",
    "duration_seconds": 86400
  }'
```

---

#### `GET /api/v1/moderation/actions/{user_id}`

Returns moderation action history for a user, including associated trust penalties.

**Auth**: Required
**Role**: `moderator` or higher

**Query Parameters**:

| Param | Type | Default | Description |
|-------|------|---------|-------------|
| `limit` | int | 20 | Max results |
| `offset` | int | 0 | Skip results |
| `role` | string | (none) | Set to `moderator` to view actions taken BY the user (council only) |

**Response** `200 OK`:

```json
{
  "actions": [
    {
      "action": {
        "id": "0193a7b2-...",
        "target_user_id": "0193a7b2-...",
        "moderator_id": "0193a7b2-...",
        "action": "warn",
        "severity": 1,
        "reason": "Minor issue",
        "duration": null,
        "created_at": "2025-07-01T16:00:00Z",
        "expires_at": null
      },
      "penalties": [
        {
          "id": "0193a7b2-...",
          "user_id": "0193a7b2-...",
          "moderation_action_id": "0193a7b2-...",
          "penalty_amount": 5.0,
          "hop_depth": 0,
          "created_at": "2025-07-01T16:00:00Z",
          "decays_at": "2025-10-01T16:00:00Z"
        }
      ]
    }
  ]
}
```

---

### Vouching

Vouching is how a member endorses another resident. These endpoints are open to
any active member; the council approval endpoints below are a separate,
bootstrap-only path to the same outcome.

#### `POST /api/v1/vouches`

Vouches for another user.

**Auth**: Required
**Role**: `member` or higher
**Rate Limit**: 3/24 hours (plus a separate 3-per-calendar-day service limit)
**Trust**: >= 60

**Request**:

```json
{
  "vouchee_id": "0193a7b2-1234-7000-8000-000000000002"
}
```

| Field | Type | Required | Constraints |
|-------|------|----------|-------------|
| `vouchee_id` | string | Yes | Non-empty after trimming |

**Response** `201 Created`:

```json
{
  "id": "0193a7b2-...",
  "voucher_id": "0193a7b2-...",
  "vouchee_id": "0193a7b2-...",
  "status": "active",
  "created_at": "2025-07-01T12:00:00Z"
}
```

**Response** `400 Bad Request`. Validation failures name the reason, prefixed
with `validation error:` — the body is `{"error": "validation error: cannot
vouch for yourself"}` and so on for:

- `cannot vouch for yourself`
- `vouch already exists for this pair`
- `daily vouch limit (3) reached`
- `vouch would create a cycle in the trust graph`

A missing or blank `vouchee_id` is rejected before the service sees it, and is
the one 400 with no prefix: `{"error": "vouchee_id is required"}`.

**Response** `403 Forbidden`: you are below trust 60, inactive, or
`pending`/`banned`. The body is the fixed `{"error": "forbidden"}` — the
underlying reason is deliberately not disclosed.
**Response** `404 Not Found`: no user with that `vouchee_id`. Body:
`{"error": "not found"}`.

If the vouchee is `pending`, a successful vouch **promotes them to `member`**.
The promotion is best-effort: the vouch is still created and still returned
`201` if the promotion itself fails.

---

#### `DELETE /api/v1/vouches/{id}`

Revokes a vouch. `{id}` is the id of the **vouch**, not of the vouchee — take it
from `GET /api/v1/users/{id}/vouches`.

**Auth**: Required
**Role**: `member` or higher
**Rate Limit**: 3/24 hours

**Response** `204 No Content`
**Response** `400 Bad Request`: `validation error: vouch is already revoked`, or
`vouch id is required` when the segment is blank.
**Response** `403 Forbidden`: you are neither the original voucher nor an active
moderator or council member. Body: `{"error": "forbidden"}`.
**Response** `404 Not Found`: no vouch with that id.

##### Revoking costs the voucher trust — but only the voucher

A successful revocation removes the trust graph edge, and if **the caller is the
original voucher**, records a **-3 trust penalty on them that decays after 30
days**.

A moderator or council member revoking somebody else's vouch is **not** charged.
The penalty exists to make vouch-and-revoke gaming cost something, and the
gamer is the voucher; a moderator doing cleanup should not be taxed for it.
Their instrument against a bad voucher is a moderation action, which carries its
own penalty.

The penalty stays at hop depth 0 — unlike a moderation penalty, it does **not**
propagate to the voucher's own vouchers, because revoking is a self-inflicted
cost on one person.

```bash
curl -X DELETE https://bell.example.com/api/v1/vouches/0193a7b2-... \
  -H "Cookie: ory_kratos_session=..."
```

---

### Approvals (Bootstrap Mode Only)

These endpoints are only available while bootstrap mode is active. They return `403 Forbidden` after bootstrap mode ends.

#### `GET /api/v1/vouches/pending`

Returns all pending users awaiting council approval.

**Auth**: Required
**Role**: `council`
**Rate Limit**: None — see the note under [Rate Limiting](#rate-limiting)

**Response** `200 OK`:

```json
{
  "users": [
    {
      "id": "0193a7b2-...",
      "kratos_identity_id": "...",
      "display_name": "New User",
      "bio": "",
      "avatar_url": "",
      "trust_score": 50.0,
      "role": "pending",
      "is_active": true,
      "joined_at": "2025-07-01T10:00:00Z",
      "created_at": "2025-07-01T10:00:00Z",
      "updated_at": "2025-07-01T10:00:00Z"
    }
  ]
}
```

---

#### `POST /api/v1/vouches/approve/{id}`

Approves a pending user, promoting them to `member`. When the active member count reaches 20, bootstrap mode auto-disables.

**Auth**: Required
**Role**: `council`
**Rate Limit**: None — bootstrap needs 20 approvals, so a daily cap would stall
it for a week. See the note under [Rate Limiting](#rate-limiting)

**Response** `200 OK`: The approved user object with `role` set to `member`.

```bash
curl -X POST https://bell.example.com/api/v1/vouches/approve/0193a7b2-... \
  -H "Cookie: ory_kratos_session=..."
```

---

### Council Voting

#### `GET /api/v1/admin/council/votes`

Returns all open proposals with vote tallies.

**Auth**: Required
**Role**: `council`

**Response** `200 OK`:

```json
{
  "proposals": [
    {
      "proposal_id": "0193a7b2-...",
      "approve_count": 2,
      "reject_count": 1,
      "total_council": 5,
      "status": "pending",
      "votes": [
        {
          "id": "0193a7b2-...",
          "proposal_id": "0193a7b2-...",
          "voter_id": "0193a7b2-...",
          "vote": "approve",
          "created_at": "2025-07-01T17:00:00Z"
        }
      ]
    }
  ]
}
```

---

#### `POST /api/v1/admin/council/votes`

Casts a vote on a proposal.

**Auth**: Required
**Role**: `council`

**Request**:

```json
{
  "proposal_id": "0193a7b2-...",
  "vote": "approve"
}
```

| Field | Type | Required | Values |
|-------|------|----------|--------|
| `proposal_id` | string | Yes | Proposal UUID |
| `vote` | string | Yes | `approve` or `reject` |

Validation rules:
- Cannot vote twice on the same proposal
- Vote must be `approve` or `reject`

**Response** `201 Created`: Updated proposal summary (same shape as in the list response). Status changes to `approved` when approve votes exceed half of total council, or `rejected` when reject votes exceed half.

```bash
curl -X POST https://bell.example.com/api/v1/admin/council/votes \
  -H "Content-Type: application/json" \
  -H "Cookie: ory_kratos_session=..." \
  -d '{"proposal_id":"0193a7b2-...","vote":"approve"}'
```

---

### Admin Statistics

#### `GET /api/v1/admin/stats`

Returns aggregate town statistics.

**Auth**: Required
**Role**: `council`

**Response** `200 OK`:

```json
{
  "total_users": 142,
  "posts_today": 23,
  "active_moderators": 5,
  "pending_users": 3
}
```

```bash
curl https://bell.example.com/api/v1/admin/stats \
  -H "Cookie: ory_kratos_session=..."
```

---

### Static Files

#### `GET /uploads/*`

Serves uploaded images from the configured `IMAGE_STORAGE_PATH`. Responses include a `Cache-Control: public, max-age=31536000` header (1-year cache).

**Auth**: None
**Role**: None
