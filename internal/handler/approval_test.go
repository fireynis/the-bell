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

// mockApprovalService implements the interface needed by ApprovalHandler.
type mockApprovalService struct {
	pendingUsers []*domain.User
	approvedUser *domain.User
	listErr      error
	approveErr   error
}

func (m *mockApprovalService) ListPending(_ context.Context) ([]*domain.User, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.pendingUsers, nil
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
