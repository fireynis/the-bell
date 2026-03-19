package handler

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/middleware"
	"github.com/fireynis/the-bell/internal/service"
)

// UserHandler handles HTTP requests for user profile operations.
type UserHandler struct {
	users   *service.UserService
	posts   *service.PostService
	vouches VouchLister
}

// VouchLister abstracts reading vouches for profile display.
type VouchLister interface {
	ListReceivedVouches(ctx context.Context, userID string) ([]*domain.Vouch, error)
	ListGivenVouches(ctx context.Context, userID string) ([]*domain.Vouch, error)
}

// NewUserHandler creates a UserHandler.
func NewUserHandler(users *service.UserService, posts *service.PostService, vouches VouchLister) *UserHandler {
	return &UserHandler{users: users, posts: posts, vouches: vouches}
}

type updateProfileRequest struct {
	DisplayName string `json:"display_name"`
	Bio         string `json:"bio"`
	AvatarURL   string `json:"avatar_url"`
}

type userProfileResponse struct {
	ID          string  `json:"id"`
	DisplayName string  `json:"display_name"`
	Bio         string  `json:"bio"`
	AvatarURL   string  `json:"avatar_url"`
	TrustScore  float64 `json:"trust_score"`
	Role        string  `json:"role"`
	IsActive    bool    `json:"is_active"`
	JoinedAt    string  `json:"joined_at"`
}

func toProfileResponse(u *domain.User) userProfileResponse {
	return userProfileResponse{
		ID:          u.ID,
		DisplayName: u.DisplayName,
		Bio:         u.Bio,
		AvatarURL:   u.AvatarURL,
		TrustScore:  u.TrustScore,
		Role:        string(u.Role),
		IsActive:    u.IsActive,
		JoinedAt:    u.JoinedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
}

// GetMe handles GET /api/v1/users/me.
func (h *UserHandler) GetMe(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	JSON(w, http.StatusOK, toProfileResponse(user))
}

// UpdateMe handles PUT /api/v1/users/me.
func (h *UserHandler) UpdateMe(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req updateProfileRequest
	if err := Decode(r, &req); err != nil {
		Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	updated, err := h.users.UpdateProfile(r.Context(), user.ID, req.DisplayName, req.Bio, req.AvatarURL)
	if err != nil {
		serviceError(w, err)
		return
	}

	JSON(w, http.StatusOK, toProfileResponse(updated))
}

// GetByID handles GET /api/v1/users/{id}.
func (h *UserHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	user, err := h.users.GetByID(r.Context(), id)
	if err != nil {
		serviceError(w, err)
		return
	}

	JSON(w, http.StatusOK, toProfileResponse(user))
}

type listUserPostsResponse struct {
	Posts []*domain.Post `json:"posts"`
}

// ListPosts handles GET /api/v1/users/{id}/posts.
func (h *UserHandler) ListPosts(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	limit := parseLimit(r.URL.Query().Get("limit"))

	posts, err := h.posts.ListByAuthor(r.Context(), id, limit)
	if err != nil {
		serviceError(w, err)
		return
	}

	if posts == nil {
		posts = []*domain.Post{}
	}

	JSON(w, http.StatusOK, listUserPostsResponse{Posts: posts})
}

type vouchEntry struct {
	ID        string `json:"id"`
	VoucherID string `json:"voucher_id"`
	VoucheeID string `json:"vouchee_id"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
}

type listVouchesResponse struct {
	Received []vouchEntry `json:"received"`
	Given    []vouchEntry `json:"given"`
}

func toVouchEntry(v *domain.Vouch) vouchEntry {
	return vouchEntry{
		ID:        v.ID,
		VoucherID: v.VoucherID,
		VoucheeID: v.VoucheeID,
		Status:    string(v.Status),
		CreatedAt: v.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
}

// ListVouches handles GET /api/v1/users/{id}/vouches.
func (h *UserHandler) ListVouches(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	received, err := h.vouches.ListReceivedVouches(r.Context(), id)
	if err != nil {
		serviceError(w, err)
		return
	}

	given, err := h.vouches.ListGivenVouches(r.Context(), id)
	if err != nil {
		serviceError(w, err)
		return
	}

	resp := listVouchesResponse{
		Received: make([]vouchEntry, 0, len(received)),
		Given:    make([]vouchEntry, 0, len(given)),
	}
	for _, v := range received {
		resp.Received = append(resp.Received, toVouchEntry(v))
	}
	for _, v := range given {
		resp.Given = append(resp.Given, toVouchEntry(v))
	}

	JSON(w, http.StatusOK, resp)
}
