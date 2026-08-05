package handler_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/handler"
	"github.com/fireynis/the-bell/internal/service"
)

// --- mock ReactionRepository ---

type mockReactionRepo struct {
	reactions map[string]*domain.Reaction // keyed by "userID:postID:type"
}

func newMockReactionRepo() *mockReactionRepo {
	return &mockReactionRepo{reactions: make(map[string]*domain.Reaction)}
}

func reactionKey(userID, postID string, rt domain.ReactionType) string {
	return userID + ":" + postID + ":" + string(rt)
}

func (m *mockReactionRepo) AddReaction(_ context.Context, reaction *domain.Reaction) error {
	key := reactionKey(reaction.UserID, reaction.PostID, reaction.Type)
	m.reactions[key] = reaction
	return nil
}

// Mirrors the real adapter: queries/reactions.sql RemoveReaction is a plain
// :exec DELETE, so removing something that is not there matches no rows and
// returns nil. This fake used to invent a "not found" error, and the handler
// test below asserted the fake's behaviour rather than the endpoint's — the
// production path could not produce a 404 at all.
func (m *mockReactionRepo) RemoveReaction(_ context.Context, userID, postID string, reactionType domain.ReactionType) error {
	delete(m.reactions, reactionKey(userID, postID, reactionType))
	return nil
}

func (m *mockReactionRepo) CountByPost(_ context.Context, postID string) (map[domain.ReactionType]int, error) {
	counts := make(map[domain.ReactionType]int)
	for _, r := range m.reactions {
		if r.PostID == postID {
			counts[r.Type]++
		}
	}
	return counts, nil
}

func (m *mockReactionRepo) GetUserReaction(_ context.Context, userID, postID string, reactionType domain.ReactionType) (*domain.Reaction, error) {
	key := reactionKey(userID, postID, reactionType)
	r, ok := m.reactions[key]
	if !ok {
		return nil, nil
	}
	return r, nil
}

func (m *mockReactionRepo) ListByPost(_ context.Context, postID string) ([]*domain.Reaction, error) {
	var result []*domain.Reaction
	for _, r := range m.reactions {
		if r.PostID == postID {
			result = append(result, r)
		}
	}
	return result, nil
}

// --- stub ReactionEventPublisher ---

type stubReactionPublisher struct {
	postIDs []string // one entry per call, in call order
	err     error
}

func (s *stubReactionPublisher) PublishReactionEvent(_ context.Context, postID, _, _, _ string) error {
	s.postIDs = append(s.postIDs, postID)
	return s.err
}

// --- test helpers ---

func newTestReactionService(repo service.ReactionRepository) *service.ReactionService {
	return service.NewReactionService(repo, func() time.Time { return fixedNow })
}

// captureLogs redirects the default slog logger into a buffer for the duration
// of one test. The handler package logs best-effort failures through the
// package-level slog functions, so that is the only place to observe them.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()

	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &buf
}

