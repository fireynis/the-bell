package handler_test

import (
	"context"
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

func (m *mockReactionRepo) RemoveReaction(_ context.Context, userID, postID string, reactionType domain.ReactionType) error {
	key := reactionKey(userID, postID, reactionType)
	if _, ok := m.reactions[key]; !ok {
		return service.ErrReactionNotFound
	}
	delete(m.reactions, key)
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

// --- test helpers ---

func newTestReactionService(repo service.ReactionRepository) *service.ReactionService {
	return service.NewReactionService(repo, func() time.Time { return fixedNow })
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
