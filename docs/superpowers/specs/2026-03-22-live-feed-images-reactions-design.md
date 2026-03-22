# Design: Live Feed, Image Posts, Reactions, and Notifications

**Date:** 2026-03-22
**Status:** Approved

## Overview

Bundle of features to make The Bell's feed interactive and fun: fix broken reactions, add image attachments to posts, SSE-powered live feed updates, bell sound notifications, and reaction notifications. Design principle: fun but not annoying — debounced, user-controllable, visually pleasant.

## Section 1: Fix Reactions (Complete the Missing Middle Layer)

**Problem:** Database schema, SQL queries, domain types, and frontend components exist but the handler, service, routes, and server wiring are all missing. PostCard hardcodes `count=0, active=false`.

**Changes:**

- Create `internal/service/reaction.go` — business logic: validate reaction type (bell/heart/celebrate), check user is authenticated, add/remove reactions via repository
- Create `internal/handler/reaction.go` — thin HTTP layer:
  - `POST /api/v1/posts/{postId}/reactions` — add reaction (body: `{"type": "bell"}`)
  - `DELETE /api/v1/posts/{postId}/reactions/{type}` — remove reaction
- Register routes in `internal/server/routes.go` under the posts group
- Add `WithReactionService()` option to `internal/server/server.go`, wire in `cmd/bell/main.go`
- Extend `domain.Post` to include `ReactionCounts map[ReactionType]int` and `UserReactions []ReactionType`
- Extend feed SQL queries (`ListPostsFeed`, `ListPostsFeedFirst`) to include reaction counts via aggregate subquery (grouped by reaction_type)
- Reaction counts are public data — no auth needed for counts
- `user_reactions` requires knowing who's viewing: add a separate lightweight query `ListUserReactionsForPosts(user_id, post_ids)` that returns the current user's reactions for a batch of post IDs. The feed handler calls this after fetching posts, only when the user is authenticated. Unauthenticated users see counts but no `user_reactions`.
- Update repository adapter to populate new Post fields
- Update `PostCard.tsx` to pass real `count` and `active` from post data to `ReactionButton`
- Update `Post` TypeScript interface to include `reaction_counts` and `user_reactions`
- Rate limit reaction endpoints: 60 reactions per minute per user
- `AddReaction` uses `ON CONFLICT DO UPDATE` (idempotent) — always returns 200 OK regardless of whether the reaction was new or already existed

**API response shape per post:**
```json
{
  "reaction_counts": {"bell": 3, "heart": 1, "celebrate": 0},
  "user_reactions": ["bell"]
}
```

**Note:** `user_reactions` is omitted (or empty) for unauthenticated requests. The feed endpoint remains accessible without auth; reaction data is layered on when auth is present.

## Section 2: Image Upload in Compose

**Backend:** Already implemented — multipart handler in `upload.go`, 5MB limit, JPEG/PNG/WebP validation with magic byte detection, `LocalStorage` saves to `/storage/the-bell/images`, served at `/uploads/`.

**Frontend changes to `Compose.tsx`:**

- Add camera/image icon button below the textarea
- Click opens native file picker (accept: `image/jpeg, image/png, image/webp`)
- Show thumbnail preview with an X button to remove before posting
- Switch API call from JSON `POST` to `multipart/form-data` (body + image fields in one request). The existing `ApiClient` hardcodes `Content-Type: application/json`, so add an `upload()` method to `ApiClient` that accepts `FormData` and omits the `Content-Type` header (letting the browser set the multipart boundary automatically).
- Client-side validation: file type check + 5MB limit with clear error message

**PostCard display improvements:**

- Constrain images with `max-height` so tall images don't dominate the feed
- Click image to expand in a simple lightbox overlay (no library, just a positioned div with backdrop)
- `loading="lazy"` attribute for images below the fold

**No backend changes needed.**

## Section 3: SSE Live Feed

**New SSE endpoint:** `GET /api/v1/feed/live`

**Backend:**

- Register the SSE route **outside** the main `/api` route group to avoid the `ContentTypeJSON` middleware that hardcodes `Content-Type: application/json`. Register it directly on the router (still under `/api/v1/feed/live` path) with its own middleware chain (auth only).
- The Go server has `WriteTimeout: 15s` which would kill SSE connections. The SSE handler must use `http.ResponseController` to call `SetWriteDeadline()` per-flush, extending the deadline after each successful write. This avoids removing the global timeout (which protects other endpoints).
- New handler that sets `Content-Type: text/event-stream`, `Cache-Control: no-cache`, `Connection: keep-alive`
- Requires authentication (Kratos middleware)
- On post creation in `PostService.Create()`, publish full serialized post to Redis pub/sub channel `bell:posts:new`
- SSE handler subscribes to Redis pub/sub channel on connect, writes events to client
- Each SSE goroutine owns exclusive write access to its `ResponseWriter` — no shared writer map needed. The goroutine filters its own subscription messages.
- Event format: `event: new_post\ndata: {json}\n\n`
- Heartbeat comment every 30s (`: heartbeat\n\n`) to keep connection alive and detect stale clients
- Clean up Redis subscription on client disconnect (`r.Context().Done()` fires on both client disconnect and server shutdown)
- On graceful server shutdown, SSE goroutines terminate via context cancellation — no special handling beyond existing shutdown logic

