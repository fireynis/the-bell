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

func (m *mockPostRepo) UpdatePostStatus(_ context.Context, id string, status domain.PostStatus, reason, removedBy string) error {
	p, ok := m.posts[id]
	if !ok {
		return service.ErrNotFound
	}
	p.Status = status
	p.RemovalReason = reason
	p.RemovedBy = removedBy
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

// Strangers no longer receive a removed post at all, so the note's guarantee
// now matters most for the callers who DO receive it: a moderator gets the
// post, and must still not get the private note through the public post shape.
// Exposing it takes a deliberate moderator-facing response type.
func TestPostHandler_GetByID_DoesNotLeakRemovalReason(t *testing.T) {
	repo := newMockPostRepo()
	repo.posts["post-1"] = &domain.Post{
		ID:            "post-1",
		AuthorID:      "user-1",
		Body:          "the post body",
		Status:        domain.PostRemovedByMod,
		RemovalReason: "harassment of another member; third strike",
		CreatedAt:     fixedNow,
	}
	svc := newTestPostService(repo)
	h := handler.NewPostHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/posts/post-1", nil)
	req = withChiURLParam(req, "id", "post-1")
	req = withUser(req, &domain.User{ID: "mod-1", Role: domain.RoleModerator, IsActive: true})
	rec := httptest.NewRecorder()

	h.GetByID(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	body := rec.Body.String()
	if strings.Contains(body, "removal_reason") {
		t.Errorf("response carries a removal_reason key: %s", body)
	}
	if strings.Contains(body, "harassment") {
		t.Errorf("response leaked the moderator's note: %s", body)
	}
}

// The feed is the highest-traffic path, so it gets the same assertion rather
// than relying on the query filter alone to keep removed posts out of it.
func TestPostHandler_ListFeed_DoesNotLeakRemovalReason(t *testing.T) {
	repo := newMockPostRepo()
	repo.posts["post-1"] = &domain.Post{
		ID:            "post-1",
		AuthorID:      "user-1",
		Body:          "visible post",
		Status:        domain.PostVisible,
		RemovalReason: "should never reach the wire",
		CreatedAt:     fixedNow,
	}
	svc := newTestPostService(repo)
	h := handler.NewPostHandler(svc)

	rec := httptest.NewRecorder()
	h.ListFeed(rec, httptest.NewRequest(http.MethodGet, "/api/v1/posts", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if strings.Contains(rec.Body.String(), "should never reach the wire") {
		t.Errorf("feed leaked a removal reason: %s", rec.Body.String())
	}
}

// A removed post must be indistinguishable from one that never existed — same
// status and the same bytes — or the refusal itself confirms the id is real.
func TestPostHandler_GetByID_RemovedPostIs404ToAStranger(t *testing.T) {
	repo := newMockPostRepo()
	repo.posts["post-1"] = &domain.Post{
		ID:            "post-1",
		AuthorID:      "author-1",
		Body:          "the post body",
		Status:        domain.PostRemovedByMod,
		RemovalReason: "harassment; third strike",
		CreatedAt:     fixedNow,
	}
	svc := newTestPostService(repo)
	h := handler.NewPostHandler(svc)

	// A stranger asking for the removed post.
	removed := httptest.NewRequest(http.MethodGet, "/api/v1/posts/post-1", nil)
	removed = withChiURLParam(removed, "id", "post-1")
	removed = withUser(removed, &domain.User{ID: "stranger", Role: domain.RoleMember, IsActive: true})
	removedRec := httptest.NewRecorder()
	h.GetByID(removedRec, removed)

	// The same caller asking for an id that was never issued.
	missing := httptest.NewRequest(http.MethodGet, "/api/v1/posts/no-such-post", nil)
	missing = withChiURLParam(missing, "id", "no-such-post")
	missing = withUser(missing, &domain.User{ID: "stranger", Role: domain.RoleMember, IsActive: true})
	missingRec := httptest.NewRecorder()
	h.GetByID(missingRec, missing)

	if removedRec.Code != http.StatusNotFound {
		t.Fatalf("removed post status = %d, want %d; body: %s", removedRec.Code, http.StatusNotFound, removedRec.Body.String())
	}
	if got, want := removedRec.Body.String(), missingRec.Body.String(); got != want {
		t.Errorf("removed-post body = %q, missing-post body = %q; they must be byte-identical", got, want)
	}
	if removedRec.Code != missingRec.Code {
		t.Errorf("removed-post status = %d, missing-post status = %d", removedRec.Code, missingRec.Code)
	}
	if strings.Contains(removedRec.Body.String(), "harassment") {
		t.Errorf("refusal leaked the removal reason: %s", removedRec.Body.String())
	}
}

func TestPostHandler_GetByID_RemovedPostIs404ToAnonymous(t *testing.T) {
	repo := newMockPostRepo()
	repo.posts["post-1"] = &domain.Post{
		ID: "post-1", AuthorID: "author-1", Body: "b",
		Status: domain.PostRemovedByAuthor, CreatedAt: fixedNow,
	}
	h := handler.NewPostHandler(newTestPostService(repo))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/posts/post-1", nil)
	req = withChiURLParam(req, "id", "post-1")
	rec := httptest.NewRecorder()

	h.GetByID(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

// The flow this whole change had to avoid breaking: a moderator opening the
// report queue fetches the reported post by id, and it is routinely no longer
// visible — the author deletes it once reported.
//
// Both removed statuses are pinned. removed_by_mod is the one moderator removal
// writes, so if it stopped being readable the queue would break the instant a
// removal succeeded — the moderator would take the post down and immediately
// lose the ability to see what they had acted on.
func TestPostHandler_GetByID_ModeratorStillSeesARemovedPost(t *testing.T) {
	for _, status := range []domain.PostStatus{domain.PostRemovedByAuthor, domain.PostRemovedByMod} {
		t.Run(string(status), func(t *testing.T) {
			repo := newMockPostRepo()
			repo.posts["post-1"] = &domain.Post{
				ID: "post-1", AuthorID: "author-1", Body: "the reported body",
				Status: status, RemovalReason: "a private note", RemovedBy: "mod-1",
				CreatedAt: fixedNow,
			}
			h := handler.NewPostHandler(newTestPostService(repo))

			for _, viewer := range []*domain.User{
				{ID: "mod-1", Role: domain.RoleModerator, IsActive: true},
				{ID: "council-1", Role: domain.RoleCouncil, IsActive: true},
				{ID: "author-1", Role: domain.RoleMember, IsActive: true},
			} {
				t.Run(string(viewer.Role), func(t *testing.T) {
					req := httptest.NewRequest(http.MethodGet, "/api/v1/posts/post-1", nil)
					req = withChiURLParam(req, "id", "post-1")
					req = withUser(req, viewer)
					rec := httptest.NewRecorder()

					h.GetByID(rec, req)

					if rec.Code != http.StatusOK {
						t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
					}
					var post domain.Post
					decodeBody(t, rec, &post)
					if post.Body != "the reported body" {
						t.Errorf("body = %q, want the post content", post.Body)
					}
					// Readable, but the moderation metadata still does not ride along.
					if strings.Contains(rec.Body.String(), "a private note") {
						t.Errorf("response leaked the removal reason: %s", rec.Body.String())
					}
				})
			}
		})
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

// --- moderator post removal ---

func removeRequest(t *testing.T, postID, body string, user *domain.User) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/moderation/posts/"+postID+"/remove", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withChiURLParam(req, "id", postID)
	if user != nil {
		req = withUser(req, user)
	}
	return req
}

func activeModerator() *domain.User {
	return &domain.User{ID: "mod-1", Role: domain.RoleModerator, IsActive: true}
}

func TestPostHandler_RemoveByModerator_TakesThePostDown(t *testing.T) {
	repo := newMockPostRepo()
	repo.posts["post-1"] = &domain.Post{
		ID: "post-1", AuthorID: "author-1", Body: "offending", Status: domain.PostVisible,
		CreatedAt: fixedNow,
	}
	h := handler.NewPostHandler(newTestPostService(repo))

	rec := httptest.NewRecorder()
	h.RemoveByModerator(rec, removeRequest(t, "post-1", `{"reason":"harassment"}`, activeModerator()))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d (body %s)", rec.Code, http.StatusNoContent, rec.Body.String())
	}
	if repo.posts["post-1"].Status != domain.PostRemovedByMod {
		t.Errorf("status = %q, want %q", repo.posts["post-1"].Status, domain.PostRemovedByMod)
	}
	if repo.posts["post-1"].RemovalReason != "harassment" {
		t.Errorf("removal reason = %q, want %q", repo.posts["post-1"].RemovalReason, "harassment")
	}
}

// The reason is a moderator's private note. The removal response is the first
// thing that ever writes a real one, so it is the first thing that could echo
// one back — and it must not, even to the moderator who just typed it.
func TestPostHandler_RemoveByModerator_DoesNotEchoTheReason(t *testing.T) {
	repo := newMockPostRepo()
	repo.posts["post-1"] = &domain.Post{
		ID: "post-1", AuthorID: "author-1", Status: domain.PostVisible, CreatedAt: fixedNow,
	}
	h := handler.NewPostHandler(newTestPostService(repo))

	rec := httptest.NewRecorder()
	h.RemoveByModerator(rec, removeRequest(t,
		"post-1", `{"reason":"harassment of another member"}`, activeModerator()))

	if strings.Contains(rec.Body.String(), "harassment") {
		t.Errorf("response echoed the moderator's note: %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "removal_reason") {
		t.Errorf("response carries a removal_reason key: %s", rec.Body.String())
	}
}

func TestPostHandler_RemoveByModerator_RejectsUnauthenticated(t *testing.T) {
	repo := newMockPostRepo()
	repo.posts["post-1"] = &domain.Post{ID: "post-1", AuthorID: "author-1", Status: domain.PostVisible}
	h := handler.NewPostHandler(newTestPostService(repo))

	rec := httptest.NewRecorder()
	h.RemoveByModerator(rec, removeRequest(t, "post-1", `{"reason":"spam"}`, nil))

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if repo.posts["post-1"].Status != domain.PostVisible {
		t.Error("post was removed by an unauthenticated caller")
	}
}

// The route group guards this, but the handler must not depend on that alone:
// a caller who reaches it without the role is refused here too.
func TestPostHandler_RemoveByModerator_RejectsANonModerator(t *testing.T) {
	repo := newMockPostRepo()
	repo.posts["post-1"] = &domain.Post{ID: "post-1", AuthorID: "author-1", Status: domain.PostVisible}
	h := handler.NewPostHandler(newTestPostService(repo))

	member := &domain.User{ID: "u1", Role: domain.RoleMember, IsActive: true}
	rec := httptest.NewRecorder()
	h.RemoveByModerator(rec, removeRequest(t, "post-1", `{"reason":"spam"}`, member))

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
	if repo.posts["post-1"].Status != domain.PostVisible {
		t.Error("post was removed by a member")
	}
}

func TestPostHandler_RemoveByModerator_ErrorStatuses(t *testing.T) {
	tests := []struct {
		name       string
		postID     string
		body       string
		wantStatus int
	}{
		{"a blank reason is a bad request", "post-1", `{"reason":"   "}`, http.StatusBadRequest},
		{"a missing reason is a bad request", "post-1", `{}`, http.StatusBadRequest},
		{"malformed JSON is a bad request", "post-1", `{"reason":`, http.StatusBadRequest},
		{"an unknown post is not found", "no-such-post", `{"reason":"spam"}`, http.StatusNotFound},
		{"an already-removed post is a bad request", "post-gone", `{"reason":"spam"}`, http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newMockPostRepo()
			repo.posts["post-1"] = &domain.Post{ID: "post-1", AuthorID: "a1", Status: domain.PostVisible}
			repo.posts["post-gone"] = &domain.Post{
				ID: "post-gone", AuthorID: "a1", Status: domain.PostRemovedByAuthor,
			}
			h := handler.NewPostHandler(newTestPostService(repo))

			rec := httptest.NewRecorder()
			h.RemoveByModerator(rec, removeRequest(t, tt.postID, tt.body, activeModerator()))

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d (body %s)", rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}

// The audit trail's whole point: the removal names the moderator who made it.
func TestPostHandler_RemoveByModerator_RecordsWhoRemovedIt(t *testing.T) {
	repo := newMockPostRepo()
	repo.posts["post-1"] = &domain.Post{
		ID: "post-1", AuthorID: "author-1", Status: domain.PostVisible, CreatedAt: fixedNow,
	}
	h := handler.NewPostHandler(newTestPostService(repo))

	rec := httptest.NewRecorder()
	h.RemoveByModerator(rec, removeRequest(t, "post-1", `{"reason":"harassment"}`, activeModerator()))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusNoContent, rec.Body.String())
	}
	if repo.posts["post-1"].RemovedBy != "mod-1" {
		t.Errorf("RemovedBy = %q, want the acting moderator %q",
			repo.posts["post-1"].RemovedBy, "mod-1")
	}
}

// The moderator's identity is moderation metadata, exactly like the reason, and
// must not ride out on the public post shape either.
func TestPostHandler_GetByID_DoesNotLeakRemovedBy(t *testing.T) {
	repo := newMockPostRepo()
	repo.posts["post-1"] = &domain.Post{
		ID: "post-1", AuthorID: "author-1", Body: "the post body",
		Status: domain.PostRemovedByMod, RemovalReason: "off topic",
		RemovedBy: "moderator-42", CreatedAt: fixedNow,
	}
	h := handler.NewPostHandler(newTestPostService(repo))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/posts/post-1", nil)
	req = withChiURLParam(req, "id", "post-1")
	req = withUser(req, activeModerator())
	rec := httptest.NewRecorder()

	h.GetByID(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if strings.Contains(rec.Body.String(), "moderator-42") {
		t.Errorf("response named the removing moderator: %s", rec.Body.String())
	}
}
