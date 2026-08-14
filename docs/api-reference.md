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

### Verified email (`REQUIRE_VERIFIED_EMAIL`)

When the server is started with `REQUIRE_VERIFIED_EMAIL=true`, a session whose
Kratos identity carries no verified address is authenticated but may not
participate. Those requests receive `403`:

```json
{"error": "email not verified"}
```

The message is distinct from the other `403` bodies (`forbidden`,
`account suspended`) so a client can tell "confirm your address" apart from
"speak to a moderator" without inspecting the route.

`GET /api/v1/me` is deliberately exempt and keeps answering `200`, because a
blocked resident has to be able to load the page that explains why. Everything
else behind a session — including `GET /api/v1/users/me` and the member
directory — is gated.

The flag defaults to **off**, and it depends on a working Kratos courier: the
verification mail is the only way for a resident to clear the check. See the
admin guide before enabling it.

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

| Endpoint | Limit | Window | Keyed by |
|----------|-------|--------|----------|
| `POST /api/v1/posts` | 10 requests | 1 hour | user |
| `POST /api/v1/posts/{postId}/reactions` | 60 requests | 1 minute | user |
| `POST /api/v1/posts/{id}/report` | 5 requests | 1 hour | user |
| `POST /api/v1/vouches` | 3 requests | 24 hours | user |
| `DELETE /api/v1/vouches/{id}` | 3 requests | 24 hours | user |
| Kratos registration flows (see below) | 10 requests | 1 hour | client IP |

Vouching and revoking have the **same limit but separate budgets**, and the
separation is the point. They shared one bucket until a member who had spent
their three vouches for the day found they could not withdraw one for the next
24 hours — revoking is the abuse-response path, what you do on realising you
vouched for the wrong person, so ordinary vouching must not be able to close it.
Spending either budget leaves the other untouched.

### Registration

Registration is the one limit keyed by **client IP** rather than by user, for
the obvious reason: it is what creates the user. It applies to the Kratos
endpoints reached through the `/.ory/*` proxy that start or submit a
registration flow:

- `GET /.ory/self-service/registration/browser` (flow init)
- `GET /.ory/self-service/registration/api` (flow init, native clients)
- `POST /.ory/self-service/registration` (submit)

Nothing else under `/.ory` is throttled — login, session checks, settings,
recovery, verification and logout all pass through untouched, as does
`GET /.ory/self-service/registration/flows`, which is the SPA reading back a
flow already in progress rather than starting a new one.

Ten per hour is not one account per hour. Kratos v1.x registration is two-step,
so a completed sign-up costs a flow init plus two submits, and a mistyped
password costs more. The budget allows roughly three sign-ups an hour from one
address while capping a scripted flood — the failure mode this exists for is an
attacker filling the council's manual approval queue with accounts nobody asked
for.

**Behind a reverse proxy, set `TRUSTED_PROXIES`.** Without it every request is
attributed to the proxy's own address and the whole town shares one registration
bucket. See the admin guide.

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

The moderation queue, the action history and the member directory use
offset-based pagination:

```
GET /api/v1/moderation/queue?limit=20&offset=0
GET /api/v1/users?limit=25&offset=0
```

