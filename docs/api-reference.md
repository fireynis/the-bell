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

The moderation queue, the action history, the member directory and the council's
approval queue use offset-based pagination:

```
GET /api/v1/moderation/queue?limit=20&offset=0
GET /api/v1/users?limit=25&offset=0
GET /api/v1/vouches/pending?limit=25&offset=0
```

The directory and the approval queue additionally return a `total`, so a client
can size a pager without walking off the end of the list. The two share their
bounds — 25 by default, 100 at most, clamped rather than rejected — and both
accept the same `q` substring search over `display_name`; they differ only in
sort order, for the reason given under each.

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
  ],
  "residency_claim": "12 Mill Lane, behind the churchyard"
}
```

##### `residency_claim`

What the caller told the council about where they live, set through
[`PUT /api/v1/users/me/residency-claim`](#put-apiv1usersmeresidency-claim). It
is on the three self views — this endpoint, `GET /api/v1/users/me` and the
response to `PUT /api/v1/users/me` — so a client can prefill the field that
edits it. The key is always present; a caller who has said nothing gets the
empty string, so prefilling is one case rather than two.

It is on **no other response** except the council's approval queue. See the
endpoint that sets it for where the line is drawn and why.

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
`mute_lifts`, `suspension_lifts` and `residency_claim`.

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
`mute_lifts`, `suspension_lifts` and `residency_claim` — this is a self view, so
it carries the same fields `GET /api/v1/users/me` does. This request does not
touch the residency claim; it is returned so that saving a profile does not hand
the client back a response whose residency field looks empty.

```bash
curl -X PUT https://bell.example.com/api/v1/users/me \
  -H "Content-Type: application/json" \
  -H "Cookie: ory_kratos_session=..." \
  -d '{"display_name":"Alice Smith","bio":"Hello!","avatar_url":""}'
```

---

#### `PUT /api/v1/users/me/residency-claim`

Records what the authenticated resident says about where they live, for the
council to read while deciding whether to approve them.

**Auth**: Required
**Role**: Any active user — including `pending`

**Request**:

```json
{
  "claim": "12 Mill Lane, behind the churchyard"
}
```

| Field | Type | Required | Constraints |
|-------|------|----------|-------------|
| `claim` | string | Yes | Trimmed; 0–300 characters. The empty string clears it |

**Response** `204 No Content` — no body.

```bash
curl -X PUT https://bell.example.com/api/v1/users/me/residency-claim \
  -H "Content-Type: application/json" \
  -H "Cookie: ory_kratos_session=..." \
  -d '{"claim":"12 Mill Lane, behind the churchyard"}'
```

##### It is an attestation, not a verified fact

Nothing checks the claim against anything. There is no address service, no
postcode table and no document upload behind this endpoint: a council member
reads the string, recognises the street or does not, and approves or does not.
What the platform records is that this person said X and that council member
believed them — the approval is already attributed — which is the honest record
and the only one available.

How hard to press on a claim is each town's own business. A village where
everyone knows everyone needs less than a commuter suburb, and encoding one
town's rigour in the API would impose it on every other. Accordingly the
endpoint accepts any text: "the blue house behind the old mill" is a better
answer than a formatted address in many towns, and a validator demanding
something address-shaped would refuse it along with most of the world's address
formats.

##### Who may set it

Auth plus the ordinary active check, with no role floor. A `pending` resident is
active — that is what makes their application reviewable — so this is reachable
from exactly the state the field exists for, and a member who moves house can
still correct what they said. Banned and suspended accounts are refused, and
neither has an application before the council.

##### Where it is readable

Two readers, and they are the two who have a reason: the resident who wrote it,
and the council reviewing them.

- **The resident's own profile** — `GET /api/v1/me`, `GET /api/v1/users/me`, and
  the response to `PUT /api/v1/users/me`. They wrote it, so there is no
  disclosure in showing it back, and a client needs it to prefill the field that
  edits the claim. Without that, changing one word of an address means retyping
  it from memory into a box that looks like it lost the answer.
- **The council's approval queue** —
  [`GET /api/v1/vouches/pending`](#get-apiv1vouchespending).

It is **not** on other residents' profiles, **not** in the member directory, and
**not** on posts or vouch listings. It is also not returned by
`POST /api/v1/vouches/approve/{id}`: approving somebody ends the review, and
with it the reason the council could see the claim.

The line is structural rather than a matter of remembering. `domain.User` tags
the field so it cannot be serialized by default, and exactly two response types
name it — the self view and the queue entry. The self view holds it on its own
type rather than on the shared profile shape it embeds, so a public profile
cannot pick it up by inheritance.

Clearing is a normal request, not an error. Withdrawing what you said about
where you live has to be as easy as saying it.

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
      "alt_text": "The bandstand in Wilson Park, freshly painted green",
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
      "alt_text": "",
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

##### Image descriptions

`alt_text` is what the author wrote to describe their image, for a reader who
cannot see it. It is present on **every** post on every read, as the empty
string when the post has no image or the author described none — never omitted —
so a client can render `alt={post.alt_text}` without a fallback. An empty
`alt_text` must reach the page as `alt=""`; an `<img>` with no `alt` attribute at
all is announced by its filename, which is worse than silence.

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
  "image_path": "",
  "alt_text": ""
}
```

