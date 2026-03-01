package handler

import (
	"net/http"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/middleware"
)

// meResponse is the public representation of the current user.
// It omits internal fields like KratosIdentityID that the frontend has no use for.
type meResponse struct {
	ID          string      `json:"id"`
	DisplayName string      `json:"display_name"`
	Bio         string      `json:"bio"`
	AvatarURL   string      `json:"avatar_url"`
	TrustScore  float64     `json:"trust_score"`
	Role        domain.Role `json:"role"`
	IsActive    bool        `json:"is_active"`
	JoinedAt    time.Time   `json:"joined_at"`
	CreatedAt   time.Time   `json:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at"`
}

func toMeResponse(u *domain.User) meResponse {
	return meResponse{
		ID:          u.ID,
		DisplayName: u.DisplayName,
		Bio:         u.Bio,
		AvatarURL:   u.AvatarURL,
		TrustScore:  u.TrustScore,
		Role:        u.Role,
		IsActive:    u.IsActive,
		JoinedAt:    u.JoinedAt,
		CreatedAt:   u.CreatedAt,
		UpdatedAt:   u.UpdatedAt,
	}
}

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

	JSON(w, http.StatusOK, toMeResponse(user))
}
