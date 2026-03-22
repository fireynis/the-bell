package handler

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/middleware"
	"github.com/fireynis/the-bell/internal/service"
	"github.com/fireynis/the-bell/internal/storage"
)

const (
	defaultLimit = 20
	maxLimit     = 100
)

// ReactionEnricher loads batch reaction data for posts.
type ReactionEnricher interface {
	BatchCountByPosts(ctx context.Context, postIDs []string) (map[string]map[domain.ReactionType]int, error)
	BatchGetUserReactions(ctx context.Context, userID string, postIDs []string) (map[string][]domain.ReactionType, error)
}

// PostHandlerOption configures a PostHandler.
type PostHandlerOption func(*PostHandler)

// WithStorage attaches a Storage backend for image uploads.
func WithStorage(s storage.Storage) PostHandlerOption {
	return func(h *PostHandler) { h.store = s }
}

// WithReactionEnricher attaches a ReactionEnricher for feed enrichment.
func WithReactionEnricher(re ReactionEnricher) PostHandlerOption {
	return func(h *PostHandler) { h.reactionEnricher = re }
}

// PostHandler handles HTTP requests for post operations.
type PostHandler struct {
	posts            *service.PostService
	store            storage.Storage
	reactionEnricher ReactionEnricher
}

// NewPostHandler creates a PostHandler.
func NewPostHandler(posts *service.PostService, opts ...PostHandlerOption) *PostHandler {
	h := &PostHandler{posts: posts}
	for _, opt := range opts {
		opt(h)
	}
	return h
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
// It accepts either application/json or multipart/form-data.
// For multipart requests the "body" form field supplies the post text and
// an optional "image" file field supplies an image to upload.
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

	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "multipart/form-data") {
		if err := h.parseMultipartCreate(r, &req); err != nil {
			if errors.Is(err, errUnsupportedType) || errors.Is(err, errFileTooLarge) {
				Error(w, http.StatusBadRequest, err.Error())
			} else {
				Error(w, http.StatusBadRequest, fmt.Sprintf("invalid multipart request: %v", err))
			}
			return
		}
	} else {
		if err := Decode(r, &req); err != nil {
			Error(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}

	post, err := h.posts.Create(r.Context(), user.ID, req.Body, req.ImagePath)
	if err != nil {
		serviceError(w, err)
		return
	}

	JSON(w, http.StatusCreated, post)
}

// parseMultipartCreate parses a multipart/form-data request into a createPostRequest.
// If an "image" file part is present it is validated, saved to storage, and the
// resulting path is set on req.ImagePath.
func (h *PostHandler) parseMultipartCreate(r *http.Request, req *createPostRequest) error {
	// Limit total request body to maxImageSize + 1 MB overhead for form fields.
	r.Body = http.MaxBytesReader(nil, r.Body, maxImageSize+1<<20)

	if err := r.ParseMultipartForm(maxImageSize); err != nil {
		return fmt.Errorf("parsing multipart form: %w", err)
	}

	req.Body = r.FormValue("body")

	imgData, ext, err := parseImageUpload(r, maxImageSize)
	if err != nil {
		return err
	}
	// No image field present — text-only post.
	if imgData == nil {
		return nil
	}

	if h.store == nil {
		return fmt.Errorf("image uploads not configured")
	}

	filename := fmt.Sprintf("%s%s", mustUUIDv7(), ext)
	path, err := h.store.Save(r.Context(), filename, bytes.NewReader(imgData))
	if err != nil {
		return fmt.Errorf("saving image: %w", err)
	}

	req.ImagePath = h.store.URL(path)
	return nil
}

func mustUUIDv7() string {
	id, err := uuid.NewV7()
	if err != nil {
		// uuid.NewV7 only fails if the random source is broken.
		panic(fmt.Sprintf("generating UUIDv7: %v", err))
	}
	return id.String()
}

// GetByID handles GET /api/v1/posts/{id}.
func (h *PostHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	post, err := h.posts.GetByID(r.Context(), id)
	if err != nil {
		serviceError(w, err)
		return
	}

	h.enrichPosts(r.Context(), r, []*domain.Post{post})

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

	h.enrichPosts(r.Context(), r, posts)

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

// enrichPosts attaches reaction counts and user reactions to posts.
func (h *PostHandler) enrichPosts(ctx context.Context, r *http.Request, posts []*domain.Post) {
	if h.reactionEnricher == nil || len(posts) == 0 {
		return
	}

	postIDs := make([]string, len(posts))
	for i, p := range posts {
		postIDs[i] = p.ID
	}

	if counts, err := h.reactionEnricher.BatchCountByPosts(ctx, postIDs); err == nil {
		for _, p := range posts {
			if c, ok := counts[p.ID]; ok {
				p.ReactionCounts = c
			}
		}
	}

	if user, ok := middleware.UserFromContext(ctx); ok {
		if userReactions, err := h.reactionEnricher.BatchGetUserReactions(ctx, user.ID, postIDs); err == nil {
			for _, p := range posts {
				if ur, ok := userReactions[p.ID]; ok {
					p.UserReactions = ur
				}
			}
		}
	}
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
