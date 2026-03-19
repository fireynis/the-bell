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
| `POST /api/v1/posts/{id}/report` | 5 requests | 1 hour |
| Approval endpoints | 3 requests | 24 hours |

Reports also have a server-side hourly limit of 5 per reporter (enforced regardless of Redis).

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
  "joined_at": "2025-06-15T10:30:00Z"
}
```

---

### Users

#### `GET /api/v1/users/me`

Returns the authenticated user's profile. Requires the user to be active.

**Auth**: Required
**Role**: Any active user

**Response** `200 OK`: Same shape as `GET /api/v1/me`.

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

**Response** `200 OK`: Updated user profile.

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

**Response** `200 OK`: User profile object.

---

#### `GET /api/v1/users/{id}/posts`

Returns posts by a specific user.

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

**Auth**: None
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
      "edited_at": null
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

---

#### `GET /api/v1/posts/{id}`

Returns a single post by ID.

**Auth**: None
**Role**: None

**Response** `200 OK`: Post object.

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
| `duration_seconds` | int/null | Depends | Required for `mute` and `suspend`. Must be null for `warn` and `ban` |

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
- Bans cannot have a duration
- Mutes and suspends require a duration

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

### Approvals (Bootstrap Mode Only)

These endpoints are only available while bootstrap mode is active. They return `403 Forbidden` after bootstrap mode ends.

#### `GET /api/v1/vouches/pending`

Returns all pending users awaiting council approval.

**Auth**: Required
**Role**: `council`
**Rate Limit**: 3/24 hours

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
**Rate Limit**: 3/24 hours

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
