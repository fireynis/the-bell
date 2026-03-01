package handler_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/handler"
	"github.com/fireynis/the-bell/internal/middleware"
	"github.com/fireynis/the-bell/internal/service"
)

// --- mock PostRepository ---

type mockPostRepo struct {
	posts map[string]*domain.Post
}

func newMockPostRepo() *mockPostRepo {
	return &mockPostRepo{posts: make(map[string]*domain.Post)}
}

func (m *mockPostRepo) CreatePost(_ context.Context, post *domain.Post) error {
	m.posts[post.ID] = post
	return nil
}

func (m *mockPostRepo) GetPostByID(_ context.Context, id string) (*domain.Post, error) {
	p, ok := m.posts[id]
	if !ok {
		return nil, service.ErrNotFound
	}
	return p, nil
}

func (m *mockPostRepo) ListPosts(_ context.Context, cursor string, limit int) ([]*domain.Post, error) {
	var result []*domain.Post
	for _, p := range m.posts {
		if p.Status != domain.PostVisible {
			continue
		}
		if cursor != "" && p.ID >= cursor {
			continue
		}
		result = append(result, p)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].ID > result[j].ID
	})
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (m *mockPostRepo) UpdatePostBody(_ context.Context, id string, body string) (*domain.Post, error) {
	p, ok := m.posts[id]
	if !ok {
		return nil, service.ErrNotFound
	}
	p.Body = body
	now := time.Now()
	p.EditedAt = &now
	return p, nil
}

func (m *mockPostRepo) UpdatePostStatus(_ context.Context, id string, status domain.PostStatus, reason string) error {
	p, ok := m.posts[id]
	if !ok {
		return service.ErrNotFound
	}
	p.Status = status
	p.RemovalReason = reason
	return nil
}

// --- test helpers ---

var fixedNow = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

func testUser() *domain.User {
	return &domain.User{
		ID:       "user-1",
		Role:     domain.RoleMember,
		IsActive: true,
	}
}

func newTestPostService(repo service.PostRepository) *service.PostService {
	return service.NewPostService(repo, service.WithClock(func() time.Time { return fixedNow }))
}

func withChiURLParam(r *http.Request, key, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func withUser(r *http.Request, user *domain.User) *http.Request {
	return r.WithContext(middleware.WithUser(r.Context(), user))
}

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder, dst any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), dst); err != nil {
		t.Fatalf("failed to decode response: %v (body: %s)", err, rec.Body.String())
	}
}

// --- Create tests ---

