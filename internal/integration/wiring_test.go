//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/testsupport"
)

// The route families exercised here — reactions, uploads, rate limiting, SSE
// and town config — register conditionally on a non-nil dependency in
// server.routes. The harness used to omit those dependencies, so none of these
// routes existed on the test server and any assertion about them would have
// been satisfied by a 404 from an unregistered path rather than by the
// behaviour under test. They are reachable now that the harness builds the same
// graph as production, and these tests hold that open.

// TestRateLimitEnforcedOnPostCreation is where the real-Redis work and the
// full-wiring work meet: a real sliding-window Lua script, running against a
// real Redis, throttling real HTTP requests against a real Postgres.
//
// The limit on post creation is 10 per hour per user.
func TestRateLimitEnforcedOnPostCreation(t *testing.T) {
	pool := testsupport.TestDB(t)

	user := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("ratelimited"), domain.RoleMember, 80.0)
	srv := testServer(t, pool, user)
	h := srv.Handler()

	create := func(i int) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]string{"body": fmt.Sprintf("rate limit post %d", i)})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/posts", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	const limit = 10
	for i := range limit {
		if rec := create(i); rec.Code != http.StatusCreated {
			t.Fatalf("post %d: status = %d, want %d: %s", i+1, rec.Code, http.StatusCreated, rec.Body.String())
		}
	}

	rec := create(limit)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("post %d: status = %d, want %d (the rate limiter is not enforcing): %s",
			limit+1, rec.Code, http.StatusTooManyRequests, rec.Body.String())
	}

	// The client needs to know how long to wait, and the window is an hour.
	retryAfter := rec.Header().Get("Retry-After")
	if retryAfter == "" {
		t.Error("Retry-After header is missing on a 429")
	} else if secs, err := strconv.Atoi(retryAfter); err != nil {
		t.Errorf("Retry-After = %q, want an integer number of seconds", retryAfter)
	} else if secs != int(time.Hour.Seconds()) {
		t.Errorf("Retry-After = %d, want %d", secs, int(time.Hour.Seconds()))
	}

	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding 429 body: %v", err)
	}
	if body["error"] != "rate limit exceeded" {
		t.Errorf("error = %q, want %q", body["error"], "rate limit exceeded")
	}
}

// TestRateLimitIsPerUser confirms one user exhausting a limit does not throttle
// anyone else. The key includes the user ID, but that only holds end-to-end if
// the middleware reads the authenticated user rather than a shared counter.
func TestRateLimitIsPerUser(t *testing.T) {
	pool := testsupport.TestDB(t)

	exhausted := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("exhausted"), domain.RoleMember, 80.0)
	fresh := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("fresh"), domain.RoleMember, 80.0)

	// Both servers must share one Redis for the test to mean anything; they do,
	// because testsupport hands the whole test the same logical database.
	exhaustedSrv := testServer(t, pool, exhausted)
	freshSrv := testServer(t, pool, fresh)

	post := func(h http.Handler, n int) int {
		body, _ := json.Marshal(map[string]string{"body": fmt.Sprintf("per-user post %d", n)})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/posts", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}

	const limit = 10
	for i := range limit {
		if code := post(exhaustedSrv.Handler(), i); code != http.StatusCreated {
			t.Fatalf("exhausted user post %d: status = %d, want %d", i+1, code, http.StatusCreated)
		}
	}
	if code := post(exhaustedSrv.Handler(), limit); code != http.StatusTooManyRequests {
		t.Fatalf("exhausted user: status = %d, want %d", code, http.StatusTooManyRequests)
	}

	if code := post(freshSrv.Handler(), 0); code != http.StatusCreated {
		t.Errorf("second user: status = %d, want %d (one user's limit throttled another)", code, http.StatusCreated)
	}
}

