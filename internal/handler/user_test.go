package handler_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/handler"
	"github.com/fireynis/the-bell/internal/service"
)

// --- stub collaborators ---

type stubProfileService struct {
	user *domain.User
	err  error

	gotID          string
	gotDisplayName string
	gotBio         string
	gotAvatarURL   string
}

func (s *stubProfileService) GetByID(_ context.Context, id string) (*domain.User, error) {
	s.gotID = id
	if s.err != nil {
		return nil, s.err
	}
	return s.user, nil
}

func (s *stubProfileService) UpdateProfile(_ context.Context, id, displayName, bio, avatarURL string) (*domain.User, error) {
	s.gotID, s.gotDisplayName, s.gotBio, s.gotAvatarURL = id, displayName, bio, avatarURL
	if s.err != nil {
		return nil, s.err
	}
	return s.user, nil
}

type stubAuthorPosts struct {
	posts []*domain.Post
	err   error

	gotAuthorID string
	gotLimit    int
}

func (s *stubAuthorPosts) ListByAuthor(_ context.Context, authorID string, limit int) ([]*domain.Post, error) {
	s.gotAuthorID, s.gotLimit = authorID, limit
	if s.err != nil {
		return nil, s.err
	}
	return s.posts, nil
}

type stubVouchLister struct {
	received    []*domain.Vouch
	given       []*domain.Vouch
	receivedErr error
	givenErr    error

	givenCalls int
}

func (s *stubVouchLister) ListReceivedVouches(_ context.Context, _ string) ([]*domain.Vouch, error) {
	if s.receivedErr != nil {
		return nil, s.receivedErr
	}
	return s.received, nil
}

func (s *stubVouchLister) ListGivenVouches(_ context.Context, _ string) ([]*domain.Vouch, error) {
	s.givenCalls++
	if s.givenErr != nil {
		return nil, s.givenErr
	}
	return s.given, nil
}

var vouchedAt = time.Date(2026, 3, 1, 12, 30, 45, 0, time.UTC)

// --- GetMe tests ---

