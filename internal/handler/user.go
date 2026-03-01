package handler

import (
	"net/http"

	"github.com/fireynis/the-bell/internal/middleware"
)

// UserHandler handles HTTP requests for user operations.
type UserHandler struct{}

// NewUserHandler creates a UserHandler.
func NewUserHandler() *UserHandler {
	return &UserHandler{}
}

// GetMe handles GET /api/v1/me.
// Returns the authenticated user from context (set by auth middleware).
func (h *UserHandler) GetMe(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	JSON(w, http.StatusOK, user)
}