The directory additionally returns a `total`, so a client can size a pager
without walking off the end of the list.

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
  "muted_until": "2025-07-02T16:00:00Z",
  "mute_lifts": [
    {
      "lifted_at": "2025-07-01T09:00:00Z",
      "previous_muted_until": "2025-07-05T09:00:00Z"
    }
  ],
  "suspension_lifts": [
    {
      "lifted_at": "2025-06-20T09:00:00Z",
      "previous_suspended_until": "2025-06-27T09:00:00Z"
    }
  ]
}
```

##### `mute_lifts` and `suspension_lifts`

Restrictions a moderator ended early, newest first, and **only on the caller's
own profile** — the three self views listed under `muted_until` below. They are
omitted entirely rather than sent as `[]` when there are none, on the same
principle as `muted_until`, and each is capped at the ten most recent.

These lists are about restrictions **undone**. What was *done* to a member —
warnings, mutes, suspensions, bans, with the reason and the trust each cost —
is read from `GET /api/v1/users/me/moderation-history`. A lift is deliberately
not in that list: it writes no `moderation_actions` row, carries no severity and
costs no trust, so filing mercy among the sanctions would misfile it.

`suspension_lifts` matters more than its counterpart. A mute at least shows to
the member as `muted_until` while it runs; a suspension only ever shows as
`is_active` being `false`, and lifting it just makes that revert — so without
this entry there is nothing to distinguish an early release from a suspension
that ran its full course.

Neither list names the moderator who acted. Which moderator handled a case
appears on **no** member-facing response — see the moderation history endpoint
below, which holds the same line for the actions themselves.

Only lifts that actually released the member appear. Both `DELETE` endpoints are
idempotent and accept a lift against anyone, and a no-op lift is recorded but
excluded here — a member must not be told a mute they never had was ended.

##### `muted_until`

`muted_until` appears on exactly four responses, and no others:

- the three that return **your own profile** — this one, `GET /api/v1/users/me`,
  and the response to `PUT /api/v1/users/me`; and
- `GET /api/v1/moderation/users/{user_id}/mute`, which is **moderator-only**.

It is never present on another user's profile. `GET /api/v1/users/{id}` does not
carry it whether or not that user is muted, and that route is not even
authenticated. A mute is between the muted user and the moderators; the
moderator route is the moderators' side of the same conversation, not a
widening of it.

The field is **omitted entirely** when there is no mute in force, so its
presence is the whole answer to "am I muted?". A mute whose time has already
passed is reported the same way as no mute at all, so a client needs no clock of
its own to interpret the field.

---

### Users

#### `GET /api/v1/users`

The member directory: everyone a signed-in resident may browse.

**Auth**: Required
**Role**: Any active user, **including pending** — there is deliberately no role
floor here.

**Query Parameters**:

| Param | Type | Default | Max | Notes |
|-------|------|---------|-----|-------|
| `limit` | int | 25 | 100 | Values outside the range are clamped, not rejected |
| `offset` | int | 0 | -- | Offset-based, like the moderation queue |
| `q` | string | (empty) | 100 chars | Case-insensitive substring of `display_name`. Empty lists everyone |

**Response** `200 OK`:

```json
{
  "users": [
    {
      "id": "0193a7b2-...",
      "display_name": "Alice Smith",
      "role": "member",
      "joined_at": "2025-07-01T12:00:00Z"
    }
  ],
  "total": 57
}
```

`total` is the number of people matching `q`, not the size of the page — it is
what a client renders a pager from. `users` is always an array, never `null`.

```bash
curl "https://bell.example.com/api/v1/users?q=ali&limit=25&offset=0" \
  -H "Cookie: ory_kratos_session=..."
```

##### Who is listed

Pending residents are included **on purpose**. A pending account cannot post, so
before this endpoint existed there was no way to find one — and finding one is
the entire prerequisite for vouching. The vouch graph's cold start depends on
residents being able to browse their neighbours and share a profile URL.

Excluded: banned accounts, deactivated accounts, and anyone currently serving a
suspension. A suspension that has lapsed is not a suspension, so those residents
are listed again the moment it expires — the clock is part of the filter, on the
same `NOW()` every other user query uses.

A resident who has never set a display name is still listed, with
`display_name` as the **empty string**. They are usually the newest arrivals, so
dropping them would hide exactly the people the directory exists to surface. As
elsewhere, the key is always present and a client falls back to the id for
anything falsy. Such a resident is not reachable by a `q` search, because there
is no name to match.

##### Order

`joined_at` descending — newest neighbours first, since they are the ones
needing a vouch. Ties break on id, so paging with `offset` cannot silently
repeat or skip a row.

##### What it does not carry

`trust_score`, `muted_until` and `is_active` are **not** in this response. The
directory is the one listing readable by any signed-in resident including a
pending one, and it is not where the town's trust scores and moderation posture
get published. `GET /api/v1/users/{id}` owns the public profile;
`GET /api/v1/me` owns the self view.

##### Search

`q` is matched literally. The `%` and `_` characters mean nothing special here —
a resident searching for `_` finds an underscore in somebody's name rather than
every neighbour in town.

---

#### `GET /api/v1/users/me`

Returns the authenticated user's profile. Requires the user to be active.

**Auth**: Required
**Role**: Any active user

**Response** `200 OK`: Same shape as `GET /api/v1/me`, including `muted_until`,
`mute_lifts` and `suspension_lifts`.

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

**Response** `200 OK`: Updated user profile, including `muted_until`,
`mute_lifts` and `suspension_lifts` — this is a self view, so it carries the
same fields `GET /api/v1/users/me` does.

```bash
curl -X PUT https://bell.example.com/api/v1/users/me \
  -H "Content-Type: application/json" \
  -H "Cookie: ory_kratos_session=..." \
  -d '{"display_name":"Alice Smith","bio":"Hello!","avatar_url":""}'
