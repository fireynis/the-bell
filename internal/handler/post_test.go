package handler_test

import (
	"context"
	"encoding/json"
	"errors"
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
	posts   map[string]*domain.Post
	listErr error
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
	if m.listErr != nil {
		return nil, m.listErr
	}

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

func (m *mockPostRepo) ListPostsByAuthor(_ context.Context, authorID string, limit int) ([]*domain.Post, error) {
	var result []*domain.Post
	for _, p := range m.posts {
		if p.AuthorID == authorID {
			result = append(result, p)
		}
	}
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
		ID:         "user-1",
		Role:       domain.RoleMember,
		IsActive:   true,
		TrustScore: 50.0,
	}
}

func newTestPostService(repo service.PostRepository) *service.PostService {
	return service.NewPostService(repo, func() time.Time { return fixedNow })
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

func TestPostHandler_Create_MutedUser(t *testing.T) {
	repo := newMockPostRepo()
	svc := newTestPostService(repo)
	h := handler.NewPostHandler(svc)

	user := &domain.User{ID: "user-1", Role: domain.RoleMember, IsActive: true, TrustScore: 20.0}
	body := `{"body":"Hello"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/posts", strings.NewReader(body))
	req = withUser(req, user)
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestPostHandler_Create_BannedUser(t *testing.T) {
	repo := newMockPostRepo()
	svc := newTestPostService(repo)
	h := handler.NewPostHandler(svc)

	user := &domain.User{ID: "user-1", Role: domain.RoleBanned, IsActive: true, TrustScore: 0}
	body := `{"body":"Hello"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/posts", strings.NewReader(body))
	req = withUser(req, user)
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestPostHandler_Create_PendingUser(t *testing.T) {
	repo := newMockPostRepo()
	svc := newTestPostService(repo)
	h := handler.NewPostHandler(svc)

	user := &domain.User{ID: "user-1", Role: domain.RolePending, IsActive: true, TrustScore: 50.0}
	body := `{"body":"Hello"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/posts", strings.NewReader(body))
	req = withUser(req, user)
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusForbidden)
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

// A feed query that fails must not leak the database error text.
func TestPostHandler_ListFeed_ServiceError(t *testing.T) {
	repo := newMockPostRepo()
	repo.listErr = errors.New(`pq: relation "posts" does not exist`)
	h := handler.NewPostHandler(newTestPostService(repo))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/posts", nil)
	rec := httptest.NewRecorder()

	h.ListFeed(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if strings.Contains(rec.Body.String(), "posts") {
		t.Errorf("body = %s, want no database detail", rec.Body.String())
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

// --- reaction enrichment tests ---

type stubReactionEnricher struct {
	counts    map[string]map[domain.ReactionType]int
	countsErr error

	userReactions    map[string][]domain.ReactionType
	userReactionsErr error

	countsCalls    int
	userCalls      int
	gotUserID      string
	gotCountIDs    []string
	gotUserPostIDs []string
}

func (s *stubReactionEnricher) BatchCountByPosts(_ context.Context, postIDs []string) (map[string]map[domain.ReactionType]int, error) {
	s.countsCalls++
	s.gotCountIDs = postIDs
	if s.countsErr != nil {
		return nil, s.countsErr
	}
	return s.counts, nil
}

func (s *stubReactionEnricher) BatchGetUserReactions(_ context.Context, userID string, postIDs []string) (map[string][]domain.ReactionType, error) {
	s.userCalls++
	s.gotUserID, s.gotUserPostIDs = userID, postIDs
	if s.userReactionsErr != nil {
		return nil, s.userReactionsErr
	}
	return s.userReactions, nil
}

func feedRepoWithTwoPosts() *mockPostRepo {
	repo := newMockPostRepo()
	repo.posts["b"] = &domain.Post{ID: "b", Status: domain.PostVisible, CreatedAt: fixedNow}
	repo.posts["a"] = &domain.Post{ID: "a", Status: domain.PostVisible, CreatedAt: fixedNow}
	return repo
}

func listFeed(t *testing.T, h *handler.PostHandler, user *domain.User) []domain.Post {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/posts?limit=10", nil)
	if user != nil {
		req = withUser(req, user)
	}
	rec := httptest.NewRecorder()

	h.ListFeed(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp struct {
		Posts []domain.Post `json:"posts"`
	}
	decodeBody(t, rec, &resp)
	return resp.Posts
}

func postByID(t *testing.T, posts []domain.Post, id string) domain.Post {
	t.Helper()
	for _, p := range posts {
		if p.ID == id {
			return p
		}
	}
	t.Fatalf("post %q not in response", id)
	return domain.Post{}
}

func TestPostHandler_ListFeed_EnrichesReactionCounts(t *testing.T) {
	enricher := &stubReactionEnricher{
		counts: map[string]map[domain.ReactionType]int{
			"a": {domain.ReactionBell: 3, domain.ReactionHeart: 1},
		},
	}
	h := handler.NewPostHandler(newTestPostService(feedRepoWithTwoPosts()), handler.WithReactionEnricher(enricher))

	posts := listFeed(t, h, nil)

	if got := postByID(t, posts, "a").ReactionCounts[domain.ReactionBell]; got != 3 {
		t.Errorf("post a bell count = %d, want 3", got)
	}
	// A post the enricher has no row for keeps its zero value rather than
	// picking up another post's counts.
	if got := postByID(t, posts, "b").ReactionCounts; got != nil {
		t.Errorf("post b counts = %v, want none", got)
	}
	if len(enricher.gotCountIDs) != 2 {
		t.Errorf("enricher got %d post ids, want 2", len(enricher.gotCountIDs))
	}
}

// User reactions are what the frontend uses to render a reaction as already
// pressed, so they are only fetched — and only meaningful — for a signed-in
// caller.
func TestPostHandler_ListFeed_EnrichesUserReactions(t *testing.T) {
	enricher := &stubReactionEnricher{
		counts:        map[string]map[domain.ReactionType]int{"a": {domain.ReactionBell: 1}},
		userReactions: map[string][]domain.ReactionType{"a": {domain.ReactionBell}},
	}
	h := handler.NewPostHandler(newTestPostService(feedRepoWithTwoPosts()), handler.WithReactionEnricher(enricher))

	posts := listFeed(t, h, testUser())

	got := postByID(t, posts, "a").UserReactions
	if len(got) != 1 || got[0] != domain.ReactionBell {
		t.Errorf("post a user reactions = %v, want [bell]", got)
	}
	if enricher.gotUserID != "user-1" {
		t.Errorf("user reactions fetched for %q, want the session user %q", enricher.gotUserID, "user-1")
	}
	if postByID(t, posts, "b").UserReactions != nil {
		t.Error("post b picked up user reactions it has none of")
	}
}

func TestPostHandler_ListFeed_AnonymousSkipsUserReactions(t *testing.T) {
	enricher := &stubReactionEnricher{counts: map[string]map[domain.ReactionType]int{"a": {domain.ReactionBell: 1}}}
	h := handler.NewPostHandler(newTestPostService(feedRepoWithTwoPosts()), handler.WithReactionEnricher(enricher))

	listFeed(t, h, nil)

	if enricher.userCalls != 0 {
		t.Errorf("user reactions were fetched %d times for an anonymous request", enricher.userCalls)
	}
}

// Enrichment is decoration: a reactions backend that is down must degrade the
// feed, not take it out. Both failures are logged and swallowed, and each is
// independent of the other.
func TestPostHandler_ListFeed_EnrichmentFailuresDoNotFailTheFeed(t *testing.T) {
	t.Run("counts fail, user reactions still applied", func(t *testing.T) {
		enricher := &stubReactionEnricher{
			countsErr:     errors.New("redis: connection refused"),
			userReactions: map[string][]domain.ReactionType{"a": {domain.ReactionHeart}},
		}
		h := handler.NewPostHandler(newTestPostService(feedRepoWithTwoPosts()), handler.WithReactionEnricher(enricher))

		posts := listFeed(t, h, testUser())

		if len(posts) != 2 {
			t.Fatalf("got %d posts, want the feed intact with 2", len(posts))
		}
		if got := postByID(t, posts, "a").ReactionCounts; got != nil {
			t.Errorf("post a counts = %v, want none after the count lookup failed", got)
		}
		if got := postByID(t, posts, "a").UserReactions; len(got) != 1 {
			t.Errorf("post a user reactions = %v, want [heart] despite the count failure", got)
		}
	})

	t.Run("user reactions fail, counts still applied", func(t *testing.T) {
		enricher := &stubReactionEnricher{
			counts:           map[string]map[domain.ReactionType]int{"a": {domain.ReactionBell: 2}},
			userReactionsErr: errors.New("redis: connection refused"),
		}
		h := handler.NewPostHandler(newTestPostService(feedRepoWithTwoPosts()), handler.WithReactionEnricher(enricher))

		posts := listFeed(t, h, testUser())

		if len(posts) != 2 {
			t.Fatalf("got %d posts, want the feed intact with 2", len(posts))
		}
		if got := postByID(t, posts, "a").ReactionCounts[domain.ReactionBell]; got != 2 {
			t.Errorf("post a bell count = %d, want 2 despite the user-reaction failure", got)
		}
		if got := postByID(t, posts, "a").UserReactions; got != nil {
			t.Errorf("post a user reactions = %v, want none", got)
		}
	})

	t.Run("both fail", func(t *testing.T) {
		enricher := &stubReactionEnricher{
			countsErr:        errors.New("redis: connection refused"),
			userReactionsErr: errors.New("redis: connection refused"),
		}
		h := handler.NewPostHandler(newTestPostService(feedRepoWithTwoPosts()), handler.WithReactionEnricher(enricher))

		if posts := listFeed(t, h, testUser()); len(posts) != 2 {
			t.Fatalf("got %d posts, want the feed intact with 2", len(posts))
		}
	})
}

// An empty feed must not reach the enricher at all — a batch query for no post
// ids is a wasted round trip.
func TestPostHandler_ListFeed_EmptyFeedSkipsEnrichment(t *testing.T) {
	enricher := &stubReactionEnricher{}
	h := handler.NewPostHandler(newTestPostService(newMockPostRepo()), handler.WithReactionEnricher(enricher))

	listFeed(t, h, testUser())

	if enricher.countsCalls != 0 || enricher.userCalls != 0 {
		t.Errorf("enricher called (%d counts, %d user) for an empty feed", enricher.countsCalls, enricher.userCalls)
	}
}

func TestPostHandler_GetByID_EnrichesSinglePost(t *testing.T) {
	repo := newMockPostRepo()
	repo.posts["post-1"] = &domain.Post{ID: "post-1", Status: domain.PostVisible, CreatedAt: fixedNow}
	enricher := &stubReactionEnricher{
		counts:        map[string]map[domain.ReactionType]int{"post-1": {domain.ReactionCelebrate: 5}},
		userReactions: map[string][]domain.ReactionType{"post-1": {domain.ReactionCelebrate}},
	}
	h := handler.NewPostHandler(newTestPostService(repo), handler.WithReactionEnricher(enricher))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/posts/post-1", nil)
	req = withChiURLParam(req, "id", "post-1")
	req = withUser(req, testUser())
	rec := httptest.NewRecorder()

	h.GetByID(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var post domain.Post
	decodeBody(t, rec, &post)
	if post.ReactionCounts[domain.ReactionCelebrate] != 5 {
		t.Errorf("celebrate count = %d, want 5", post.ReactionCounts[domain.ReactionCelebrate])
	}
	if len(post.UserReactions) != 1 {
		t.Errorf("user reactions = %v, want [celebrate]", post.UserReactions)
	}
}
