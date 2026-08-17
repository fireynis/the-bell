package handler

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/middleware"
)

// ApprovalService defines the operations needed by the approval handler.
type ApprovalService interface {
	ListPending(ctx context.Context, query string, limit, offset int) ([]*domain.User, int, error)
	Approve(ctx context.Context, userID string) (*domain.User, error)
}

// ApprovalHandler handles HTTP requests for council user approval.
type ApprovalHandler struct {
	approvals ApprovalService
}

// NewApprovalHandler creates an ApprovalHandler.
func NewApprovalHandler(approvals ApprovalService) *ApprovalHandler {
	return &ApprovalHandler{approvals: approvals}
}

// pendingUser is one applicant as the reviewing council sees them.
//
// The embedded *domain.User is promoted inline by encoding/json, so this is the
// existing pending-user shape with one field added rather than a new one — the
// queue has published the whole user record since the town's first release and
// narrowing it here would break the council's own screen.
//
// residency_claim is that field, and this type exists to hold it. The claim is
// untagged on domain.User precisely so that it cannot ride along anywhere the
// struct is serialized; naming it here is the single opt-in, and this is the
// only response in the API that carries it. It is not on public profiles, not
// in the member directory, and not on the approved user returned by Approve —
// once somebody is a member, what they said to get in is no longer a thing the
// API hands out.
//
// It is always present, empty string included, so the council's screen can tell
// "said nothing" from "not asked" without a second rule.
type pendingUser struct {
	*domain.User
	ResidencyClaim string `json:"residency_claim"`
}

// listPendingResponse pairs the page with the size of the whole match, exactly
// as the directory's does: the total is what lets the council's screen say how
// many neighbours are waiting without walking off the end of the queue to find
// out.
type listPendingResponse struct {
	Users []pendingUser `json:"users"`
	Total int           `json:"total"`
}

// ListPending handles GET /api/v1/vouches/pending.
func (h *ApprovalHandler) ListPending(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	users, total, err := h.approvals.ListPending(r.Context(),
		query.Get("q"),
		parseDirectoryLimit(query.Get("limit")),
		parseOffset(query.Get("offset")),
	)
	if err != nil {
		serviceError(w, err)
		return
	}

	entries := make([]pendingUser, 0, len(users))
	for _, u := range users {
		entries = append(entries, pendingUser{User: u, ResidencyClaim: u.ResidencyClaim})
	}

	JSON(w, http.StatusOK, listPendingResponse{Users: entries, Total: total})
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