**Multipart Request**:

```bash
curl -X POST https://bell.example.com/api/v1/posts \
  -H "Cookie: ory_kratos_session=..." \
  -F "body=Check out this sunset!" \
  -F "image=@sunset.jpg" \
  -F "alt_text=The sun setting behind the water tower, sky orange"
```

| Field | Type | Required | Constraints |
|-------|------|----------|-------------|
| `body` | string | Yes | Non-empty, max 1,000 characters |
| `image` | file | No | JPEG, PNG, or WebP. Max 5 MB |
| `alt_text` | string | No | Trimmed, max 500 **characters** (runes). Only with an image |

**Response** `201 Created`: The created post object.
**Response** `403 Forbidden`: `posting not allowed`.

##### Describing the image

`alt_text` is the author's description of the image, read aloud in place of it
by a screen reader. It is optional — posting stays frictionless, and a post
with an undescribed image is still a post — but a client should ask for one
whenever an image is attached.

Two rules are worth knowing before you send it:

- It is bounded at **500 runes**, not bytes, unlike `body`'s 1,000-byte bound.
  A description in Kanji or Cyrillic gets the same room as one in English.
- Sending a non-empty `alt_text` on a post with **no image** is `400`, and no
  post is written. There is nothing for the description to describe, and storing
  it would leave a string on the record that no reader ever hears. An empty or
  whitespace-only value is fine either way, so a client that always sends the
  field does not have to special-case a text-only post.

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

Updates a post's body text and, on a post that has an image, its description.
Only the author can edit, and only within 15 minutes of creation. The image
itself cannot be changed or removed.

**Auth**: Required
**Role**: `member` or higher

**Request**:

```json
{
  "body": "Updated text here",
  "alt_text": "A new description of the image"
}
```

| Field | Type | Required | Meaning |
|-------|------|----------|---------|
| `body` | string | Yes | The new body. Same rules as on create |
| `alt_text` | string | No | **Omit** to leave the description as it is; send `""` to clear it |

**Response** `200 OK`: Updated post object.
**Response** `400 Bad Request`: `alt_text` over 500 runes, or non-empty on a post
with no image. Nothing is written — the body is not updated either.
**Response** `409 Conflict`: Edit window expired. The description is editable in
exactly the same window as the body; there is one edit, not two.

```bash
# Fix a typo, leaving the image description untouched.
curl -X PATCH https://bell.example.com/api/v1/posts/0193a7b2-... \
  -H "Content-Type: application/json" \
  -H "Cookie: ory_kratos_session=..." \
  -d '{"body":"Fixed a typo"}'

# Improve the description as well.
curl -X PATCH https://bell.example.com/api/v1/posts/0193a7b2-... \
  -H "Content-Type: application/json" \
  -H "Cookie: ory_kratos_session=..." \
  -d '{"body":"Fixed a typo","alt_text":"A heron on the frozen millpond"}'
```

##### Why `alt_text` is optional and `body` is not

An absent `alt_text` means "leave it alone"; an `alt_text` of `""` means "clear
it". The two must be distinguishable, because the alternative is that every
client editing a typo has to remember to resend the description — and the one
that forgets silently strips it off the image, with no error and nothing in the
UI to notice. `body` has no such treatment: it has always been required on
every PATCH, and sending it unchanged is how you edit only the description.

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

One page of the pending users awaiting council approval, longest wait first.

**Auth**: Required
**Role**: `council`
**Rate Limit**: None — see the note under [Rate Limiting](#rate-limiting)

**Query Parameters**: the member directory's, exactly.

| Param | Type | Default | Max | Notes |
|-------|------|---------|-----|-------|
| `limit` | int | 25 | 100 | Values outside the range are clamped, not rejected |
| `offset` | int | 0 | -- | Offset-based, like the moderation queue |
| `q` | string | (empty) | 100 chars | Case-insensitive substring of `display_name`. Empty lists everyone waiting |

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
      "updated_at": "2025-07-01T10:00:00Z",
      "residency_claim": "12 Mill Lane, behind the churchyard"
    }
  ],
  "total": 43
}
```

`total` is the number of applicants matching `q`, not the size of the page —
it is what the council's screen counts the waiting neighbours from. `users` is
always an array, never `null`.

```bash
curl "https://bell.example.com/api/v1/vouches/pending?q=ali&limit=25&offset=0" \
  -H "Cookie: ory_kratos_session=..."
