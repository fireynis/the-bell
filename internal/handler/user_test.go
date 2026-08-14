package handler_test

import (
	"context"
	"encoding/json"
	"errors"
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

// stubReliefLister serves both self-view lists. The two are kept separate so a
// handler that read one and rendered it under the other's key would fail here
// rather than tell a member their mute was lifted when it was their suspension.
type stubReliefLister struct {
	lifts            []domain.ModerationRelief
	suspensionLifts  []domain.ModerationRelief
	err              error
	suspensionErr    error
	gotUserID        string
	gotSuspensionFor string
	calls            int
}

func (s *stubReliefLister) MuteLifts(_ context.Context, userID string, _ int) ([]domain.ModerationRelief, error) {
	s.calls++
	s.gotUserID = userID
	if s.err != nil {
		return nil, s.err
	}
	return s.lifts, nil
}

func (s *stubReliefLister) SuspensionLifts(_ context.Context, userID string, _ int) ([]domain.ModerationRelief, error) {
	s.gotSuspensionFor = userID
	if s.suspensionErr != nil {
		return nil, s.suspensionErr
	}
	return s.suspensionLifts, nil
}

// ownProfileResponse decoded far enough to check the moderation half.
type ownProfileBody struct {
	ID         string `json:"id"`
	MutedUntil string `json:"muted_until"`
	MuteLifts  []struct {
		LiftedAt           string `json:"lifted_at"`
		PreviousMutedUntil string `json:"previous_muted_until"`
		ModeratorID        string `json:"moderator_id"`
	} `json:"mute_lifts"`
	SuspensionLifts []struct {
		LiftedAt               string `json:"lifted_at"`
		PreviousSuspendedUntil string `json:"previous_suspended_until"`
		ModeratorID            string `json:"moderator_id"`
	} `json:"suspension_lifts"`
}

// --- mute lifts on the self profile ---

// A member who was muted and then released could not see that anywhere: the
// mute disappears from their profile when it is lifted, and the whole audit
// trail is moderator-only. The lift is the one moderation fact they are shown.
func TestUserHandler_GetMe_ShowsMuteLifts(t *testing.T) {
	previous := time.Date(2026, 3, 2, 14, 0, 0, 0, time.UTC)
	lifted := time.Date(2026, 3, 1, 14, 0, 0, 0, time.UTC)
	lifts := &stubReliefLister{lifts: []domain.ModerationRelief{{
		ID: "relief-1", TargetUserID: "user-1", ModeratorID: "mod-1",
		Type: domain.ReliefMuteLift, PreviousExpiresAt: &previous,
		WasInForce: true, CreatedAt: lifted,
	}}}
	h := handler.NewUserHandler(nil, nil, nil)
	h.SetReliefLister(lifts)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/me", nil)
	req = withUser(req, testUser())
	rec := httptest.NewRecorder()

	h.GetMe(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if lifts.gotUserID != "user-1" {
		t.Errorf("lifts read for %q, want the session user %q", lifts.gotUserID, "user-1")
	}

	var body ownProfileBody
	decodeBody(t, rec, &body)
	if len(body.MuteLifts) != 1 {
		t.Fatalf("%d mute lifts in the response, want 1; body: %s", len(body.MuteLifts), rec.Body.String())
	}
	got := body.MuteLifts[0]
	if got.LiftedAt != "2026-03-01T14:00:00Z" {
		t.Errorf("lifted_at = %q, want %q", got.LiftedAt, "2026-03-01T14:00:00Z")
	}
	if got.PreviousMutedUntil != "2026-03-02T14:00:00Z" {
		t.Errorf("previous_muted_until = %q, want %q", got.PreviousMutedUntil, "2026-03-02T14:00:00Z")
	}
	// The moderator who released them is deliberately not named. Which
	// moderator acted is on no member-facing response today, and deciding that
	// members may see it is a policy question, not a side effect of this field.
	if got.ModeratorID != "" {
		t.Errorf("moderator_id = %q; the member view must not name the moderator", got.ModeratorID)
	}
}

// Omitted entirely rather than sent as an empty array, matching muted_until:
// the field's presence is the whole answer, so a member who has never been
// released sees nothing rather than an empty section.
func TestUserHandler_GetMe_OmitsMuteLiftsWhenThereAreNone(t *testing.T) {
	h := handler.NewUserHandler(nil, nil, nil)
	h.SetReliefLister(&stubReliefLister{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/me", nil)
	req = withUser(req, testUser())
	rec := httptest.NewRecorder()

	h.GetMe(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if strings.Contains(rec.Body.String(), "mute_lifts") {
		t.Errorf("mute_lifts is present for a member with no lifts: %s", rec.Body.String())
	}
}

// A failed read is reported rather than rendered as "no lifts". An empty list
// is a definite answer to "was I ever released?", and returning it when the
// truth is unknown is the silent wrong answer this record exists to replace.
func TestUserHandler_GetMe_ReportsAFailedLiftRead(t *testing.T) {
	h := handler.NewUserHandler(nil, nil, nil)
	h.SetReliefLister(&stubReliefLister{err: errors.New("db unavailable")})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/me", nil)
	req = withUser(req, testUser())
	rec := httptest.NewRecorder()

	h.GetMe(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d; body: %s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
}

// A deployment without the moderation service wired still serves profiles. The
// handler predates the lister and every other field is independent of it.
func TestUserHandler_GetMe_WithoutALiftListerStillServesTheProfile(t *testing.T) {
	h := handler.NewUserHandler(nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/me", nil)
	req = withUser(req, testUser())
	rec := httptest.NewRecorder()

	h.GetMe(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

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

// --- muted_until exposure ---

// A muted user must be able to see why they cannot post.
func TestUserHandler_GetMe_ShowsOwnMute(t *testing.T) {
	// Relative to the wall clock: GetMe compares against time.Now(), so a fixed
	// date would silently start passing or failing as real time moved past it.
	until := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	user := testUser()
	user.MutedUntil = &until
	h := handler.NewUserHandler(nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/me", nil)
	req = withUser(req, user)
	rec := httptest.NewRecorder()

	h.GetMe(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp struct {
		MutedUntil string `json:"muted_until"`
	}
	decodeBody(t, rec, &resp)
	if want := until.Format(time.RFC3339); resp.MutedUntil != want {
		t.Errorf("muted_until = %q, want %q", resp.MutedUntil, want)
	}
}

// An expired mute is reported as no mute at all, so the client needs no clock
// of its own to interpret the field.
func TestUserHandler_GetMe_OmitsExpiredAndAbsentMutes(t *testing.T) {
	past := time.Now().Add(-time.Hour)

	tests := []struct {
		name  string
		muted *time.Time
	}{
		{"never muted", nil},
		{"mute already expired", &past},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user := testUser()
			user.MutedUntil = tt.muted
			h := handler.NewUserHandler(nil, nil, nil)

			req := httptest.NewRequest(http.MethodGet, "/api/v1/users/me", nil)
			req = withUser(req, user)
			rec := httptest.NewRecorder()

			h.GetMe(rec, req)

			if strings.Contains(rec.Body.String(), "muted_until") {
				t.Errorf("body = %s, want no muted_until field", rec.Body.String())
			}
		})
	}
}

// A mute is between the user and the moderators. It must not appear on another
// user's profile, and — the easy one to miss — domain.User is serialized whole
// by the pending-users list, so the struct must not carry a public json tag
// either.
func TestUserHandler_GetByID_NeverExposesAnotherUsersMute(t *testing.T) {
	until := time.Now().Add(time.Hour)
	target := testUser()
	target.ID = "user-2"
	target.MutedUntil = &until

	h := handler.NewUserHandler(&stubProfileService{user: target}, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/user-2", nil)
	req = withChiURLParam(req, "id", "user-2")
	rec := httptest.NewRecorder()

	h.GetByID(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if strings.Contains(rec.Body.String(), "muted") {
		t.Errorf("another user's profile leaked their mute: %s", rec.Body.String())
	}
}

// domain.User is marshalled directly by the pending-users endpoint, so a json
// tag on the struct would publish every user's mute there. This asserts on the
// struct rather than on a handler, because the struct is what would leak.
func TestDomainUser_DoesNotSerializeMutedUntil(t *testing.T) {
	until := time.Now().Add(time.Hour)
	user := testUser()
	user.MutedUntil = &until

	data, err := json.Marshal(user)
	if err != nil {
		t.Fatalf("marshalling user: %v", err)
	}
	if strings.Contains(string(data), "muted") {
		t.Errorf("domain.User serializes its mute: %s", data)
	}
}

// --- display names on the vouch list ---

// vouchNamesResponse decodes only the names, which is what this half of the DTO
// exists for.
type vouchNamesResponse struct {
	Received []struct {
		VoucherID          string `json:"voucher_id"`
		VoucherDisplayName string `json:"voucher_display_name"`
		VoucheeID          string `json:"vouchee_id"`
		VoucheeDisplayName string `json:"vouchee_display_name"`
	} `json:"received"`
	Given []struct {
		VoucherDisplayName string `json:"voucher_display_name"`
		VoucheeDisplayName string `json:"vouchee_display_name"`
	} `json:"given"`
}

// A vouch list rendered from ids alone is a column of UUID prefixes. Both names
// travel with the row from the query's joins, and both sides of the list carry
// both — the page shows the pair, not just the counterpart.
func TestUserHandler_ListVouches_CarriesBothDisplayNames(t *testing.T) {
	vouches := &stubVouchLister{
		received: []*domain.Vouch{{
			ID: "vouch-1", VoucherID: "user-1", VoucheeID: "user-2",
			Status: domain.VouchActive, CreatedAt: vouchedAt,
			VoucherDisplayName: "Alice", VoucheeDisplayName: "Bob",
		}},
		given: []*domain.Vouch{{
			ID: "vouch-2", VoucherID: "user-2", VoucheeID: "user-3",
			Status: domain.VouchActive, CreatedAt: vouchedAt,
			VoucherDisplayName: "Bob", VoucheeDisplayName: "Carol",
		}},
	}
	h := handler.NewUserHandler(nil, nil, vouches)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/user-2/vouches", nil)
	req = withChiURLParam(req, "id", "user-2")
	rec := httptest.NewRecorder()

	h.ListVouches(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp vouchNamesResponse
	decodeBody(t, rec, &resp)
	if len(resp.Received) != 1 || len(resp.Given) != 1 {
		t.Fatalf("got %d received and %d given, want 1 and 1", len(resp.Received), len(resp.Given))
	}
	got := resp.Received[0]
	if got.VoucherDisplayName != "Alice" || got.VoucheeDisplayName != "Bob" {
		t.Errorf("received names = %q/%q, want Alice/Bob", got.VoucherDisplayName, got.VoucheeDisplayName)
	}
	// The ids stay: the list links to profiles by id, so replacing them with
	// names would break every row's link.
	if got.VoucherID != "user-1" || got.VoucheeID != "user-2" {
		t.Errorf("received ids = %q/%q, want user-1/user-2", got.VoucherID, got.VoucheeID)
	}
	if resp.Given[0].VoucherDisplayName != "Bob" || resp.Given[0].VoucheeDisplayName != "Carol" {
		t.Errorf("given names = %q/%q, want Bob/Carol",
			resp.Given[0].VoucherDisplayName, resp.Given[0].VoucheeDisplayName)
	}
}

// A member who has set no display name yet sends the empty string rather than
// dropping the key: the list always renders both parties, so the client falls
// back to the id and needs no separate rule for an absent field.
func TestUserHandler_ListVouches_MissingNameIsAnEmptyString(t *testing.T) {
	vouches := &stubVouchLister{
		received: []*domain.Vouch{{
			ID: "vouch-1", VoucherID: "user-1", VoucheeID: "user-2",
			Status: domain.VouchActive, CreatedAt: vouchedAt,
		}},
	}
	h := handler.NewUserHandler(nil, nil, vouches)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/user-2/vouches", nil)
	req = withChiURLParam(req, "id", "user-2")
	rec := httptest.NewRecorder()

	h.ListVouches(rec, req)

	if !strings.Contains(rec.Body.String(), `"voucher_display_name":""`) {
		t.Errorf("body = %s, want voucher_display_name present and empty", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"vouchee_display_name":""`) {
		t.Errorf("body = %s, want vouchee_display_name present and empty", rec.Body.String())
	}
}

// --- suspension lifts on the self profile ---

// A suspension shows to the member as is_active being false; lifting it just
// makes that revert. Without this record a member released early cannot tell
// that from a suspension that ran its full course.
func TestUserHandler_GetMe_ShowsSuspensionLifts(t *testing.T) {
	previous := time.Date(2026, 3, 8, 14, 0, 0, 0, time.UTC)
	lifted := time.Date(2026, 3, 1, 14, 0, 0, 0, time.UTC)
	lifts := &stubReliefLister{suspensionLifts: []domain.ModerationRelief{{
		ID: "relief-2", TargetUserID: "user-1", ModeratorID: "mod-1",
		Type: domain.ReliefSuspensionLift, PreviousExpiresAt: &previous,
		WasInForce: true, CreatedAt: lifted,
	}}}
	h := handler.NewUserHandler(nil, nil, nil)
	h.SetReliefLister(lifts)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/me", nil)
	req = withUser(req, testUser())
	rec := httptest.NewRecorder()

	h.GetMe(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if lifts.gotSuspensionFor != "user-1" {
		t.Errorf("suspension lifts read for %q, want the session user %q", lifts.gotSuspensionFor, "user-1")
	}

	var body ownProfileBody
	decodeBody(t, rec, &body)
	if len(body.SuspensionLifts) != 1 {
		t.Fatalf("%d suspension lifts in the response, want 1; body: %s", len(body.SuspensionLifts), rec.Body.String())
	}
	got := body.SuspensionLifts[0]
	if got.LiftedAt != "2026-03-01T14:00:00Z" {
		t.Errorf("lifted_at = %q, want %q", got.LiftedAt, "2026-03-01T14:00:00Z")
	}
	if got.PreviousSuspendedUntil != "2026-03-08T14:00:00Z" {
		t.Errorf("previous_suspended_until = %q, want %q", got.PreviousSuspendedUntil, "2026-03-08T14:00:00Z")
	}
	// The moderator is not named here either, for the same policy reason.
	if got.ModeratorID != "" {
		t.Errorf("moderator_id = %q; the member view must not name the moderator", got.ModeratorID)
	}
	// Both lists come from one table, so a lift landing under the wrong key is
	// the failure worth pinning: it would tell this member a mute they never had
	// was ended.
	if len(body.MuteLifts) != 0 {
		t.Errorf("%d mute lifts for a member who only had a suspension lifted", len(body.MuteLifts))
	}
}

func TestUserHandler_GetMe_OmitsSuspensionLiftsWhenThereAreNone(t *testing.T) {
	h := handler.NewUserHandler(nil, nil, nil)
	h.SetReliefLister(&stubReliefLister{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/me", nil)
	req = withUser(req, testUser())
	rec := httptest.NewRecorder()

	h.GetMe(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if strings.Contains(rec.Body.String(), "suspension_lifts") {
		t.Errorf("suspension_lifts is present for a member with no lifts: %s", rec.Body.String())
	}
}

// A failed suspension read is reported rather than rendered as "no lifts", the
// same rule the mute read follows: an empty list is a definite answer, and
// giving it when the truth is unknown is the silent wrong answer.
func TestUserHandler_GetMe_ReportsAFailedSuspensionLiftRead(t *testing.T) {
	h := handler.NewUserHandler(nil, nil, nil)
	h.SetReliefLister(&stubReliefLister{suspensionErr: errors.New("db unavailable")})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/me", nil)
	req = withUser(req, testUser())
	rec := httptest.NewRecorder()

	h.GetMe(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d; body: %s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
}
