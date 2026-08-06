//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/testsupport"
)

// feedCacheKey mirrors feedKey in internal/cache/feed.go, which is unexported.
const feedCacheKey = "feed:latest"

// mustJSON marshals a request body, failing the test rather than the request.
func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshalling request body: %v", err)
	}
	return data
}

// removePost issues the moderator removal and returns the recorder.
func removePost(t *testing.T, h http.Handler, postID, reason string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/moderation/posts/"+postID+"/remove",
		bytes.NewReader(mustJSON(t, map[string]string{"reason": reason})))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// feedPostIDs reads the feed over HTTP.
func feedPostIDs(t *testing.T, h http.Handler) []string {
	t.Helper()

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/posts", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("listing feed: status %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Posts []domain.Post `json:"posts"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding feed: %v", err)
	}

	ids := make([]string, len(resp.Posts))
	for i, p := range resp.Posts {
		ids[i] = p.ID
	}
	return ids
}

// awaitWarmFeedCache blocks until the feed cache holds an entry.
//
// GetFeed serves a cold read from Postgres and warms Redis in a background
// goroutine, so a test that asserted on the cache immediately after the first
// read would race it and pass whether or not invalidation works.
func awaitWarmFeedCache(t *testing.T, rdb *redis.Client) {
	t.Helper()
	ctx := context.Background()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		n, err := rdb.ZCard(ctx, feedCacheKey).Result()
		if err != nil {
			t.Fatalf("reading feed cache: %v", err)
		}
		if n > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("feed cache never warmed; the invalidation assertion would be meaningless")
}

// A moderator removing a post is the whole point of the feature: before it
// existed the only remedy was an action against the author, and the offending
// post stayed up. This drives the real route against real Postgres and real
// Redis, because the feed cache is where a removal most plausibly fails to take
// effect — the cache holds whole posts, so one left in the sorted set keeps
// being served to the entire town.
func TestModeratorRemovesAPostAndTheCachedFeedStopsServingIt(t *testing.T) {
	pool := testsupport.TestDB(t)
	rdb := testsupport.TestRedis(t)

	author := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("removal-author"), domain.RoleMember, 80)
	moderator := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("removal-mod"), domain.RoleModerator, 90)

	authorH := testServerWithRedis(t, pool, rdb, author).Handler()
	modH := testServerWithRedis(t, pool, rdb, moderator).Handler()

	postID := createPost(t, authorH, "something the town should not have to read")

	// Read the feed so the cache warms, then wait for the background rebuild.
	if !slices.Contains(feedPostIDs(t, modH), postID) {
		t.Fatalf("post %s is not in the feed before removal", postID)
	}
	awaitWarmFeedCache(t, rdb)

	if w := removePost(t, modH, postID, "harassment of another member"); w.Code != http.StatusNoContent {
		t.Fatalf("removal: status %d, want %d: %s", w.Code, http.StatusNoContent, w.Body.String())
	}

	// The direct assertion: the key is gone, so the next read rebuilds from
	// Postgres rather than replaying the removed post out of Redis.
	exists, err := rdb.Exists(context.Background(), feedCacheKey).Result()
	if err != nil {
		t.Fatalf("checking feed cache: %v", err)
	}
	if exists != 0 {
		t.Error("feed cache survived the removal; the removed post keeps serving from Redis")
	}

	if ids := feedPostIDs(t, modH); slices.Contains(ids, postID) {
		t.Errorf("removed post %s is still in the feed: %v", postID, ids)
	}

	// The audit trail, read straight from the row: who removed it and why.
	var status, reason string
	var removedBy *string
	err = pool.QueryRow(context.Background(),
		`SELECT status, removal_reason, removed_by FROM posts WHERE id = $1`, postID,
	).Scan(&status, &reason, &removedBy)
	if err != nil {
		t.Fatalf("reading the removed post back: %v", err)
	}
	if status != string(domain.PostRemovedByMod) {
		t.Errorf("status = %q, want %q", status, domain.PostRemovedByMod)
	}
	if reason != "harassment of another member" {
		t.Errorf("removal_reason = %q, want the moderator's note", reason)
	}
	if removedBy == nil {
		t.Error("removed_by is NULL; the removal cannot be attributed to anyone")
	} else if *removedBy != moderator.ID {
		t.Errorf("removed_by = %q, want the acting moderator %q", *removedBy, moderator.ID)
	}
}

// An author deleting their own post goes through the same column and must
// leave it NULL — there is no moderator, and removed_by is a foreign key to
// users, so the empty string is not an available stand-in.
func TestAuthorDeletionLeavesRemovedByNull(t *testing.T) {
	pool := testsupport.TestDB(t)
	rdb := testsupport.TestRedis(t)

	author := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("selfdel-author"), domain.RoleMember, 80)
	authorH := testServerWithRedis(t, pool, rdb, author).Handler()

	postID := createPost(t, authorH, "the author changes their mind")

	w := httptest.NewRecorder()
	authorH.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/api/v1/posts/"+postID, nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("author delete: status %d: %s", w.Code, w.Body.String())
	}

	var removedBy *string
	if err := pool.QueryRow(context.Background(),
		`SELECT removed_by FROM posts WHERE id = $1`, postID).Scan(&removedBy); err != nil {
		t.Fatalf("reading removed_by: %v", err)
	}
	if removedBy != nil {
		t.Errorf("removed_by = %q on an author deletion, want NULL", *removedBy)
	}
}

func TestModeratorRemovedPostStaysVisibleToModeratorsAndVanishesForEveryoneElse(t *testing.T) {
	pool := testsupport.TestDB(t)
	rdb := testsupport.TestRedis(t)

	author := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("vis-author"), domain.RoleMember, 80)
	moderator := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("vis-mod"), domain.RoleModerator, 90)
	stranger := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("vis-stranger"), domain.RoleMember, 80)

	authorH := testServerWithRedis(t, pool, rdb, author).Handler()
	modH := testServerWithRedis(t, pool, rdb, moderator).Handler()
	strangerH := testServerWithRedis(t, pool, rdb, stranger).Handler()

	postID := createPost(t, authorH, "the reported post")
	if w := removePost(t, modH, postID, "off topic; second warning"); w.Code != http.StatusNoContent {
		t.Fatalf("removal: status %d: %s", w.Code, w.Body.String())
	}

	get := func(h http.Handler) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/posts/"+postID, nil))
		return w
	}

	// The moderation queue fetches the reported post through this same public
	// route, so a removal that hid the post from moderators too would blind the
	// queue to the very post being reviewed.
	if w := get(modH); w.Code != http.StatusOK {
		t.Errorf("moderator GET after removal: status %d, want 200: %s", w.Code, w.Body.String())
	}
	if w := get(authorH); w.Code != http.StatusOK {
		t.Errorf("author GET after removal: status %d, want 200: %s", w.Code, w.Body.String())
	}
	if w := get(strangerH); w.Code != http.StatusNotFound {
		t.Errorf("stranger GET after removal: status %d, want 404", w.Code)
	}
	if w := get(anonymousTestServer(t, pool, rdb).Handler()); w.Code != http.StatusNotFound {
		t.Errorf("anonymous GET after removal: status %d, want 404", w.Code)
	}
}

// The removal endpoint is the first thing that ever writes a real removal
// reason, so it is the first thing that makes the leak the `json:"-"` tag
// prevents actually matter. Every response that can carry the post afterwards
// is checked against the real stack.
func TestModeratorRemovalReasonNeverReachesTheWire(t *testing.T) {
	pool := testsupport.TestDB(t)
	rdb := testsupport.TestRedis(t)

	const secret = "private note: third strike for harassment"

	author := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("leak-author"), domain.RoleMember, 80)
	moderator := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("leak-mod"), domain.RoleModerator, 90)

	authorH := testServerWithRedis(t, pool, rdb, author).Handler()
	modH := testServerWithRedis(t, pool, rdb, moderator).Handler()

	postID := createPost(t, authorH, "the offending post")

	removal := removePost(t, modH, postID, secret)
	if removal.Code != http.StatusNoContent {
		t.Fatalf("removal: status %d: %s", removal.Code, removal.Body.String())
	}

	bodies := map[string]string{
		"the removal response itself": removal.Body.String(),
	}

	for name, h := range map[string]http.Handler{
		"the moderator's GET": modH,
		"the author's GET":    authorH,
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/posts/"+postID, nil))
		bodies[name] = w.Body.String()
	}

	w := httptest.NewRecorder()
	modH.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/users/"+author.ID+"/posts?limit=20", nil))
	bodies["the author's post listing"] = w.Body.String()

	for name, body := range bodies {
		if strings.Contains(body, secret) {
			t.Errorf("%s leaked the moderator's note: %s", name, body)
		}
		if strings.Contains(body, "removal_reason") {
			t.Errorf("%s carries a removal_reason key: %s", name, body)
		}
		// removed_by is moderation metadata of the same kind. Checked by
		// decoded key where possible, but these bodies are collections as well
		// as single posts, so the moderator's id is the reliable probe: a
		// substring search for "removed_by" would match the status value
		// "removed_by_mod" on every removed post and pass for the wrong reason.
		if strings.Contains(body, moderator.ID) {
			t.Errorf("%s named the removing moderator: %s", name, body)
		}
	}
}

func TestModeratorRemovalRefusals(t *testing.T) {
	pool := testsupport.TestDB(t)
	rdb := testsupport.TestRedis(t)

	author := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("refuse-author"), domain.RoleMember, 80)
	moderator := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("refuse-mod"), domain.RoleModerator, 90)
	member := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("refuse-member"), domain.RoleMember, 80)
	council := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("refuse-council"), domain.RoleCouncil, 90)

	authorH := testServerWithRedis(t, pool, rdb, author).Handler()
	modH := testServerWithRedis(t, pool, rdb, moderator).Handler()
	memberH := testServerWithRedis(t, pool, rdb, member).Handler()
	councilH := testServerWithRedis(t, pool, rdb, council).Handler()

	t.Run("a member is refused by the route guard", func(t *testing.T) {
		postID := createPost(t, authorH, "a member must not be able to remove this")

		if w := removePost(t, memberH, postID, "spam"); w.Code != http.StatusForbidden {
			t.Errorf("status = %d, want %d: %s", w.Code, http.StatusForbidden, w.Body.String())
		}

		// And the post is genuinely untouched, not merely reported as refused.
		if !slices.Contains(feedPostIDs(t, memberH), postID) {
			t.Error("post left the feed despite the refusal")
		}
	})

	t.Run("the council may remove", func(t *testing.T) {
		postID := createPost(t, authorH, "council removes this one")

		if w := removePost(t, councilH, postID, "off topic"); w.Code != http.StatusNoContent {
			t.Errorf("status = %d, want %d: %s", w.Code, http.StatusNoContent, w.Body.String())
		}
	})

	t.Run("a blank reason is refused", func(t *testing.T) {
		postID := createPost(t, authorH, "no reason given")

		if w := removePost(t, modH, postID, "   "); w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want %d: %s", w.Code, http.StatusBadRequest, w.Body.String())
		}
	})

	t.Run("an unknown post is not found", func(t *testing.T) {
		w := removePost(t, modH, "01930000-0000-7000-8000-00000000dead", "spam")
		if w.Code != http.StatusNotFound {
			t.Errorf("status = %d, want %d: %s", w.Code, http.StatusNotFound, w.Body.String())
		}
	})

	// Two moderators working the queue can reach the same report. The second
	// removal must not overwrite the first one's note.
	t.Run("a second removal is refused and leaves the first note intact", func(t *testing.T) {
		postID := createPost(t, authorH, "removed twice")

		if w := removePost(t, modH, postID, "the first note"); w.Code != http.StatusNoContent {
			t.Fatalf("first removal: status %d: %s", w.Code, w.Body.String())
		}
		if w := removePost(t, modH, postID, "the second note"); w.Code != http.StatusBadRequest {
			t.Errorf("second removal: status %d, want %d: %s", w.Code, http.StatusBadRequest, w.Body.String())
		}

		var status, reason string
		err := pool.QueryRow(context.Background(),
			`SELECT status, removal_reason FROM posts WHERE id = $1`, postID).Scan(&status, &reason)
		if err != nil {
			t.Fatalf("reading post back: %v", err)
		}
		if status != string(domain.PostRemovedByMod) {
			t.Errorf("status = %q, want %q", status, domain.PostRemovedByMod)
		}
		if reason != "the first note" {
			t.Errorf("removal_reason = %q, want the first note preserved", reason)
		}
	})

	// An author deleting their own post records removed_by_author. A moderator
	// removal afterwards would rewrite that, erasing the fact the author acted
	// first.
	t.Run("a post the author already deleted keeps its own status", func(t *testing.T) {
		postID := createPost(t, authorH, "the author thinks better of it")

		w := httptest.NewRecorder()
		authorH.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/api/v1/posts/"+postID, nil))
		if w.Code != http.StatusNoContent {
			t.Fatalf("author delete: status %d: %s", w.Code, w.Body.String())
		}

		if w := removePost(t, modH, postID, "too late"); w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want %d: %s", w.Code, http.StatusBadRequest, w.Body.String())
		}

		var status string
		if err := pool.QueryRow(context.Background(),
			`SELECT status FROM posts WHERE id = $1`, postID).Scan(&status); err != nil {
			t.Fatalf("reading post back: %v", err)
		}
		if status != string(domain.PostRemovedByAuthor) {
			t.Errorf("status = %q, want %q", status, domain.PostRemovedByAuthor)
		}
	})
}

// The public feed and single-post reads personalize their response through
// OptionalAuth: reaction_counts is the same for everyone, but user_reactions is
// the caller's own. Until the harness overrode OptionalAuth every integration
// test looked anonymous on these routes, so this branch ran but was never
// verified with a real identity. It is asserted here so the harness fix cannot
// quietly regress.
func TestPublicPostReadsCarryTheCallersOwnReactions(t *testing.T) {
	pool := testsupport.TestDB(t)
	rdb := testsupport.TestRedis(t)

	author := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("react-author"), domain.RoleMember, 80)
	reader := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("react-reader"), domain.RoleMember, 80)

	authorH := testServerWithRedis(t, pool, rdb, author).Handler()
	readerH := testServerWithRedis(t, pool, rdb, reader).Handler()

	postID := createPost(t, authorH, "react to this")

	react := httptest.NewRequest(http.MethodPost, "/api/v1/posts/"+postID+"/reactions",
		bytes.NewReader(mustJSON(t, map[string]string{"type": string(domain.ReactionBell)})))
	react.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	readerH.ServeHTTP(w, react)
	if w.Code != http.StatusCreated && w.Code != http.StatusOK {
		t.Fatalf("adding reaction: status %d: %s", w.Code, w.Body.String())
	}

	get := func(h http.Handler) domain.Post {
		t.Helper()
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/posts/"+postID, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET post: status %d: %s", rec.Code, rec.Body.String())
		}
		var p domain.Post
		if err := json.NewDecoder(rec.Body).Decode(&p); err != nil {
			t.Fatalf("decoding post: %v", err)
		}
		return p
	}

	// The reader sees their own reaction.
	if got := get(readerH); !slices.Contains(got.UserReactions, domain.ReactionBell) {
		t.Errorf("reader's user_reactions = %v, want it to contain %q; OptionalAuth did not resolve the caller",
			got.UserReactions, domain.ReactionBell)
	}

	// The author did not react, so theirs is empty — but the count is shared,
	// which is what separates "not resolved" from "resolved and genuinely none".
	authorView := get(authorH)
	if slices.Contains(authorView.UserReactions, domain.ReactionBell) {
		t.Errorf("author's user_reactions = %v, want empty — that is the reader's reaction",
			authorView.UserReactions)
	}
	if authorView.ReactionCounts[domain.ReactionBell] != 1 {
		t.Errorf("reaction_counts[%q] = %d, want 1", domain.ReactionBell,
			authorView.ReactionCounts[domain.ReactionBell])
	}

	// An anonymous reader gets the counts and none of the personalization.
	anon := get(anonymousTestServer(t, pool, rdb).Handler())
	if len(anon.UserReactions) != 0 {
		t.Errorf("anonymous user_reactions = %v, want empty", anon.UserReactions)
	}
	if anon.ReactionCounts[domain.ReactionBell] != 1 {
		t.Errorf("anonymous reaction_counts[%q] = %d, want 1", domain.ReactionBell,
			anon.ReactionCounts[domain.ReactionBell])
	}
}