// withChiURLParams sets multiple chi URL params on a request without overwriting.
func withChiURLParams(r *http.Request, params map[string]string) *http.Request {
	rctx := chi.NewRouteContext()
	for k, v := range params {
		rctx.URLParams.Add(k, v)
	}
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

// --- Add reaction tests ---

func TestReactionHandler_Add(t *testing.T) {
	repo := newMockReactionRepo()
	svc := newTestReactionService(repo)
	h := handler.NewReactionHandler(svc, nil)

	body := `{"type":"bell"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/posts/post-1/reactions", strings.NewReader(body))
	req = withChiURLParam(req, "postId", "post-1")
	req = withUser(req, testUser())
	rec := httptest.NewRecorder()

	h.Add(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var reaction domain.Reaction
	decodeBody(t, rec, &reaction)
	if reaction.PostID != "post-1" {
		t.Errorf("post_id = %q, want %q", reaction.PostID, "post-1")
	}
	if reaction.Type != domain.ReactionBell {
		t.Errorf("type = %q, want %q", reaction.Type, domain.ReactionBell)
	}
	if reaction.UserID != "user-1" {
		t.Errorf("user_id = %q, want %q", reaction.UserID, "user-1")
	}
}

func TestReactionHandler_Add_InvalidType(t *testing.T) {
	repo := newMockReactionRepo()
	svc := newTestReactionService(repo)
	h := handler.NewReactionHandler(svc, nil)

	body := `{"type":"invalid"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/posts/post-1/reactions", strings.NewReader(body))
	req = withChiURLParam(req, "postId", "post-1")
	req = withUser(req, testUser())
	rec := httptest.NewRecorder()

	h.Add(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d; body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestReactionHandler_Add_NoUser(t *testing.T) {
	repo := newMockReactionRepo()
	svc := newTestReactionService(repo)
	h := handler.NewReactionHandler(svc, nil)

	body := `{"type":"bell"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/posts/post-1/reactions", strings.NewReader(body))
	req = withChiURLParam(req, "postId", "post-1")
	rec := httptest.NewRecorder()

	h.Add(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestReactionHandler_Add_InvalidJSON(t *testing.T) {
	repo := newMockReactionRepo()
	svc := newTestReactionService(repo)
	h := handler.NewReactionHandler(svc, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/posts/post-1/reactions", strings.NewReader(`{bad}`))
	req = withChiURLParam(req, "postId", "post-1")
	req = withUser(req, testUser())
	rec := httptest.NewRecorder()

	h.Add(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

// --- SSE notification tests ---

func TestReactionHandler_Add_PublishesEvent(t *testing.T) {
	postRepo := newMockPostRepo()
	postRepo.posts["post-1"] = &domain.Post{ID: "post-1", AuthorID: "author-9", Status: domain.PostVisible, CreatedAt: fixedNow}
	pub := &stubReactionPublisher{}
	h := handler.NewReactionHandler(
		newTestReactionService(newMockReactionRepo()),
		newTestPostService(postRepo),
		handler.WithReactionPublisher(pub),
	)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/posts/post-1/reactions", strings.NewReader(`{"type":"bell"}`))
	req = withChiURLParam(req, "postId", "post-1")
	req = withUser(req, testUser())
	rec := httptest.NewRecorder()

	h.Add(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if len(pub.postIDs) != 1 || pub.postIDs[0] != "post-1" {
		t.Errorf("publisher calls = %v, want exactly one for post-1", pub.postIDs)
	}
}

// A notification that cannot be delivered must not fail the reaction, but it
// must leave a trace — this used to be discarded with `_ =`, so a silently
// broken SSE pipeline looked identical to a healthy one.
func TestReactionHandler_Add_PublishFailureIsLoggedNotReturned(t *testing.T) {
	logs := captureLogs(t)
	postRepo := newMockPostRepo()
	postRepo.posts["post-1"] = &domain.Post{ID: "post-1", AuthorID: "author-9", Status: domain.PostVisible, CreatedAt: fixedNow}
	pub := &stubReactionPublisher{err: errors.New("sse broker unavailable")}
	h := handler.NewReactionHandler(
		newTestReactionService(newMockReactionRepo()),
		newTestPostService(postRepo),
		handler.WithReactionPublisher(pub),
	)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/posts/post-1/reactions", strings.NewReader(`{"type":"bell"}`))
	req = withChiURLParam(req, "postId", "post-1")
	req = withUser(req, testUser())
	rec := httptest.NewRecorder()

	h.Add(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := logs.String(); !strings.Contains(got, "publishing reaction event") {
		t.Errorf("nothing logged about the failed publish; logs: %s", got)
	}
	if got := logs.String(); !strings.Contains(got, "sse broker unavailable") {
		t.Errorf("log did not carry the underlying error; logs: %s", got)
	}
}

// Same contract when the post lookup fails: the reaction stands, the missed
// notification is recorded, and no event is published for a post we could not
// read the author of.
func TestReactionHandler_Add_PostLookupFailureIsLoggedNotReturned(t *testing.T) {
	logs := captureLogs(t)
	pub := &stubReactionPublisher{}
	h := handler.NewReactionHandler(
		newTestReactionService(newMockReactionRepo()),
		newTestPostService(newMockPostRepo()), // post-1 is absent -> ErrNotFound
		handler.WithReactionPublisher(pub),
	)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/posts/post-1/reactions", strings.NewReader(`{"type":"bell"}`))
	req = withChiURLParam(req, "postId", "post-1")
	req = withUser(req, testUser())
	rec := httptest.NewRecorder()

	h.Add(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if len(pub.postIDs) != 0 {
		t.Errorf("published %v despite the post lookup failing", pub.postIDs)
	}
	if got := logs.String(); !strings.Contains(got, "loading post to notify its author") {
		t.Errorf("nothing logged about the failed lookup; logs: %s", got)
	}
}

// --- Remove reaction tests ---

func TestReactionHandler_Remove(t *testing.T) {
	repo := newMockReactionRepo()
	// Seed a reaction to remove.
	repo.reactions[reactionKey("user-1", "post-1", domain.ReactionBell)] = &domain.Reaction{
		ID:        "r-1",
		UserID:    "user-1",
		PostID:    "post-1",
		Type:      domain.ReactionBell,
		CreatedAt: fixedNow,
	}
	svc := newTestReactionService(repo)
	h := handler.NewReactionHandler(svc, nil)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/posts/post-1/reactions/bell", nil)
	req = withChiURLParams(req, map[string]string{"postId": "post-1", "type": "bell"})
	req = withUser(req, testUser())
	rec := httptest.NewRecorder()

	h.Remove(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d; body: %s", rec.Code, http.StatusNoContent, rec.Body.String())
	}
}

func TestReactionHandler_Remove_InvalidType(t *testing.T) {
	repo := newMockReactionRepo()
	svc := newTestReactionService(repo)
	h := handler.NewReactionHandler(svc, nil)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/posts/post-1/reactions/invalid", nil)
	req = withChiURLParams(req, map[string]string{"postId": "post-1", "type": "invalid"})
	req = withUser(req, testUser())
	rec := httptest.NewRecorder()

	h.Remove(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d; body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

// Removing a reaction the user never left is a no-op, not an error: a reaction
// is a toggle, so a double-tap or a retried request is ordinary use and 204 is
// the retry-safe answer. It also matches what the database actually does — the
// DELETE matches no rows and reports nothing.
//
// A 404 here would be actively worse than useless: the frontend reaction button
// reverts its optimistic toggle on any error, so the reaction would snap back to
// "present" in the UI when the server has none.
func TestReactionHandler_Remove_NotPresentIsNoContent(t *testing.T) {
	repo := newMockReactionRepo()
	svc := newTestReactionService(repo)
	h := handler.NewReactionHandler(svc, nil)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/posts/post-1/reactions/bell", nil)
	req = withChiURLParams(req, map[string]string{"postId": "post-1", "type": "bell"})
	req = withUser(req, testUser())
	rec := httptest.NewRecorder()

	h.Remove(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d; body: %s", rec.Code, http.StatusNoContent, rec.Body.String())
	}
}

// Removing the same reaction twice is the same as removing it once. The second
// call is what a retry or a double-tap looks like.
func TestReactionHandler_Remove_IsIdempotent(t *testing.T) {
	repo := newMockReactionRepo()
	svc := newTestReactionService(repo)
	h := handler.NewReactionHandler(svc, nil)

	add := httptest.NewRequest(http.MethodPost, "/api/v1/posts/post-1/reactions", strings.NewReader(`{"type":"bell"}`))
	add = withChiURLParams(add, map[string]string{"postId": "post-1"})
	add = withUser(add, testUser())
	h.Add(httptest.NewRecorder(), add)

	for attempt := 1; attempt <= 2; attempt++ {
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/posts/post-1/reactions/bell", nil)
		req = withChiURLParams(req, map[string]string{"postId": "post-1", "type": "bell"})
		req = withUser(req, testUser())
		rec := httptest.NewRecorder()

		h.Remove(rec, req)

		if rec.Code != http.StatusNoContent {
			t.Errorf("attempt %d: status = %d, want %d; body: %s", attempt, rec.Code, http.StatusNoContent, rec.Body.String())
		}
	}
}

func TestReactionHandler_Remove_NoUser(t *testing.T) {
	repo := newMockReactionRepo()
	svc := newTestReactionService(repo)
	h := handler.NewReactionHandler(svc, nil)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/posts/post-1/reactions/bell", nil)
	req = withChiURLParams(req, map[string]string{"postId": "post-1", "type": "bell"})
	rec := httptest.NewRecorder()

	h.Remove(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}
