package handler_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/handler"
)

func TestUserHandler_GetMe(t *testing.T) {
	h := handler.NewUserHandler()

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
	h := handler.NewUserHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	rec := httptest.NewRecorder()

	h.GetMe(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}