```

---

#### `GET /api/v1/users/me/moderation-history`

The moderation taken **against the signed-in member**, newest first: what was
done, why, when it ends, and what it cost their standing.

**Auth**: Required
**Role**: Any — **including suspended and banned**, and including a member who
has not verified their email.

**Query Parameters**:

| Param | Type | Default | Max |
|-------|------|---------|-----|
| `limit` | int | 20 | 100 |
| `offset` | int | 0 | — |

**Response** `200 OK`:

```json
{
  "actions": [
    {
      "id": "0193a7b2-1234-7000-8000-00000000000a",
      "action": "mute",
      "severity": 3,
      "reason": "Posting the same notice in six threads.",
      "created_at": "2026-03-01T12:00:00Z",
      "expires_at": "2026-03-04T12:00:00Z",
      "penalty": {
        "amount": 25,
        "decays_at": "2026-11-26T12:00:00Z"
      }
    }
  ]
}
```

A member who has never been moderated gets `200` with `"actions": []`. That is
the answer most callers will ever see, and it is a success rather than a `404`:
"nothing has happened to you" answers the question.

##### The subject is the session

There is no user id anywhere in this request — not in the path, not in a query
parameter. The subject is whoever the session authenticates, which is the whole
of the authorization: there is nothing to tamper with, so there is no way to ask
this route about anybody else. It is also why it carries no role floor. Every
member is entitled to their own record and to nobody else's.

##### Why it is readable while suspended or banned

Every other authenticated read requires an active account. This one does not,
and that is the point of it. A suspended member is precisely the person who most
needs to know what they did and when it ends; being told "account suspended" by
the endpoint that exists to explain the suspension is the failure this route was
added to remove. The same reasoning already governs `GET /api/v1/me`.

Email verification is skipped for the same reason: a member who has not verified
can still be moderated, so the explanation cannot be gated on it.

##### It names no moderator

No field here identifies who acted, and none will be added. This is the same
line `mute_lifts` and `suspension_lifts` hold, for the same reason: in a town
small enough to run this platform, naming the individual moderator turns a
decision of the moderation team into a personal grievance with a neighbour. A
member who disagrees takes it up with the team.

The moderator-facing `GET /api/v1/moderation/actions/{user_id}` does carry
`moderator_id` and `moderator_display_name`, and it requires the moderator role;
the two responses are separate types on the server precisely so they cannot
drift into one.

##### It carries only the member's own penalty

`penalty` is the **direct** trust cost to this member and nothing else. A
moderation action also propagates a decayed penalty to everyone who vouched for
the person — out to three hops for a ban — and none of that appears here.
Showing it would tell a member exactly who stands one step from them in the
vouch graph, and reveal that those neighbours took a hit.

For the same reason, penalties propagated **to** this member by somebody *else's*
moderation are absent: they would disclose that a neighbour had been moderated.
Those penalties are real and they do lower the member's composite trust score,
which reflects them silently. That is where they belong.

##### Reading `penalty`

- **`penalty` present with `decays_at`** — the penalty fades away completely at
  that time. Most do; a minor warning's 5 points are gone in 90 days.
- **`penalty` present with no `decays_at`** — it never decays. This is a ban's
  100 points, and nothing else.
- **`penalty` absent entirely** — no direct penalty was recorded for this
  action. This happens when propagation failed after the action was written, a
  case the server tolerates rather than losing the action over.

The distinction between the second and third is only readable because the whole
object is omitted in the third case, so "permanent" and "none" cannot be
confused.

##### `expires_at`

When the restriction the action imposed ends. Absent for a warning and for a
ban, neither of which expires.

It is the expiry **as recorded at the time**. A mute a moderator later lifted
early still reports the expiry it was originally given — the audit trail is not
rewritten after the fact, because it exists to preserve what was actually
decided. The lift reaches the member separately, as `mute_lifts` on their
profile. Do not read `expires_at` here to decide whether somebody is restricted
right now; `muted_until` on the self profile answers that.

```bash
curl https://bell.example.com/api/v1/users/me/moderation-history \
  -H "Cookie: ory_kratos_session=..."