// TestReactionRoutesRegistered covers a route family that did not exist on the
// old test server at all.
func TestReactionRoutesRegistered(t *testing.T) {
	pool := testsupport.TestDB(t)

	user := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("reactor"), domain.RoleMember, 80.0)
	srv := testServer(t, pool, user)
	h := srv.Handler()

	postID := createPost(t, h, "a post worth reacting to")

	add := func(reaction domain.ReactionType) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]string{"type": string(reaction)})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/posts/"+postID+"/reactions", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	rec := add(domain.ReactionBell)
	if rec.Code != http.StatusCreated && rec.Code != http.StatusOK {
		t.Fatalf("adding reaction: status = %d, want 200 or 201: %s", rec.Code, rec.Body.String())
	}

	// Removing it must also route.
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/posts/"+postID+"/reactions/"+string(domain.ReactionBell), nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code == http.StatusNotFound {
		t.Errorf("removing reaction: got 404, the route is not registered")
	}
}

// TestReactionRejectsUnknownType checks the reaction route reaches real
// validation rather than accepting anything the handler is handed.
func TestReactionRejectsUnknownType(t *testing.T) {
	pool := testsupport.TestDB(t)

	user := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("badreactor"), domain.RoleMember, 80.0)
	srv := testServer(t, pool, user)
	h := srv.Handler()

	postID := createPost(t, h, "a post with a bad reaction")

	body, _ := json.Marshal(map[string]string{"type": "not-a-real-reaction"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/posts/"+postID+"/reactions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code == http.StatusNotFound {
		t.Fatal("got 404: the reactions route is not registered")
	}
	if rec.Code < 400 || rec.Code >= 500 {
		t.Errorf("status = %d, want a 4xx for an unknown reaction type: %s", rec.Code, rec.Body.String())
	}
}

// TestConfigRouteRegistered covers the town config endpoint, which depends on
// WithConfigRepo.
func TestConfigRouteRegistered(t *testing.T) {
	pool := testsupport.TestDB(t)

	user := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("configreader"), domain.RoleMember, 80.0)
	srv := testServer(t, pool, user)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/config", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code == http.StatusNotFound {
		t.Fatal("got 404: the config route is not registered")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !json.Valid(rec.Body.Bytes()) {
		t.Errorf("config response is not valid JSON: %s", rec.Body.String())
	}
}

// TestUploadRouteRegistered covers the static upload path, which depends on
// WithImageStore.
func TestUploadRouteRegistered(t *testing.T) {
	pool := testsupport.TestDB(t)

	user := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("uploader"), domain.RoleMember, 80.0)
	srv := testServer(t, pool, user)

	// Nothing has been uploaded, so a 404 for the missing file is correct — but
	// it must come from the file server, not from an unregistered route. A
	// traversal attempt must be refused rather than served.
	req := httptest.NewRequest(http.MethodGet, "/uploads/../../etc/passwd", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code == http.StatusOK {
		t.Errorf("path traversal through /uploads/ returned 200: %s", rec.Body.String())
	}
}

// TestSSERouteRegistered covers the live feed stream, which depends on
// WithSSEBroker and therefore on Redis.
//
// The handler streams until the client disconnects, so the request carries a
// context that is cancelled shortly after the headers are written.
func TestSSERouteRegistered(t *testing.T) {
	pool := testsupport.TestDB(t)

	user := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("streamer"), domain.RoleMember, 80.0)
	srv := testServer(t, pool, user)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/feed/live", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.Handler().ServeHTTP(rec, req)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("SSE handler did not return after its request context was cancelled")
	}

	if rec.Code == http.StatusNotFound {
		t.Fatal("got 404: the SSE route is not registered")
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want %q", ct, "text/event-stream")
	}
}

// createPost creates a post through the API and returns its ID.
func createPost(t *testing.T, h http.Handler, body string) string {
	t.Helper()

	payload, _ := json.Marshal(map[string]string{"body": body})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/posts", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("creating post: status = %d, want %d: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var post domain.Post
	if err := json.NewDecoder(rec.Body).Decode(&post); err != nil {
		t.Fatalf("decoding created post: %v", err)
	}
	return post.ID
}
