package handler_test

import (
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

// directoryBody is the published contract, decoded. It is spelled out rather
// than reusing a handler type so a renamed JSON key fails here.
type directoryBody struct {
	Users []struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
		Role        string `json:"role"`
		JoinedAt    string `json:"joined_at"`
	} `json:"users"`
	Total int `json:"total"`
}

func directoryRequest(t *testing.T, users *stubProfileService, query string) *httptest.ResponseRecorder {
	t.Helper()

	h := handler.NewUserHandler(users, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/users"+query, nil)
	rec := httptest.NewRecorder()
	h.ListDirectory(rec, req)
	return rec
}

func TestUserHandler_ListDirectory_ResponseShape(t *testing.T) {
	joined := time.Date(2026, 3, 1, 12, 30, 45, 0, time.UTC)
	users := &stubProfileService{
		directory: []*domain.User{
			{
				ID: "user-1", DisplayName: "Ada", Role: domain.RoleMember,
				IsActive: true, JoinedAt: joined,
				// None of these may reach the wire: the directory is the one
				// listing a pending resident can read, and it is not where the
				// town's trust scores and mute state get published.
				TrustScore: 87.5, Bio: "builds things", AvatarURL: "/img/ada.jpg",
			},
		},
		total: 1,
	}

	rec := directoryRequest(t, users, "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var body directoryBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding response: %v; body: %s", err, rec.Body.String())
	}
	if len(body.Users) != 1 {
		t.Fatalf("%d users in the response, want 1", len(body.Users))
	}
	got := body.Users[0]
	if got.ID != "user-1" || got.DisplayName != "Ada" || got.Role != "member" {
		t.Errorf("entry = %+v, want user-1/Ada/member", got)
	}
	if got.JoinedAt != "2026-03-01T12:30:45Z" {
		t.Errorf("joined_at = %q, want RFC3339 %q", got.JoinedAt, "2026-03-01T12:30:45Z")
	}
	if body.Total != 1 {
		t.Errorf("total = %d, want 1", body.Total)
	}

	for _, leaked := range []string{"trust_score", "muted_until", "is_active", "bio", "avatar_url"} {
		if strings.Contains(rec.Body.String(), leaked) {
			t.Errorf("the directory published %q: %s", leaked, rec.Body.String())
		}
	}
}

// An empty directory is an empty array, never null. A client that maps over the
// field must not have to special-case the town's first day.
func TestUserHandler_ListDirectory_EmptyIsAnArray(t *testing.T) {
	rec := directoryRequest(t, &stubProfileService{}, "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if body := rec.Body.String(); !strings.Contains(body, `"users":[]`) {
		t.Errorf("body = %s, want an empty users array", body)
	}
}

// A resident who has not set a name is sent as the empty string rather than
// omitted, matching the vouch listings: the key is always there, so a client
// falls back to the id for anything falsy.
func TestUserHandler_ListDirectory_EmptyDisplayNameIsSentAsEmptyString(t *testing.T) {
	users := &stubProfileService{
		directory: []*domain.User{{ID: "user-1", Role: domain.RolePending, IsActive: true}},
		total:     1,
	}

	rec := directoryRequest(t, users, "")

	var body directoryBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if len(body.Users) != 1 {
		t.Fatalf("%d users in the response, want 1", len(body.Users))
	}
	if body.Users[0].DisplayName != "" {
		t.Errorf("display_name = %q, want the empty string", body.Users[0].DisplayName)
	}
	if !strings.Contains(rec.Body.String(), `"display_name":""`) {
		t.Errorf("display_name was omitted rather than sent empty: %s", rec.Body.String())
	}
}

// The query string is where limit, offset and the search term are read, and
// this is the only place that parsing is observable.
func TestUserHandler_ListDirectory_QueryParameters(t *testing.T) {
	tests := []struct {
		name       string
		query      string
		wantQuery  string
		wantLimit  int
		wantOffset int
	}{
		{"defaults", "", "", service.DirectoryDefaultLimit, 0},
		{"a search term", "?q=ali", "ali", service.DirectoryDefaultLimit, 0},
		{"pagination", "?limit=10&offset=30", "", 10, 30},
		{"all three", "?q=bo&limit=5&offset=5", "bo", 5, 5},
		{"a limit above the ceiling is clamped", "?limit=5000", "", service.DirectoryMaxLimit, 0},
		{"a limit of zero falls back to the default", "?limit=0", "", service.DirectoryDefaultLimit, 0},
		{"a negative limit falls back to the default", "?limit=-3", "", service.DirectoryDefaultLimit, 0},
		{"an unparseable limit falls back to the default", "?limit=lots", "", service.DirectoryDefaultLimit, 0},
		{"a negative offset floors at zero", "?offset=-3", "", service.DirectoryDefaultLimit, 0},
		{"an unparseable offset floors at zero", "?offset=soon", "", service.DirectoryDefaultLimit, 0},
		// The service trims; the handler must not swallow the term first.
		{"whitespace reaches the service untouched", "?q=%20ali%20", " ali ", service.DirectoryDefaultLimit, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			users := &stubProfileService{}
			rec := directoryRequest(t, users, tt.query)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
			}
			if users.gotQuery != tt.wantQuery {
				t.Errorf("service got q = %q, want %q", users.gotQuery, tt.wantQuery)
			}
			if users.gotLimit != tt.wantLimit {
				t.Errorf("service got limit = %d, want %d", users.gotLimit, tt.wantLimit)
			}
			if users.gotOffset != tt.wantOffset {
				t.Errorf("service got offset = %d, want %d", users.gotOffset, tt.wantOffset)
			}
		})
	}
}

// total is the whole match, so it can and should exceed the page.
func TestUserHandler_ListDirectory_TotalExceedsThePage(t *testing.T) {
	users := &stubProfileService{
		directory: []*domain.User{
			{ID: "user-1", DisplayName: "Ada", Role: domain.RoleMember, IsActive: true},
			{ID: "user-2", DisplayName: "Bob", Role: domain.RoleMember, IsActive: true},
		},
		total: 57,
	}

	rec := directoryRequest(t, users, "?limit=2")

	var body directoryBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if len(body.Users) != 2 {
		t.Errorf("%d users on the page, want 2", len(body.Users))
	}
	if body.Total != 57 {
		t.Errorf("total = %d, want 57", body.Total)
	}
}

func TestUserHandler_ListDirectory_ValidationErrorIsA400(t *testing.T) {
	users := &stubProfileService{directoryErr: service.ErrValidation}

	rec := directoryRequest(t, users, "?q=whatever")

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d; body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestUserHandler_ListDirectory_ServiceFailureIsA500(t *testing.T) {
	users := &stubProfileService{directoryErr: errors.New("db down")}

	rec := directoryRequest(t, users, "")

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d; body: %s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
}