```

---

#### `GET /api/v1/users/{id}`

Returns a public user profile by ID.

**Auth**: None
**Role**: None

**Response** `200 OK`: User profile object. This response **never** includes
`muted_until`, `mute_lifts` or `suspension_lifts`, whether or not the user is
muted — see the notes under `GET /api/v1/me`.

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
      "voucher_display_name": "Alice",
      "vouchee_id": "0193a7b2-...",
      "vouchee_display_name": "Bob",
      "status": "active",
      "created_at": "2025-07-01T12:00:00Z"
    }
  ],
  "given": []
}
```

##### Display names

Both parties are named on every row, on both lists, because the list shows the
pair. The ids stay alongside them: the names are for reading, the ids are what
a client links to a profile with.

A member who has not set a display name is sent as the **empty string**, not
omitted — the key is always present on this response, so a client falls back to
the id for anything falsy and needs no separate rule for an absent field.

The names come only from this listing. `POST /api/v1/vouches` echoes back the
vouch it created and carries neither, because the create path knows both people
by id alone.

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
      "reporter_display_name": "Alice",
      "post_id": "0193a7b2-...",
      "reason": "Spam content",
      "status": "pending",
      "created_at": "2025-07-01T15:00:00Z"
    }
  ]
}
```

`reporter_display_name` appears **only here**. This is the one read that shows a
report to somebody other than the person who filed it, and a moderator weighing
a report has to know who filed it. `POST /api/v1/posts/{id}/report` returns the
report to its own reporter and omits the field entirely.

A reporter who has set no display name has the key **omitted**, the same as on
the submit response — the field cannot distinguish "not looked up" from "no name
set", so treat any falsy value as "fall back to the id". Their report is still
in the queue either way: the name is missing, not the row.

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
**Role**: `moderator` or higher for `warn`, `mute`, and `suspend`; `ban` requires
`council`. The council check is enforced in the service, runs before the target
lookup (so a refused ban does not reveal whether the target exists), and fails
with `403 Forbidden`.

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
| `duration_seconds` | int/null | Depends | Required for `mute` and `suspend`; rejected for `warn` and `ban`. Must be positive when given |

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
- Warnings and bans cannot have a duration
- Mutes and suspends require a duration
- A duration, where allowed, must be greater than zero

`warn` and `ban` reject `duration_seconds` for the same reason: neither ends.
A warning is a permanent note on the record and imposes no restriction that
could expire, so a duration sent with one would be written to the action's
`expires_at` describing nothing — and anything later deciding "is this still in
force" from that column would read the warning as temporary. Send `null`.

A mute sent without `duration_seconds` is rejected with
`mute requires a duration; use suspend for an indefinite restriction` — an
indefinite mute is a suspension, and suspend is the action for that.

**What each action does to the target**, beyond the trust penalty it propagates:

| Action | Immediate effect |
|--------|------------------|
| `warn` | None. The penalty is the whole action |
| `mute` | Sets `muted_until` to the action's `expires_at`, blocking `POST /api/v1/posts` until then. **Does not change the trust score** |
| `suspend` | Sets `suspended_until` to the action's `expires_at`. The `is_active` column is not touched; the API's `is_active` field reads `false` while the suspension is in force and reverts to `true` on its own once it lapses. A moderator can end it early with `DELETE /api/v1/moderation/users/{user_id}/suspension` |
| `ban` | Sets the role to `banned` and the trust score to 0 |

The mute's end time is the one the moderator chose: `users.muted_until` and the
action's `expires_at` are the same instant, so the two cannot disagree. A mute
applies even to a user already below the posting threshold — their score may
recover before the mute expires.

A mute can be ended early by a moderator with
`DELETE /api/v1/moderation/users/{user_id}/mute`, and a suspension with
`DELETE /api/v1/moderation/users/{user_id}/suspension`. Either clears the
expiry on the user row and **leaves this action row exactly as written** — its
`expires_at` still shows the original end time. The audit trail records what was
decided at the time, so it is not the place to ask whether a restriction is
still in force; use `GET /api/v1/moderation/users/{user_id}/mute` or
`GET /api/v1/moderation/users/{user_id}/suspension` for that.

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
        "target_display_name": "Alice",
        "moderator_id": "0193a7b2-...",
        "moderator_display_name": "Mallory",
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

##### Display names

`target_display_name` and `moderator_display_name` are on this listing only.
`POST /api/v1/moderation/actions` echoes back the action it created and carries
neither: the create path knows both people by id alone, and two empty strings
there would read as two nameless members rather than as "not looked up".

A member who has set no display name has their key **omitted** rather than sent
as `""` — the field cannot distinguish that from "not looked up", so treat any
falsy value as "fall back to the id". The ids stay alongside the names, and the
moderator views link by id.

This does **not** widen who may see which moderator handled a case. The
`role=moderator` view is still council-only, exactly as it was when the id alone
was carried.

---

#### `GET /api/v1/moderation/users/{user_id}/mute`

Reports whether a user is currently muted, and until when.

**Auth**: Required
**Role**: `moderator` or higher

**Response** `200 OK`, user is muted:

```json
{"muted_until": "2025-07-02T16:00:00Z"}
```

**Response** `200 OK`, user is **not** muted:

```json
{}
```

**Response** `404 Not Found`: no user with that id.

The field is **omitted entirely** rather than sent as `null`, so its presence is
the whole answer — the same rule the self-profile responses use, so a client can
read both shapes with one code path. A mute whose time has already passed is
reported as no mute at all, so the client needs no clock of its own.

##### Why this endpoint exists

`muted_until` is on no other response a moderator can see. Without this, the
only available clue would be a past `mute` action in the audit trail — and that
row **never changes after a mute is lifted or expires**, so reading it would go
on reporting a mute that no longer exists. Ask this endpoint, not
`GET /api/v1/moderation/actions/{user_id}`, whether somebody is muted right now.

Unlike the `DELETE` below, this does **not** refuse a self-query: a moderator
may ask about their own mute here.

---

#### `DELETE /api/v1/moderation/users/{user_id}/mute`

Ends a mute before its duration runs out — for a mute applied in error, or one a
moderator agrees to shorten after an appeal.

**Auth**: Required
**Role**: `moderator` or higher

**Response** `204 No Content`
**Response** `400 Bad Request`: `validation error: cannot moderate yourself` —
see below.
**Response** `403 Forbidden`: not an active moderator or council member.
**Response** `404 Not Found`: no user with that id.

##### Idempotent

`204` is returned whether or not the user was actually muted. The caller asked
for a state and the state now holds, which is the same reasoning behind
`DELETE /api/v1/posts/{postId}/reactions/{type}` returning `204` for a reaction
that was never left.

A user who does **not exist** is still a `404`, though. Idempotence is about the
mute, not about the id: a mistyped user id must never report that somebody was
released.

##### A moderator cannot lift their own mute

Self-lifting is refused with `400`, and it is the one case no route guard can
catch — a mute does not deactivate an account, so a muted moderator passes both
the active check and the role check and would otherwise be able to overturn a
colleague's decision about themselves. The service refuses it explicitly, ahead
of the role check, so the caller gets the specific answer rather than being sent
off to acquire a role that still would not permit it.

Note the status: this is `400`, not `403`. Being muted is not a permissions
problem, and `POST /api/v1/moderation/actions` refuses self-moderation the same
way with the same message.

##### It writes no action row and touches no trust

Lifting a mute records **no `moderation_actions` row**. It therefore does not
appear in the member's action history, costs nobody trust, propagates no penalty
through the vouch graph, and queues no recalculation.

That is deliberate. Every severity in `moderation_actions` is 1–5 and every one
of those names a real trust penalty — there is no severity meaning "not a
punishment", so a release would have to file itself as one, against the person
released and against everyone who vouched for them. The audit trail also reaches
the person it concerns now, through
`GET /api/v1/users/me/moderation-history`, which opens each entry with "You were
muted" and the trust it cost — so an act of mercy would be shown to them as one
more sanction, in the plainest possible words. This is the same wall
`POST /api/v1/moderation/posts/{id}/remove` hit from the other side.

The consequence for readers: **the original mute action stays in the audit trail
exactly as written**, with its original `expires_at`, and nothing there marks it
as lifted. Do not read that row to decide whether somebody is muted now.

##### It is recorded, and the member sees it

The lift is written to `moderation_reliefs`, a table with **no severity column
at all** — which is what makes it safe to show the member. They read it back as
`mute_lifts` on their own profile (`GET /api/v1/me`, `GET /api/v1/users/me`,
`PUT /api/v1/users/me`) and nowhere else.

Lifts issued against somebody who was **not** muted are recorded but excluded
from that view. The endpoint is idempotent and accepts a lift against anyone, so
showing them would tell a member their mute was lifted when they never had one.

The record names no moderator. Which moderator acted appears on no member-facing
response, and changing that is a policy decision rather than a property of this
record.

```bash
curl -X DELETE https://bell.example.com/api/v1/moderation/users/0193a7b2-.../mute \
  -H "Cookie: ory_kratos_session=..."