func TestUserHandler_GetMe(t *testing.T) {
	h := handler.NewUserHandler(nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	req = withUser(req, testUser())
	rec := httptest.NewRecorder()

	h.GetMe(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var user domain.User
	decodeBody(t, rec, &user)
	if user.ID != "user-1" {
		t.Errorf("id = %q, want %q", user.ID, "user-1")
	}
	if user.Role != domain.RoleMember {
		t.Errorf("role = %q, want %q", user.Role, domain.RoleMember)
	}
}

func TestUserHandler_GetMe_NoUser(t *testing.T) {
	h := handler.NewUserHandler(nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	rec := httptest.NewRecorder()

	h.GetMe(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

// --- UpdateMe tests ---

func TestUserHandler_UpdateMe(t *testing.T) {
	users := &stubProfileService{user: &domain.User{
		ID: "user-1", DisplayName: "Ada", Bio: "builds things", AvatarURL: "/img/ada.jpg",
		Role: domain.RoleMember, IsActive: true,
	}}
	h := handler.NewUserHandler(users, nil, nil)

	body := `{"display_name":"Ada","bio":"builds things","avatar_url":"/img/ada.jpg"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/users/me", strings.NewReader(body))
	req = withUser(req, testUser())
	rec := httptest.NewRecorder()

	h.UpdateMe(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if users.gotDisplayName != "Ada" || users.gotBio != "builds things" || users.gotAvatarURL != "/img/ada.jpg" {
		t.Errorf("service got (%q, %q, %q), want the request body fields",
			users.gotDisplayName, users.gotBio, users.gotAvatarURL)
	}

	var resp struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
	}
	decodeBody(t, rec, &resp)
	if resp.DisplayName != "Ada" {
		t.Errorf("display_name = %q, want %q", resp.DisplayName, "Ada")
	}
}

// The updated profile is always the caller's own; the id comes from the
// session, never from anything the request could set.
func TestUserHandler_UpdateMe_UpdatesTheSessionUser(t *testing.T) {
	users := &stubProfileService{user: &domain.User{ID: "user-1"}}
	h := handler.NewUserHandler(users, nil, nil)

	body := `{"display_name":"Ada","bio":"","avatar_url":""}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/users/me", strings.NewReader(body))
	req = withUser(req, testUser())
	rec := httptest.NewRecorder()

	h.UpdateMe(rec, req)

	if users.gotID != "user-1" {
		t.Errorf("updated id = %q, want the session user %q", users.gotID, "user-1")
	}
}

func TestUserHandler_UpdateMe_NoUser(t *testing.T) {
	users := &stubProfileService{user: &domain.User{ID: "user-1"}}
	h := handler.NewUserHandler(users, nil, nil)

	body := `{"display_name":"Ada"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/users/me", strings.NewReader(body))
	rec := httptest.NewRecorder()

	h.UpdateMe(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if users.gotID != "" {
		t.Errorf("service was called with %q for an unauthenticated request", users.gotID)
	}
}

func TestUserHandler_UpdateMe_BadBody(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"malformed json", `{bad`},
		{"empty body", ``},
		{"unknown field is rejected", `{"display_name":"Ada","role":"council"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			users := &stubProfileService{user: &domain.User{ID: "user-1"}}
			h := handler.NewUserHandler(users, nil, nil)

			req := httptest.NewRequest(http.MethodPut, "/api/v1/users/me", strings.NewReader(tt.body))
			req = withUser(req, testUser())
			rec := httptest.NewRecorder()

			h.UpdateMe(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
			}
			if users.gotID != "" {
				t.Error("service was called with an undecodable body")
			}
		})
	}
}

func TestUserHandler_UpdateMe_ServiceErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{"validation", service.ErrValidation, http.StatusBadRequest},
		{"not found", service.ErrNotFound, http.StatusNotFound},
		{"forbidden", service.ErrForbidden, http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := handler.NewUserHandler(&stubProfileService{err: tt.err}, nil, nil)

			body := `{"display_name":"Ada","bio":"","avatar_url":""}`
			req := httptest.NewRequest(http.MethodPut, "/api/v1/users/me", strings.NewReader(body))
			req = withUser(req, testUser())
			rec := httptest.NewRecorder()

			h.UpdateMe(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
		})
	}
}

// --- GetByID tests ---

func TestUserHandler_GetByID(t *testing.T) {
	users := &stubProfileService{user: &domain.User{
		ID: "user-2", DisplayName: "Grace", Role: domain.RoleCouncil, IsActive: true,
		TrustScore: 90, JoinedAt: vouchedAt,
	}}
	h := handler.NewUserHandler(users, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/user-2", nil)
	req = withChiURLParam(req, "id", "user-2")
	rec := httptest.NewRecorder()

	h.GetByID(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if users.gotID != "user-2" {
		t.Errorf("looked up %q, want the URL param %q", users.gotID, "user-2")
	}

	var resp struct {
		ID       string `json:"id"`
		Role     string `json:"role"`
		JoinedAt string `json:"joined_at"`
	}
	decodeBody(t, rec, &resp)
	if resp.ID != "user-2" || resp.Role != "council" {
		t.Errorf("got (%q, %q), want (%q, %q)", resp.ID, resp.Role, "user-2", "council")
	}
	if resp.JoinedAt != "2026-03-01T12:30:45Z" {
		t.Errorf("joined_at = %q, want %q", resp.JoinedAt, "2026-03-01T12:30:45Z")
	}
}

func TestUserHandler_GetByID_NotFound(t *testing.T) {
	h := handler.NewUserHandler(&stubProfileService{err: service.ErrNotFound}, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/nobody", nil)
	req = withChiURLParam(req, "id", "nobody")
	rec := httptest.NewRecorder()

	h.GetByID(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

// --- ListPosts tests ---

func TestUserHandler_ListPosts(t *testing.T) {
	posts := &stubAuthorPosts{posts: []*domain.Post{
		{ID: "post-2", AuthorID: "user-2", Body: "second"},
		{ID: "post-1", AuthorID: "user-2", Body: "first"},
	}}
	h := handler.NewUserHandler(nil, posts, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/user-2/posts?limit=5", nil)
	req = withChiURLParam(req, "id", "user-2")
	rec := httptest.NewRecorder()

	h.ListPosts(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if posts.gotAuthorID != "user-2" {
		t.Errorf("listed author %q, want the URL param %q", posts.gotAuthorID, "user-2")
	}
	if posts.gotLimit != 5 {
		t.Errorf("limit = %d, want 5", posts.gotLimit)
	}

	var resp struct {
		Posts []domain.Post `json:"posts"`
	}
	decodeBody(t, rec, &resp)
	if len(resp.Posts) != 2 {
		t.Fatalf("got %d posts, want 2", len(resp.Posts))
	}
}

// An author with no posts must serialize as [] so the frontend can map over the
// response without a null check.
func TestUserHandler_ListPosts_EmptyIsAnArray(t *testing.T) {
	h := handler.NewUserHandler(nil, &stubAuthorPosts{posts: nil}, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/user-2/posts", nil)
	req = withChiURLParam(req, "id", "user-2")
	rec := httptest.NewRecorder()

	h.ListPosts(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), `"posts":[]`) {
		t.Errorf("body = %s, want posts to be an empty array", rec.Body.String())
	}
}

func TestUserHandler_ListPosts_MissingLimitUsesDefault(t *testing.T) {
	posts := &stubAuthorPosts{}
	h := handler.NewUserHandler(nil, posts, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/user-2/posts?limit=abc", nil)
	req = withChiURLParam(req, "id", "user-2")
	rec := httptest.NewRecorder()

	h.ListPosts(rec, req)

	if posts.gotLimit != 20 {
		t.Errorf("limit = %d, want the default 20", posts.gotLimit)
	}
}

func TestUserHandler_ListPosts_ServiceError(t *testing.T) {
	h := handler.NewUserHandler(nil, &stubAuthorPosts{err: service.ErrNotFound}, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/nobody/posts", nil)
	req = withChiURLParam(req, "id", "nobody")
	rec := httptest.NewRecorder()

	h.ListPosts(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

// --- ListVouches tests ---

type vouchesResponse struct {
	Received []struct {
		ID        string `json:"id"`
		VoucherID string `json:"voucher_id"`
		Status    string `json:"status"`
		CreatedAt string `json:"created_at"`
	} `json:"received"`
	Given []struct {
		ID string `json:"id"`
	} `json:"given"`
}

func TestUserHandler_ListVouches(t *testing.T) {
	vouches := &stubVouchLister{
		received: []*domain.Vouch{
			{ID: "vouch-1", VoucherID: "user-1", VoucheeID: "user-2", Status: domain.VouchActive, CreatedAt: vouchedAt},
		},
		given: []*domain.Vouch{
			{ID: "vouch-2", VoucherID: "user-2", VoucheeID: "user-3", Status: domain.VouchRevoked, CreatedAt: vouchedAt},
		},
	}
	h := handler.NewUserHandler(nil, nil, vouches)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/user-2/vouches", nil)
	req = withChiURLParam(req, "id", "user-2")
	rec := httptest.NewRecorder()

	h.ListVouches(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp vouchesResponse
	decodeBody(t, rec, &resp)
	if len(resp.Received) != 1 || len(resp.Given) != 1 {
		t.Fatalf("got %d received and %d given, want 1 and 1", len(resp.Received), len(resp.Given))
	}
	if resp.Received[0].ID != "vouch-1" || resp.Received[0].Status != "active" {
		t.Errorf("received[0] = %+v, want vouch-1/active", resp.Received[0])
	}
	if resp.Received[0].CreatedAt != "2026-03-01T12:30:45Z" {
		t.Errorf("created_at = %q, want %q", resp.Received[0].CreatedAt, "2026-03-01T12:30:45Z")
	}
	if resp.Given[0].ID != "vouch-2" {
		t.Errorf("given[0].id = %q, want %q", resp.Given[0].ID, "vouch-2")
	}
}

// A profile with no vouches must serialize both sides as [] rather than null;
// the frontend renders the two lists without null checks.
func TestUserHandler_ListVouches_EmptyListsAreArrays(t *testing.T) {
	h := handler.NewUserHandler(nil, nil, &stubVouchLister{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/user-2/vouches", nil)
	req = withChiURLParam(req, "id", "user-2")
	rec := httptest.NewRecorder()

	h.ListVouches(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if body := rec.Body.String(); body != `{"received":[],"given":[]}` {
		t.Errorf("body = %s, want both lists to be empty arrays", body)
	}
}

func TestUserHandler_ListVouches_ReceivedError(t *testing.T) {
	vouches := &stubVouchLister{receivedErr: service.ErrNotFound}
	h := handler.NewUserHandler(nil, nil, vouches)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/nobody/vouches", nil)
	req = withChiURLParam(req, "id", "nobody")
	rec := httptest.NewRecorder()

	h.ListVouches(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if vouches.givenCalls != 0 {
		t.Error("given vouches were still fetched after the received lookup failed")
	}
}

func TestUserHandler_ListVouches_GivenError(t *testing.T) {
	h := handler.NewUserHandler(nil, nil, &stubVouchLister{givenErr: service.ErrForbidden})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/user-2/vouches", nil)
	req = withChiURLParam(req, "id", "user-2")
	rec := httptest.NewRecorder()

	h.ListVouches(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}
