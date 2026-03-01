package handler

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/middleware"
	"github.com/fireynis/the-bell/internal/service"
)

const (
	defaultLimit = 20
	maxLimit     = 100
)

// PostHandler handles HTTP requests for post operations.
type PostHandler struct {
	posts *service.PostService
}

// NewPostHandler creates a PostHandler.
func NewPostHandler(posts *service.PostService) *PostHandler {
	return &PostHandler{posts: posts}
}

type createPostRequest struct {
	Body      string `json:"body"`
	ImagePath string `json:"image_path,omitempty"`
}

type updatePostRequest struct {
	Body string `json:"body"`
}

type listFeedResponse struct {
	Posts      []*domain.Post `json:"posts"`
	NextCursor string         `json:"next_cursor,omitempty"`
}

// Create handles POST /api/v1/posts.
func (h *PostHandler) Create(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	if !user.CanPost() {
		Error(w, http.StatusForbidden, "posting not allowed")
		return
	}

	var req createPostRequest
	if err := Decode(r, &req); err != nil {
		Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	post, err := h.posts.Create(r.Context(), user.ID, req.Body, req.ImagePath)
	if err != nil {
		serviceError(w, err)
		return
	}

	JSON(w, http.StatusCreated, post)
}

// GetByID handles GET /api/v1/posts/{id}.
func (h *PostHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	post, err := h.posts.GetByID(r.Context(), id)
	if err != nil {
		serviceError(w, err)
		return
	}

	JSON(w, http.StatusOK, post)
}

// ListFeed handles GET /api/v1/posts.
func (h *PostHandler) ListFeed(w http.ResponseWriter, r *http.Request) {
	cursor := r.URL.Query().Get("cursor")
	limit := parseLimit(r.URL.Query().Get("limit"))

	posts, err := h.posts.ListFeed(r.Context(), cursor, limit)
	if err != nil {
		serviceError(w, err)
		return
	}

	if posts == nil {
		posts = []*domain.Post{}
	}

	resp := listFeedResponse{Posts: posts}
	if len(posts) == limit {
		resp.NextCursor = posts[len(posts)-1].ID
	}

	JSON(w, http.StatusOK, resp)
}

// Update handles PATCH /api/v1/posts/{id}.
func (h *PostHandler) Update(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id := chi.URLParam(r, "id")

	var req updatePostRequest
	if err := Decode(r, &req); err != nil {
		Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	post, err := h.posts.UpdateBody(r.Context(), id, user.ID, req.Body)
	if err != nil {
		serviceError(w, err)
		return
	}

	JSON(w, http.StatusOK, post)
}

// Delete handles DELETE /api/v1/posts/{id}.
func (h *PostHandler) Delete(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id := chi.URLParam(r, "id")

	if err := h.posts.Delete(r.Context(), id, user.ID); err != nil {
		serviceError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func parseLimit(s string) int {
	if s == "" {
		return defaultLimit
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return defaultLimit
	}
	if n > maxLimit {
		return maxLimit
	}
	return n
}

func parseOffset(s string) int {
	if s == "" {
		return 0
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