```

---

#### `GET /api/v1/moderation/users/{user_id}/suspension`

Reports whether a user is currently suspended, and until when.

**Auth**: Required
**Role**: `moderator` or higher

**Response** `200 OK`, user is suspended:

```json
{"suspended_until": "2025-07-08T16:00:00Z"}
```

**Response** `200 OK`, user is **not** suspended:

```json
{}
```

**Response** `404 Not Found`: no user with that id.

Same rule as the mute status above: the field is **omitted entirely** rather
than sent as `null`, so its presence is the whole answer, and a suspension whose
time has already passed is reported as no suspension at all.

##### Why not just read `is_active`?

`is_active` on a user profile does read `false` while a suspension is in force.
But it also reads `false` for an account deactivated for any other reason, and
it never says **when** the suspension ends — so a moderator reading it cannot
tell whether an early lift is worth offering or how much time it would save.

The audit trail is not a substitute either: the original `suspend` action keeps
its original `expires_at` forever, whether or not the suspension was lifted or
has since lapsed.

Like the mute status and unlike the `DELETE` below, this does **not** refuse a
self-query.

---

#### `DELETE /api/v1/moderation/users/{user_id}/suspension`

Ends a suspension before its duration runs out — for a suspension applied in
error, or one a moderator agrees to shorten after an appeal.

**Auth**: Required
**Role**: `moderator` or higher

**Response** `204 No Content`
**Response** `400 Bad Request`: `validation error: cannot moderate yourself`.
**Response** `403 Forbidden`: not an active moderator or council member.
**Response** `404 Not Found`: no user with that id.

```bash
curl -X DELETE https://bell.example.com/api/v1/moderation/users/0193a7b2-.../suspension \
  -H "Cookie: ory_kratos_session=..."
