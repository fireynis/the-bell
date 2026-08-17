package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/middleware"
	"github.com/fireynis/the-bell/internal/service"
)

// mockApprovalService implements ApprovalService for testing.
type mockApprovalService struct {
	pendingUsers []*domain.User
	total        int
	approvedUser *domain.User
	listErr      error
	approveErr   error

	// What the last listing was asked for, so a test can assert the handler's
	// parsing rather than infer it from the page that came back.
	gotQuery  string
	gotLimit  int
	gotOffset int
}

func (m *mockApprovalService) ListPending(_ context.Context, query string, limit, offset int) ([]*domain.User, int, error) {
	m.gotQuery, m.gotLimit, m.gotOffset = query, limit, offset
	if m.listErr != nil {
		return nil, 0, m.listErr
	}
	return m.pendingUsers, m.total, nil
}

func (m *mockApprovalService) Approve(_ context.Context, userID string) (*domain.User, error) {
	if m.approveErr != nil {
		return nil, m.approveErr
	}
	return m.approvedUser, nil
}

func TestApprovalHandler_ListPending_Success(t *testing.T) {
	svc := &mockApprovalService{
		pendingUsers: []*domain.User{
			{ID: "user-1", DisplayName: "Alice", Role: domain.RolePending},
		},
		total: 1,
	}
	h := NewApprovalHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/vouches/pending", nil)
	ctx := middleware.WithUser(req.Context(), &domain.User{
		ID: "council-1", Role: domain.RoleCouncil, IsActive: true,
	})
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.ListPending(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp struct {
		Users []domain.User `json:"users"`
		Total int           `json:"total"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if len(resp.Users) != 1 {
		t.Fatalf("len(users) = %d, want 1", len(resp.Users))
	}
	if resp.Users[0].ID != "user-1" {
		t.Errorf("users[0].ID = %q, want %q", resp.Users[0].ID, "user-1")
	}
	if resp.Total != 1 {
		t.Errorf("total = %d, want 1", resp.Total)
	}
}

// The query string is where limit, offset and the search term are read, and
// this is the only place that parsing is observable. The bounds are the
// directory's, which is why they are asserted against its constants.
func TestApprovalHandler_ListPending_QueryParameters(t *testing.T) {
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
		{"an unparseable limit falls back to the default", "?limit=lots", "", service.DirectoryDefaultLimit, 0},
		{"a negative offset floors at zero", "?offset=-3", "", service.DirectoryDefaultLimit, 0},
		// The service trims; the handler must not swallow the term first.
		{"whitespace reaches the service untouched", "?q=%20ali%20", " ali ", service.DirectoryDefaultLimit, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &mockApprovalService{}
			h := NewApprovalHandler(svc)

			req := httptest.NewRequest(http.MethodGet, "/api/v1/vouches/pending"+tt.query, nil)
			req = req.WithContext(middleware.WithUser(req.Context(), &domain.User{
				ID: "council-1", Role: domain.RoleCouncil, IsActive: true,
			}))
			w := httptest.NewRecorder()

			h.ListPending(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
			}
			if svc.gotQuery != tt.wantQuery {
				t.Errorf("service got q = %q, want %q", svc.gotQuery, tt.wantQuery)
			}
			if svc.gotLimit != tt.wantLimit {
				t.Errorf("service got limit = %d, want %d", svc.gotLimit, tt.wantLimit)
			}
			if svc.gotOffset != tt.wantOffset {
				t.Errorf("service got offset = %d, want %d", svc.gotOffset, tt.wantOffset)
			}
		})
	}
}

// total is the whole queue, so it can and should exceed the page: it is what
// the council's screen counts the waiting neighbours from.
func TestApprovalHandler_ListPending_TotalExceedsThePage(t *testing.T) {
	svc := &mockApprovalService{
		pendingUsers: []*domain.User{
			{ID: "user-1", DisplayName: "Ada", Role: domain.RolePending},
			{ID: "user-2", DisplayName: "Bo", Role: domain.RolePending},
		},
		total: 57,
	}
	h := NewApprovalHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/vouches/pending?limit=2", nil)
	req = req.WithContext(middleware.WithUser(req.Context(), &domain.User{
		ID: "council-1", Role: domain.RoleCouncil, IsActive: true,
	}))
	w := httptest.NewRecorder()

	h.ListPending(w, req)

	var resp struct {
		Users []domain.User `json:"users"`
		Total int           `json:"total"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if len(resp.Users) != 2 {
		t.Errorf("len(users) = %d, want 2", len(resp.Users))
	}
	if resp.Total != 57 {
		t.Errorf("total = %d, want 57", resp.Total)
	}
}

func TestApprovalHandler_ListPending_EmptyList(t *testing.T) {
	svc := &mockApprovalService{pendingUsers: nil}
	h := NewApprovalHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/vouches/pending", nil)
	ctx := middleware.WithUser(req.Context(), &domain.User{
		ID: "council-1", Role: domain.RoleCouncil, IsActive: true,
	})
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.ListPending(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp struct {
		Users []domain.User `json:"users"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Users == nil || len(resp.Users) != 0 {
		t.Errorf("expected empty array, got %v", resp.Users)
	}
}

func TestApprovalHandler_ListPending_ServiceError(t *testing.T) {
	svc := &mockApprovalService{
		listErr: service.ErrForbidden,
	}
	h := NewApprovalHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/vouches/pending", nil)
	ctx := middleware.WithUser(req.Context(), &domain.User{
		ID: "council-1", Role: domain.RoleCouncil, IsActive: true,
	})
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.ListPending(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestApprovalHandler_Approve_Success(t *testing.T) {
	svc := &mockApprovalService{
		approvedUser: &domain.User{
			ID: "user-1", DisplayName: "Alice", Role: domain.RoleMember,
		},
	}
	h := NewApprovalHandler(svc)

	r := chi.NewRouter()
	r.Post("/api/v1/vouches/approve/{id}", func(w http.ResponseWriter, req *http.Request) {
		ctx := middleware.WithUser(req.Context(), &domain.User{
			ID: "council-1", Role: domain.RoleCouncil, IsActive: true,
		})
		h.Approve(w, req.WithContext(ctx))
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/vouches/approve/user-1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp domain.User
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if resp.Role != domain.RoleMember {
		t.Errorf("role = %q, want %q", resp.Role, domain.RoleMember)
	}
}

func TestApprovalHandler_Approve_NotFound(t *testing.T) {
	svc := &mockApprovalService{
		approveErr: service.ErrNotFound,
	}
	h := NewApprovalHandler(svc)

	r := chi.NewRouter()
	r.Post("/api/v1/vouches/approve/{id}", func(w http.ResponseWriter, req *http.Request) {
		ctx := middleware.WithUser(req.Context(), &domain.User{
			ID: "council-1", Role: domain.RoleCouncil, IsActive: true,
		})
		h.Approve(w, req.WithContext(ctx))
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/vouches/approve/nonexistent", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestApprovalHandler_Approve_Unauthorized(t *testing.T) {
	svc := &mockApprovalService{}
	h := NewApprovalHandler(svc)

	// No user in context
	req := httptest.NewRequest(http.MethodPost, "/api/v1/vouches/approve/user-1", nil)
	w := httptest.NewRecorder()

	h.Approve(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}