```

##### Order

`joined_at` **ascending** — the applicant who has been waiting longest is first.
This is deliberately the opposite of the member directory, which is newest-first
because it answers a different question ("who has just arrived and needs a
vouch?"). A queue is worked through in the order people joined it, and a
registration flood must not be able to bury somebody who signed up last week
behind fifty newer strangers.

Ties break on id, so paging with `offset` cannot silently repeat or skip an
applicant when two accounts were created in the same instant.

##### Who is listed

Pending accounts only, and only the ones with a live application: banned and
deactivated accounts are excluded, as is anyone currently serving a suspension.
A suspension that has lapsed is not a suspension, so that applicant is
reviewable again the moment it expires — the same `NOW()` clock every other user
query uses.

With no `q`, `total` counts exactly what the `pending_users` figure on
[`GET /api/v1/admin/stats`](#get-apiv1adminstats) counts, so the dashboard and
the queue cannot contradict each other.

##### `residency_claim`

What the applicant said about where they live, set through
[`PUT /api/v1/users/me/residency-claim`](#put-apiv1usersmeresidency-claim). The
key is always present; an applicant who has said nothing gets the empty string,
so the reviewing screen can tell "said nothing" from "not asked" without a
second rule.

**It is a claim the applicant made, not a fact the platform verified.** Nothing
checks it against anything — see the endpoint that sets it. Treat it as one
signal among the vouches and the display name, weigh it however your town
weighs such things, and remember that the record the platform keeps is who
approved on the strength of it.

Besides the applicant's own profile, this is the only endpoint in the API that
returns a residency claim — and the only one that returns somebody *else's*. It
is deliberately absent from public profiles, the member directory, and the
approved-user response below.

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

### Council Proposals

The council's Town Hall: a motion is raised, the council votes, and a motion
that carries **executes immediately**. There is no separate step where somebody
applies the result.

That last sentence is the whole of what changed. `GET`/`POST
/api/v1/admin/council/votes` used to exist and are **gone**. They recorded votes
against proposal ids that referred to nothing — there was no proposal entity, so
a motion had no text, no proposer and no outcome, and no code path ever tallied
or acted on one. Any client still calling them will get a `404`.

#### Proposal types

| `type` | Target | What passing it does |
|--------|--------|----------------------|
| `council_promotion` | An active `moderator` | Sets the target's role to `council` |
| `council_removal` | A sitting `council` member | Sets the target's role to `member` |
| `bootstrap_reentry` | None | Sets `bootstrap_mode` to `true` |

Every type on the list changes something. That is a rule about what may be added
to the list, not a coincidence: a type nothing executes would be a motion the
council can pass and watch do nothing, which is the failure this feature exists
to end.

#### The electorate

**A motion about a person is never decided with that person's own seat in the
denominator.** A motion carries on a simple majority — strictly more than half —
of its own electorate, and `council_size` in every response below is that
electorate rather than the size of the council. A client rendering progress
towards a majority must use it.

- `council_promotion` and `bootstrap_reentry`: the whole council. (A promotion's
  target is a moderator and holds no seat, so the rule above costs nothing until
  the motion has carried; then it stops the seat the motion just created from
  being counted against it.)
- `council_removal`: the council **minus the target**. They do not vote on
  whether they keep their seat — `POST .../votes` answers them `403` — and
  counting them would raise the bar their colleagues have to clear. On a council
  of four that is 2-of-3 rather than 3-of-4.

An electorate of zero never decides anything, so an empty or miscounted council
cannot carry motions unopposed.

#### Execution, and what happens when it can no longer run

A motion is decided the moment either side reaches its majority. On a pass the
change is applied in the same request, and a role change writes a `role_history`
row exactly as every other role change in the application does.

Execution re-validates first, because a motion can outlive the state it was
raised in. If the target is no longer eligible — a moderator demoted while the
vote ran, a council member who has already left, a town that has grown past the
bootstrap threshold — **the motion is recorded as `rejected`**. Nothing happened,
and a `passed` motion that changed nothing would be a lie in the council's own
record.

A removal is also refused at execution if it would leave the council empty, on
top of the same check when it is raised: two removals can be open at once, each
legal when raised, and between them they must not leave the town with nobody who
can approve a resident or vote a council back.

If execution fails for an infrastructural reason, the vote still stands and the
motion stays `open` with its majority intact. The next `GET
/api/v1/admin/proposals?status=open` finishes it. Execution is idempotent, so a
repair cannot promote somebody twice.

---

#### `GET /api/v1/admin/proposals`

Lists motions as the calling council member sees them.

**Auth**: Required
**Role**: `council`

| Query | Values | Default |
|-------|--------|---------|
| `status` | `open`, `decided` | `open` — anything that is not exactly `decided` lists the open queue |

**Response** `200 OK`:

```json
{
  "proposals": [
    {
      "id": "0193a7b2-...",
      "type": "council_promotion",
      "target_user_id": "0193a7b2-...",
      "target_display_name": "Grace",
      "rationale": "She has run the report queue for a year.",
      "created_by": "0193a7b2-...",
      "created_by_display_name": "Ada",
      "status": "open",
      "created_at": "2026-08-14T09:00:00Z",
      "approve_count": 2,
      "reject_count": 1,
      "council_size": 5,
      "my_vote": "approve"
    }
  ]
}
```

| Field | Notes |
|-------|-------|
| `target_user_id`, `target_display_name` | Omitted entirely for `bootstrap_reentry`. Their absence is the answer to "is this about somebody" |
| `status` | `open`, `passed` or `rejected` |
| `decided_at` | Present only once decided |
| `council_size` | The electorate for **this** motion — see above |
| `my_vote` | `"approve"`, `"reject"`, or `null` when the caller has not voted. Always present |

`approve_count`, `reject_count` and `council_size` are the same for every
council member; `my_vote` is not. The response is therefore built per caller and
must not be cached across them.

Listing the open queue also finishes any motion whose majority was reached but
whose execution did not complete — see above. Such a motion leaves this listing
and appears under `?status=decided`.

```bash
curl https://bell.example.com/api/v1/admin/proposals?status=open \
  -H "Cookie: ory_kratos_session=..."
