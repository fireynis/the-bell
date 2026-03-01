package handler

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/middleware"
)

// ApprovalLister lists pending users and approves them.
type ApprovalLister interface {
	ListPending(ctx context.Context) ([]*domain.User, error)
	Approve(ctx context.Context, userID string) (*domain.User, error)
}

// ApprovalHandler handles HTTP requests for council user approval.
type ApprovalHandler struct {
	approvals ApprovalLister
}

// NewApprovalHandler creates an ApprovalHandler.
func NewApprovalHandler(approvals ApprovalLister) *ApprovalHandler {
	return &ApprovalHandler{approvals: approvals}
}

type listPendingResponse struct {
	Users []*domain.User `json:"users"`
}

// ListPending handles GET /api/v1/vouches/pending.
func (h *ApprovalHandler) ListPending(w http.ResponseWriter, r *http.Request) {
	users, err := h.approvals.ListPending(r.Context())
	if err != nil {
		serviceError(w, err)
		return
	}

	if users == nil {
		users = []*domain.User{}
	}

	JSON(w, http.StatusOK, listPendingResponse{Users: users})
}

// Approve handles POST /api/v1/vouches/approve/{id}.
func (h *ApprovalHandler) Approve(w http.ResponseWriter, r *http.Request) {
	_, ok := middleware.UserFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	userID := chi.URLParam(r, "id")

	user, err := h.approvals.Approve(r.Context(), userID)
	if err != nil {
		serviceError(w, err)
		return
	}

	JSON(w, http.StatusOK, user)
}