**Frontend — new `useLiveFeed` hook:**

- Opens `EventSource` to `/api/v1/feed/live`
- On `new_post` event: accumulate posts in a buffer
- Every 15 seconds, if buffer is non-empty, flush to `pendingPosts` state and update `pendingCount`
- Render **"N new posts"** banner at top of feed when `pendingCount > 0`
- Click banner: prepend buffered posts to the feed list, clear pending state, smooth scroll to top
- On `EventSource` error: auto-reconnect with exponential backoff (1s, 2s, 4s, max 30s)
- Close EventSource on unmount

**Why Redis pub/sub:** Already have Redis for caching. Pub/sub enables multi-instance fan-out if Bell ever scales horizontally.

## Section 4: Bell Sound & Mute Toggle

**Sound asset:**

- Ship `web/public/sounds/bell.mp3` — short, pleasant bell ding
- Use Web Audio API for playback (better control, non-blocking)
- Triggered by `useLiveFeed` hook when the 15s batch flushes with new posts
- Plays once per batch, not per post — avoids rapid-fire dings

**Mute toggle:**

- Small bell icon in the top-right area of the feed page header
- Unmuted: bell icon. Muted: bell-with-slash icon
- State stored in `localStorage` key `bell-sound-muted`
- Default: unmuted (sound on) — first experience should showcase the feature
- `useLiveFeed` checks mute state before calling audio play

**Bell ring animation:**

- When new posts arrive (regardless of mute state), the bell icon plays a CSS shake animation
- `@keyframes ring` — tilt left 15deg, right 15deg, settle — ~0.5s duration
- Visual indicator even when sound is off

## Section 5: Reaction Notifications

**New SSE event type:** `reaction` on the same `/api/v1/feed/live` endpoint.

**Backend:**

- On reaction creation, publish to Redis pub/sub channel `bell:reactions:new` with payload including `post_author_id`, `reaction_type`, and `post_id`
- SSE handler subscribes to both `bell:posts:new` and `bell:reactions:new`
- For reaction events: only forward to the connected client if their user ID matches the post's `author_id` — no leaking other users' reaction activity

**Frontend:**

- `useLiveFeed` hook handles `reaction` events
- Plays a softer, distinct sound (`web/public/sounds/chime.mp3`) — controlled by the same mute toggle
- Toast/pill notification bottom-right: "Someone reacted {emoji} to your post", fades after 3s
- Debounced on the same 15s window: if multiple reactions arrive, batch into "5 reactions on your post"

## Section 6: Fun Polish

**Post character countdown:**

- In `Compose.tsx`, replace the existing always-visible `X / 1000` counter (which currently has a single color change at 950) with a progressive color system:
  - 0-900: counter hidden
  - 900-950: green counter fades in
  - 950-980: yellow
  - 980-1000: red
- CSS `transition: color 0.3s` for smooth effect

**New posts banner styling:**

- Matches town theme colors (uses `--color-primary` from ThemeContext)
- Subtle slide-down animation when it appears
- Shows count badge that increments in real-time as more posts buffer

## Technical Notes

- SSE requires the Kratos session cookie to be sent with EventSource. Since the API is same-origin (`/api/v1/`), cookies are sent automatically — no extra config needed.
- Redis pub/sub subscriptions are cheap — one goroutine per connected client reading from a channel.
- Sound files should be small (<50KB). Web Audio API requires user gesture before first play — if the first playback attempt is blocked by the browser's autoplay policy, silently skip the sound and retry on the next user interaction (click/tap). No UI prompt needed.
- The `useLiveFeed` hook should be disabled/not connected when the user navigates away from the feed page (cleanup on unmount).
- When a user creates a post via `Compose.tsx` and navigates back to Home, the feed does a fresh fetch (existing behavior via `useFeed` mount). The SSE buffer should not duplicate this — the `useLiveFeed` hook skips SSE events for posts already present in the feed (compare by post ID).
- Feed cache (`FeedCache` in Redis) stores serialized `domain.Post` structs. Reaction counts are NOT included in the cached post — they are fetched separately and layered on in the handler. This avoids stale counts in cache and avoids invalidating the feed cache on every reaction.

## Out of Scope

- Multi-image posts (future enhancement)
- Push notifications / service worker
- User preferences backend (localStorage is sufficient for mute toggle)
- Vouch-based notification filtering
- Image compression/resizing on upload (serve as-is for now)