```

---

#### `POST /api/v1/admin/proposals`

Raises a motion.

**Auth**: Required
**Role**: `council`

**Request**:

```json
{
  "type": "council_promotion",
  "target_user_id": "0193a7b2-...",
  "rationale": "She has run the report queue for a year."
}
```

| Field | Type | Required | Constraints |
|-------|------|----------|-------------|
| `type` | string | Yes | One of the three types above |
| `target_user_id` | string | For the two targeted types | Must be absent or empty for `bootstrap_reentry` |
| `rationale` | string | Yes | Trimmed; 1–1000 characters |

Validation, all `400` unless noted:

- `council_promotion` requires a target who is an **active moderator**. Promoting
  straight from `member` would skip the standing the moderator role represents.
- `council_removal` requires a target who is **on the council**, and requires the
  council to have more than one member.
- `bootstrap_reentry` takes **no target**, requires the town to be **out** of
  bootstrap mode, and requires the active member count to be **below** the
  threshold that ends bootstrap mode (20). Above it, the mode would be switched
  straight back off by the next approval, so the council would have voted for
  something the system undoes on its own; the refusal says so.
- One open motion per `(type, target)`. A second is refused, because two open
  motions on one question would split the council's votes between them and
  neither would reach a majority. A **decided** motion blocks nothing — the
  council may revisit a question next month.
- An unknown `target_user_id` is `404`.

**Response** `201 Created`: the new motion, in the shape above, with a zero
tally and its electorate filled in.

```bash
curl -X POST https://bell.example.com/api/v1/admin/proposals \
  -H "Content-Type: application/json" \
  -H "Cookie: ory_kratos_session=..." \
  -d '{"type":"council_promotion","target_user_id":"0193a7b2-...","rationale":"She has run the report queue for a year."}'
```

---

#### `POST /api/v1/admin/proposals/{id}/votes`

Casts the caller's vote.

**Auth**: Required
**Role**: `council`

**Request**:

```json
{
  "approve": true
}
```

| Field | Type | Required | Values |
|-------|------|----------|--------|
| `approve` | boolean | Yes | `true` to approve, `false` to reject |

- One vote per council member per motion. A second — including changing your
  mind — is `400`.
- A motion that is already decided is `400`.
- The target of a `council_removal` voting on their own removal is `403`.
- An unknown motion is `404`.

**Response** `200 OK`: the motion in the same shape as the listing, updated —
including any decision and execution this vote just triggered. The council
member who casts the deciding vote therefore sees the outcome, and any role
change that has just taken effect, without reloading.

```bash
curl -X POST https://bell.example.com/api/v1/admin/proposals/0193a7b2-.../votes \
  -H "Content-Type: application/json" \
  -H "Cookie: ory_kratos_session=..." \
  -d '{"approve":true}'
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