func TestPostHandler_Create(t *testing.T) {
	repo := newMockPostRepo()
	svc := newTestPostService(repo)
	h := handler.NewPostHandler(svc)

	body := `{"body":"Hello, world!","image_path":"/img/photo.jpg"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/posts", strings.NewReader(body))
	req = withUser(req, testUser())
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusCreated)
	}

	var post domain.Post
	decodeBody(t, rec, &post)
	if post.ID == "" {
		t.Error("expected non-empty post ID")
	}
	if post.Body != "Hello, world!" {
		t.Errorf("body = %q, want %q", post.Body, "Hello, world!")
	}
	if post.AuthorID != "user-1" {
		t.Errorf("author_id = %q, want %q", post.AuthorID, "user-1")
	}
}

func TestPostHandler_Create_NoUser(t *testing.T) {
	repo := newMockPostRepo()
	svc := newTestPostService(repo)
	h := handler.NewPostHandler(svc)

	body := `{"body":"Hello"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/posts", strings.NewReader(body))
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestPostHandler_Create_EmptyBody(t *testing.T) {
	repo := newMockPostRepo()
	svc := newTestPostService(repo)
	h := handler.NewPostHandler(svc)

	body := `{"body":""}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/posts", strings.NewReader(body))
	req = withUser(req, testUser())
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestPostHandler_Create_BodyTooLong(t *testing.T) {
	repo := newMockPostRepo()
	svc := newTestPostService(repo)
	h := handler.NewPostHandler(svc)

	longBody := fmt.Sprintf(`{"body":"%s"}`, strings.Repeat("a", domain.MaxPostBodyLength+1))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/posts", strings.NewReader(longBody))
	req = withUser(req, testUser())
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestPostHandler_Create_InvalidJSON(t *testing.T) {
	repo := newMockPostRepo()
	svc := newTestPostService(repo)
	h := handler.NewPostHandler(svc)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/posts", strings.NewReader(`{invalid`))
	req = withUser(req, testUser())
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

// --- GetByID tests ---

func TestPostHandler_GetByID(t *testing.T) {
	repo := newMockPostRepo()
	repo.posts["post-1"] = &domain.Post{
		ID:        "post-1",
		AuthorID:  "user-1",
		Body:      "test post",
		Status:    domain.PostVisible,
		CreatedAt: fixedNow,
	}
	svc := newTestPostService(repo)
	h := handler.NewPostHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/posts/post-1", nil)
	req = withChiURLParam(req, "id", "post-1")
	rec := httptest.NewRecorder()

	h.GetByID(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var post domain.Post
	decodeBody(t, rec, &post)
	if post.ID != "post-1" {
		t.Errorf("id = %q, want %q", post.ID, "post-1")
	}
}

func TestPostHandler_GetByID_NotFound(t *testing.T) {
	repo := newMockPostRepo()
	svc := newTestPostService(repo)
	h := handler.NewPostHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/posts/nonexistent", nil)
	req = withChiURLParam(req, "id", "nonexistent")
	rec := httptest.NewRecorder()

	h.GetByID(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

// --- ListFeed tests ---

func TestPostHandler_ListFeed(t *testing.T) {
	repo := newMockPostRepo()
	repo.posts["c"] = &domain.Post{ID: "c", Status: domain.PostVisible, CreatedAt: fixedNow}
	repo.posts["b"] = &domain.Post{ID: "b", Status: domain.PostVisible, CreatedAt: fixedNow}
	repo.posts["a"] = &domain.Post{ID: "a", Status: domain.PostVisible, CreatedAt: fixedNow}
	svc := newTestPostService(repo)
	h := handler.NewPostHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/posts?limit=2", nil)
	rec := httptest.NewRecorder()

	h.ListFeed(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp struct {
		Posts      []domain.Post `json:"posts"`
		NextCursor string        `json:"next_cursor"`
	}
	decodeBody(t, rec, &resp)

	if len(resp.Posts) != 2 {
		t.Fatalf("got %d posts, want 2", len(resp.Posts))
	}
	if resp.NextCursor == "" {
		t.Error("expected next_cursor to be set when results == limit")
	}
}

func TestPostHandler_ListFeed_Empty(t *testing.T) {
	repo := newMockPostRepo()
	svc := newTestPostService(repo)
	h := handler.NewPostHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/posts", nil)
	rec := httptest.NewRecorder()

	h.ListFeed(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp struct {
		Posts      []json.RawMessage `json:"posts"`
		NextCursor string            `json:"next_cursor"`
	}
	decodeBody(t, rec, &resp)

	if resp.Posts == nil {
		t.Error("expected empty array, got null")
	}
	if len(resp.Posts) != 0 {
		t.Errorf("got %d posts, want 0", len(resp.Posts))
	}
	if resp.NextCursor != "" {
		t.Errorf("next_cursor = %q, want empty", resp.NextCursor)
	}
}

func TestPostHandler_ListFeed_NoNextCursor(t *testing.T) {
	repo := newMockPostRepo()
	repo.posts["a"] = &domain.Post{ID: "a", Status: domain.PostVisible, CreatedAt: fixedNow}
	svc := newTestPostService(repo)
	h := handler.NewPostHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/posts?limit=10", nil)
	rec := httptest.NewRecorder()

	h.ListFeed(rec, req)

	var resp struct {
		NextCursor string `json:"next_cursor"`
	}
	decodeBody(t, rec, &resp)

	if resp.NextCursor != "" {
		t.Errorf("next_cursor = %q, want empty when results < limit", resp.NextCursor)
	}
}

func TestPostHandler_ListFeed_LimitClamping(t *testing.T) {
	repo := newMockPostRepo()
	svc := newTestPostService(repo)
	h := handler.NewPostHandler(svc)

	tests := []struct {
		name  string
		query string
	}{
		{"limit > 100 clamped", "?limit=200"},
		{"limit <= 0 uses default", "?limit=-5"},
		{"non-numeric uses default", "?limit=abc"},
		{"no limit uses default", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/posts"+tt.query, nil)
			rec := httptest.NewRecorder()
			h.ListFeed(rec, req)
			if rec.Code != http.StatusOK {
				t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
			}
		})
	}
}

// --- Update tests ---

func TestPostHandler_Update(t *testing.T) {
	repo := newMockPostRepo()
	repo.posts["post-1"] = &domain.Post{
		ID:        "post-1",
		AuthorID:  "user-1",
		Body:      "original",
		Status:    domain.PostVisible,
		CreatedAt: fixedNow.Add(-5 * time.Minute),
	}
	svc := newTestPostService(repo)
	h := handler.NewPostHandler(svc)

	body := `{"body":"updated body"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/posts/post-1", strings.NewReader(body))
	req = withChiURLParam(req, "id", "post-1")
	req = withUser(req, testUser())
	rec := httptest.NewRecorder()

	h.Update(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var post domain.Post
	decodeBody(t, rec, &post)
	if post.Body != "updated body" {
		t.Errorf("body = %q, want %q", post.Body, "updated body")
	}
}

func TestPostHandler_Update_NotFound(t *testing.T) {
	repo := newMockPostRepo()
	svc := newTestPostService(repo)
	h := handler.NewPostHandler(svc)

	body := `{"body":"update"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/posts/nonexistent", strings.NewReader(body))
	req = withChiURLParam(req, "id", "nonexistent")
	req = withUser(req, testUser())
	rec := httptest.NewRecorder()

	h.Update(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestPostHandler_Update_EditWindowExpired(t *testing.T) {
	repo := newMockPostRepo()
	repo.posts["post-1"] = &domain.Post{
		ID:        "post-1",
		AuthorID:  "user-1",
		Body:      "original",
		Status:    domain.PostVisible,
		CreatedAt: fixedNow.Add(-1 * time.Hour), // well past 15-min window
	}
	svc := newTestPostService(repo)
	h := handler.NewPostHandler(svc)

	body := `{"body":"too late"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/posts/post-1", strings.NewReader(body))
	req = withChiURLParam(req, "id", "post-1")
	req = withUser(req, testUser())
	rec := httptest.NewRecorder()

	h.Update(rec, req)

	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusConflict)
	}
}

func TestPostHandler_Update_WrongAuthor(t *testing.T) {
	repo := newMockPostRepo()
	repo.posts["post-1"] = &domain.Post{
		ID:        "post-1",
		AuthorID:  "user-other",
		Body:      "original",
		Status:    domain.PostVisible,
		CreatedAt: fixedNow.Add(-5 * time.Minute),
	}
	svc := newTestPostService(repo)
	h := handler.NewPostHandler(svc)

	body := `{"body":"hijack"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/posts/post-1", strings.NewReader(body))
	req = withChiURLParam(req, "id", "post-1")
	req = withUser(req, testUser()) // user-1 trying to edit user-other's post
	rec := httptest.NewRecorder()

	h.Update(rec, req)

	// CanEdit returns false for wrong author, service returns ErrEditWindow → 409
	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusConflict)
	}
}

func TestPostHandler_Update_InvalidJSON(t *testing.T) {
	repo := newMockPostRepo()
	svc := newTestPostService(repo)
	h := handler.NewPostHandler(svc)

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/posts/post-1", strings.NewReader(`{bad}`))
	req = withChiURLParam(req, "id", "post-1")
	req = withUser(req, testUser())
	rec := httptest.NewRecorder()

	h.Update(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestPostHandler_Update_NoUser(t *testing.T) {
	repo := newMockPostRepo()
	svc := newTestPostService(repo)
	h := handler.NewPostHandler(svc)

	body := `{"body":"update"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/posts/post-1", strings.NewReader(body))
	req = withChiURLParam(req, "id", "post-1")
	rec := httptest.NewRecorder()

	h.Update(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

// --- Delete tests ---

func TestPostHandler_Delete(t *testing.T) {
	repo := newMockPostRepo()
	repo.posts["post-1"] = &domain.Post{
		ID:       "post-1",
		AuthorID: "user-1",
		Body:     "to delete",
		Status:   domain.PostVisible,
	}
	svc := newTestPostService(repo)
	h := handler.NewPostHandler(svc)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/posts/post-1", nil)
	req = withChiURLParam(req, "id", "post-1")
	req = withUser(req, testUser())
	rec := httptest.NewRecorder()

	h.Delete(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}

	// Verify soft-deleted
	if repo.posts["post-1"].Status != domain.PostRemovedByAuthor {
		t.Errorf("post status = %q, want %q", repo.posts["post-1"].Status, domain.PostRemovedByAuthor)
	}
}

func TestPostHandler_Delete_NotFound(t *testing.T) {
	repo := newMockPostRepo()
	svc := newTestPostService(repo)
	h := handler.NewPostHandler(svc)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/posts/nonexistent", nil)
	req = withChiURLParam(req, "id", "nonexistent")
	req = withUser(req, testUser())
	rec := httptest.NewRecorder()

	h.Delete(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestPostHandler_Delete_WrongAuthor(t *testing.T) {
	repo := newMockPostRepo()
	repo.posts["post-1"] = &domain.Post{
		ID:       "post-1",
		AuthorID: "user-other",
		Body:     "not yours",
		Status:   domain.PostVisible,
	}
	svc := newTestPostService(repo)
	h := handler.NewPostHandler(svc)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/posts/post-1", nil)
	req = withChiURLParam(req, "id", "post-1")
	req = withUser(req, testUser())
	rec := httptest.NewRecorder()

	h.Delete(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestPostHandler_Delete_NoUser(t *testing.T) {
	repo := newMockPostRepo()
	svc := newTestPostService(repo)
	h := handler.NewPostHandler(svc)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/posts/post-1", nil)
	req = withChiURLParam(req, "id", "post-1")
	rec := httptest.NewRecorder()

	h.Delete(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}
