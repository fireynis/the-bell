package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/middleware"
)

// timestampFormat is the wire format for every timestamp this handler emits.
// The frontend parses profile and vouch timestamps with the same code, so the
// two must not drift.
const timestampFormat = "2006-01-02T15:04:05Z07:00"

// UserHandler handles HTTP requests for user profile operations.
type UserHandler struct {
	users   profileService
	posts   authorPostLister
	vouches VouchLister
}

// profileService abstracts the user profile reads and writes the handler needs.
type profileService interface {
	GetByID(ctx context.Context, id string) (*domain.User, error)
	UpdateProfile(ctx context.Context, id, displayName, bio, avatarURL string) (*domain.User, error)
}

// authorPostLister abstracts listing a single author's posts.
type authorPostLister interface {
	ListByAuthor(ctx context.Context, authorID string, limit int) ([]*domain.Post, error)
}

// VouchLister abstracts reading vouches for profile display.
type VouchLister interface {
	ListReceivedVouches(ctx context.Context, userID string) ([]*domain.Vouch, error)
	ListGivenVouches(ctx context.Context, userID string) ([]*domain.Vouch, error)
}

// NewUserHandler creates a UserHandler.
func NewUserHandler(users profileService, posts authorPostLister, vouches VouchLister) *UserHandler {
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

// ownProfileResponse is the profile a user sees of themselves. It carries the
// moderation state the user is entitled to know about but nobody else is.
type ownProfileResponse struct {
	userProfileResponse
	// MutedUntil is omitted entirely when the user is not muted, so the field's
	// presence is the answer to "am I muted?".
	MutedUntil string `json:"muted_until,omitempty"`
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
		JoinedAt:    u.JoinedAt.Format(timestampFormat),
	}
}

// toOwnProfileResponse builds the self view. A mute is between the user and the
// moderators: the user must be able to see why they cannot post, and no other
// caller has any business knowing, so muted_until appears here and in no other
// response shape.
//
// An expired mute is reported as no mute at all rather than as a past
// timestamp, so the client needs no clock of its own to interpret the field.
func toOwnProfileResponse(u *domain.User, now time.Time) ownProfileResponse {
	resp := ownProfileResponse{userProfileResponse: toProfileResponse(u)}
	if u.IsMuted(now) {
		resp.MutedUntil = u.MutedUntil.Format(timestampFormat)
	}
	return resp
}

// GetMe handles GET /api/v1/users/me.
func (h *UserHandler) GetMe(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	JSON(w, http.StatusOK, toOwnProfileResponse(user, time.Now()))
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

	// Also a self view, so it carries the same moderation state GetMe does.
	JSON(w, http.StatusOK, toOwnProfileResponse(updated, time.Now()))
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
		CreatedAt: v.CreatedAt.Format(timestampFormat),
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