```

This is the mute lift one severity up, and **every rule documented for
`DELETE .../mute` above holds here word for word**:

- **Idempotent.** `204` whether or not the user was actually suspended. A user
  who does not exist is still `404` — idempotence is about the suspension, not
  about the id.
- **No self-lift.** Refused with `400`, ahead of the role check, so the caller
  gets the specific answer.
- **No action row, no trust movement.** No `moderation_actions` row is written,
  no penalty propagates, no score is touched, no recalculation is queued. Every
  severity in that table names a real trust penalty, so filing a release there
  would punish the person released and everyone who vouched for them.
- **Recorded in `moderation_reliefs`**, and read back by the member as
  `suspension_lifts` on their own profile. Lifts against somebody who was not
  suspended are recorded but hidden from that view.

Clearing `suspended_until` is the whole operation. **`is_active` is deliberately
not written.** It reverts on its own once the suspension is cleared, and writing
it here would risk reactivating an account that was deactivated for some
entirely separate reason — the trap migration `00019` was written to remove,
arriving from the other side.

Before this endpoint existed a suspension could only end by lapsing, so a
moderator who suspended the wrong person had nothing to undo it with short of
waiting out the duration they had chosen.

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
- `vouch already exists for this pair` — only an **active** vouch conflicts; see
  re-vouching below
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
A promotion that fails is reported as a `500`, and the vouch is **not** rolled
back — the response says the promotion did not happen, not that nothing was
written. Retrying the vouch will then hit `vouch already exists for this pair`,
so a vouchee left pending this way needs a moderator rather than another vouch.

**Re-vouching after a revoke is allowed.** A pair whose vouch was revoked may
vouch again; the original vouch is reactivated, so the response carries the same
`id` with `status` back to `"active"` and `created_at` set to the new vouch's
time. Every rule that applies to a first vouch applies again — the trust floor,
the daily limit, cycle detection, and promoting a `pending` vouchee. Two things
deliberately do not reset: the revoked vouch still counts against the voucher's
daily limit for the day it was made, and the trust penalty the voucher paid for
revoking is not refunded (it decays on its own 30-day schedule). Both exist to
price vouch-and-revoke churn, which is exactly what re-vouching is half of.

---

#### `DELETE /api/v1/vouches/{id}`

Revokes a vouch. `{id}` is the id of the **vouch**, not of the vouchee — take it
from `GET /api/v1/users/{id}/vouches`.

**Auth**: Required
**Role**: `member` or higher
**Rate Limit**: 3/24 hours, on its own budget — spending the `POST` allowance
does not consume this one. See [Rate Limiting](#rate-limiting).

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
